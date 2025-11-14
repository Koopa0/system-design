# URL Shortener 系統設計文檔

## 📋 問題定義

### 業務需求
構建短網址服務（類似 bit.ly、TinyURL），支援：
- **縮短 URL**：將長 URL 轉換為短碼（如 `https://short.url/aB3xD9`）
- **重定向**：訪問短碼時跳轉到原始 URL
- **自定義短碼**：允許用戶指定品牌短鏈（如 `bit.ly/google-io`）
- **點擊統計**：追蹤短碼被訪問的次數
- **過期機制**：支持設置短碼有效期

### 技術指標
| 指標 | 目標值 | 挑戰 |
|------|--------|------|
| **寫入 QPS** | 12 (100 萬 URL/天) | 如何生成唯一短碼？ |
| **讀取 QPS** | 1,160 (1 億重定向/天) | 如何承受高頻讀取？ |
| **重定向延遲** | P99 < 10ms | 如何保證低延遲？ |
| **短碼長度** | 6-7 字符 | 如何平衡長度與容量？ |
| **存儲容量** | 36.5 億 URL (10 年) | 如何選擇編碼方式？ |

### 容量估算
```
假設：
- 寫入：100 萬 URL/天
- 讀寫比：100:1（1 億次重定向/天）
- 存儲時間：10 年
- URL 平均長度：100 bytes

計算：
- 總 URL 數：100 萬/天 × 365 天 × 10 年 = 36.5 億
- 寫入 QPS：100 萬 / 86,400 ≈ 12
- 讀取 QPS：1 億 / 86,400 ≈ 1,160
- 存儲空間：36.5 億 × 100 bytes ≈ 365 GB
- 快取需求（80/20 法則）：20% 熱門 URL = 73 GB
```

---

## 🤔 設計決策樹

### 決策 1：如何生成唯一短碼？

```
需求：將長 URL 映射為短碼，保證全局唯一

❌ 方案 A：隨機字符串 + 衝突檢測
   機制：生成隨機 6 位字符串，檢查資料庫是否存在
   問題：
   - 衝突率隨容量增加而上升
   - 多次檢查性能開銷大
   - 不可預測重試次數

   計算衝突率：
   - 6 位 Base62：62^6 = 568 億種組合
   - 已有 10 億 URL
   - 衝突率 = 10 億 / 568 億 ≈ 1.76%
   - 平均需要 1.02 次查詢（第一次 + 1.76% × 第二次）
   - 但尾部延遲高（可能需要多次重試）

❌ 方案 B：Hash(URL) + Base62
   機制：對 URL 計算 MD5/SHA256，取前 N 位編碼
   問題：
   - 衝突無法完全避免（生日悖論）
   - 需要處理衝突（增加隨機 salt 重試）
   - 同一 URL 重複提交會生成相同短碼（可能不期望）

   計算：
   - MD5 取前 32 bit → Base62 編碼 ≈ 6 位
   - 10 億 URL 的衝突率：1 - e^(-10億^2 / (2×2^32)) ≈ 100%（必定衝突）

❌ 方案 C：資料庫自增 ID + Base62
   機制：使用 PostgreSQL AUTO_INCREMENT，編碼為 Base62
   問題：
   - 單點故障：單一資料庫瓶頸
   - 擴展困難：多資料庫時需要協調
   - 可預測性：攻擊者可以枚舉所有 URL

   分析：
   - 單機寫入上限：約 5,000 QPS
   - 當前需求 12 QPS → 足夠
   - 但無法水平擴展（多資料庫 ID 會衝突）

✅ 方案 D：Snowflake ID + Base62
   機制：
   - Snowflake 生成全局唯一 64-bit ID
   - Base62 編碼為 7 位字符串
   - 無需檢查衝突（算法保證唯一）

   Snowflake 結構（64 bit）：
   - 1 bit：符號位（未使用）
   - 41 bit：時間戳（毫秒）→ 可用 69 年
   - 10 bit：機器 ID → 支持 1024 台機器
   - 12 bit：序列號 → 每毫秒 4096 個 ID

   優勢：
   - 全局唯一：時間戳 + 機器 ID + 序列號
   - 趨勢遞增：有利於資料庫 B-Tree 索引
   - 高性能：本地生成，無網絡開銷
   - 可擴展：每台機器獨立生成

   權衡：
   - 需要機器 ID 分配（配置管理）
   - 時鐘回撥問題（需要檢測）
   - 短碼稍長（7 位 vs 6 位）
```

**選擇：方案 D（Snowflake + Base62）**

**實現細節：**
```go
// Snowflake 生成器
gen, _ := snowflake.NewGenerator(machineID)
id := gen.Generate()  // 如：123456789

// Base62 編碼
shortCode := base62.Encode(id)  // 如："8M0kX"

// 容量分析：
// - 7 位 Base62：62^7 = 3.5 兆（3.5 trillion）
// - 足夠使用數十年
// - 實際使用中約 7-8 位（從 2024 年開始）
```

---

### 決策 2：為什麼用 Base62 而非其他編碼？

```
問題：如何將 64-bit 整數編碼為短字符串？

❌ 方案 A：Base10（十進制）
   編碼：0-9
   長度：19 位數字（2^63 ≈ 9.2 × 10^18）
   問題：太長，不適合短 URL

❌ 方案 B：Base64
   編碼：A-Z, a-z, 0-9, +, /
   長度：11 位字符
   問題：
   - 包含 + 和 /（URL 中需要轉義）
   - 轉義後：+ → %2B, / → %2F
   - 不美觀：https://short.url/aB+3x/D9 → https://short.url/aB%2B3x%2FD9

✅ 方案 C：Base62
   編碼：A-Z, a-z, 0-9（僅字母和數字）
   長度：11 位字符（實際使用 7-8 位）
   優勢：
   - URL 友好：無需轉義
   - 美觀：易讀易記
   - 兼容性：所有系統支持
   - 大小寫敏感：增加組合數

   權衡：
   - 比 Base64 稍長（11 位 vs 10.7 位）
   - 犧牲 3% 壓縮率，換取更好兼容性

❌ 方案 D：Base32
   編碼：A-Z, 2-7（避免混淆）
   長度：13 位字符
   問題：太長，不適合短 URL
   優勢：無混淆（0/O, 1/I/l）→ 適合手動輸入場景
```

**選擇：方案 C（Base62）**

**編碼表：**
```
索引 0-9  → 字符 0-9
索引 10-35 → 字符 a-z
索引 36-61 → 字符 A-Z

範例：
ID: 123456789
Base62: 8M0kX

計算過程：
123456789 % 62 = 27 → b
  1991238 % 62 = 48 → M
    32116 % 62 = 46 → K
      518 % 62 =  8 → 8
        8 % 62 =  8 → 8

倒序：8M0kX
```

---

### 決策 3：如何處理高頻讀取？

```
問題：讀寫比 100:1，如何承受 1,160 QPS 讀取？

❌ 方案 A：直接查資料庫
   機制：每次請求都查 PostgreSQL
   問題：
   - 資料庫成為瓶頸：單機 PostgreSQL ~5,000 QPS
   - 延遲高：SSD 讀取 ~1-5ms
   - 浪費資源：熱門 URL 重複查詢

   計算：
   - 1,160 QPS 查詢
   - P99 延遲：~10ms
   - 資料庫 CPU：~30%（可接受但不優雅）

❌ 方案 B：只用 Redis（無資料庫）
   問題：
   - 持久化風險：Redis 重啟後數據可能丟失
   - 成本高：36.5 億 URL × 100 bytes = 365 GB 內存（昂貴）
   - 冷數據浪費：80% 的 URL 很少被訪問

✅ 方案 C：Cache-Aside 模式（Redis + PostgreSQL）
   機制：
   1. 先查 Redis（內存）
   2. 快取命中：直接返回（< 1ms）
   3. 快取未命中：查資料庫 → 寫入 Redis
   4. 設置 TTL：24 小時（自動過期）

   優勢：
   - 快取命中率高：80-85%（80/20 法則）
   - 延遲低：快取命中 < 1ms
   - 成本優化：僅快取熱門 URL（73 GB vs 365 GB）
   - 持久化：資料庫保證數據不丟失

   權衡：
   - 快取未命中延遲：5-10ms
   - 快取一致性：更新 URL 需要失效快取
   - 複雜度：需要處理快取問題（穿透、雪崩、擊穿）

   性能計算：
   - 快取命中（80%）：1,160 × 80% = 928 req，延遲 < 1ms
   - 快取未命中（20%）：1,160 × 20% = 232 req，延遲 ~5ms
   - 平均延遲：80% × 1ms + 20% × 5ms = 1.8ms ✅
```

**選擇：方案 C（Cache-Aside）**

**實現細節：**
```go
func Redirect(ctx context.Context, shortCode string) (string, error) {
    // 1. 先查 Redis
    if longURL, err := cache.Get(ctx, shortCode); err == nil {
        return longURL, nil  // 快取命中
    }

    // 2. 快取未命中，查資料庫
    url, err := store.GetByShortCode(ctx, shortCode)
    if err != nil {
        return "", err
    }

    // 3. 寫入 Redis（異步）
    go cache.Set(ctx, shortCode, url.LongURL, 24*time.Hour)

    return url.LongURL, nil
}
```

---

### 決策 4：如何處理快取問題？

```
Cache-Aside 模式的三大問題

問題 1：快取穿透（Cache Penetration）
場景：攻擊者查詢大量不存在的短碼
問題：
- 每次都查資料庫（快取無效）
- 資料庫壓力暴增
- 可能導致服務不可用

範例：
GET /abc123 → Redis 無 → DB 無 → 404
GET /xyz789 → Redis 無 → DB 無 → 404
... 每次都打到資料庫

❌ 方案 A：快取空值
機制：不存在的短碼也快取（值為 null）
問題：浪費內存（攻擊者可以生成大量無效 key）

✅ 方案 B：Bloom Filter（佈隆過濾器）
機制：
- 啟動時將所有短碼加入 Bloom Filter
- 查詢時先檢查 Bloom Filter
- 不存在：直接返回 404（不查 DB）
- 可能存在：繼續查 Redis/DB

優勢：
- 空間效率：36.5 億 URL，誤判率 1% → 約 5 GB 內存
- 速度快：O(k) 時間複雜度（k 為哈希函數數量）
- 完全阻擋不存在的 key

權衡：
- 誤判率：1% 的不存在 key 會被誤判為存在（可接受）
- 更新複雜：新增 URL 需要更新 Bloom Filter

---

問題 2：快取雪崩（Cache Avalanche）
場景：大量快取同時過期
問題：
- 瞬間所有請求打到資料庫
- 資料庫壓力暴增
- 可能導致級聯失敗

時序範例：
- T0：設置 1000 個 URL，TTL = 24h
- T1 (24h 後)：1000 個 key 同時過期
- T2：1000 個請求同時查資料庫 ❌

✅ 方案：隨機 TTL
機制：TTL = 24h ± 1h（隨機）
效果：過期時間分散，避免同時失效

實現：
ttl := 24*time.Hour + time.Duration(rand.Intn(3600))*time.Second

---

問題 3：快取擊穿（Cache Breakdown）
場景：熱門 URL 快取過期瞬間
問題：
- 大量並發請求同時查資料庫（查詢同一 key）
- 資料庫瞬時壓力峰值

時序範例：
- T0：熱門 URL 快取過期
- T1：100 個並發請求同時到達
- T2：100 個請求都查資料庫（重複查詢）

❌ 方案 A：分散式鎖
機制：第一個請求加鎖查資料庫，其他請求等待
問題：增加延遲、複雜度高

✅ 方案 B：熱門 URL 永不過期
機制：
- 識別熱門 URL（點擊數 > 閾值）
- 設置 TTL = 0（永不過期）
- 後台定期刷新（可選）

實現：
if url.Clicks > 10000 {
    cache.Set(ctx, shortCode, longURL, 0)  // 永不過期
} else {
    cache.Set(ctx, shortCode, longURL, 24*time.Hour)
}
```

**已實現：** 隨機 TTL（防雪崩）
**教學簡化：** Bloom Filter、熱門 URL 永不過期（生產環境建議）

---

### 決策 5：如何防止安全問題？

```
問題：用戶提交的 URL 可能包含惡意內容

威脅 1：SSRF（Server-Side Request Forgery）
場景：攻擊者提交內網 URL
範例：
POST /shorten
{"long_url": "http://192.168.1.1/admin"}

危害：
- 短碼重定向到內網服務（繞過防火牆）
- 訪問雲服務元數據（如 http://169.254.169.254）
- 獲取敏感信息（AWS credentials）

✅ 防護方案：
1. 驗證 scheme：僅允許 http/https
2. 拒絕私有 IP：
   - 127.0.0.0/8（localhost）
   - 10.0.0.0/8（私有網段 A）
   - 172.16.0.0/12（私有網段 B）
   - 192.168.0.0/16（私有網段 C）
   - 169.254.0.0/16（AWS 元數據）
3. DNS 解析檢查：
   - 解析域名為 IP
   - 檢查所有結果 IP（防止 evil.com → 192.168.1.1）

⚠️ 已知限制（教學簡化）：
- DNS rebinding 攻擊（TOCTOU）
- IPv6 私有範圍未檢查
- 無 HTTP 重定向檢查

---

威脅 2：XSS（Cross-Site Scripting）
場景：攻擊者提交 javascript: URL
範例：
POST /shorten
{"long_url": "javascript:alert(document.cookie)"}

危害：
- 用戶點擊短碼時執行惡意腳本
- 竊取 cookie、token

✅ 防護方案：
- 僅允許 http/https scheme
- 拒絕 javascript:、data:、file: 等

---

威脅 3：釣魚（Phishing）
場景：攻擊者創建短碼指向釣魚網站
範例：
短碼：bit.ly/paypal-verify
實際：http://evil.com/fake-paypal

緩解方案（教學未實現）：
- URL 黑名單：已知釣魚網站
- 用戶舉報機制
- 訪問警告：顯示原始 URL
```

**已實現：** SSRF 基本防護、scheme 驗證
**生產環境建議：** URL 黑名單、用戶舉報、訪問警告

---

## 📈 擴展性分析

### 當前架構容量

```
單機配置：
- API Server：4 core, 8 GB
- Redis：16 GB 內存
- PostgreSQL：4 core, 100 GB SSD

性能：
- 寫入：12 QPS（輕鬆應對）
- 讀取：1,160 QPS（快取命中率 80%）
  - Redis：928 req（< 1ms）
  - PostgreSQL：232 req（~5ms）
- 資料庫負載：~10%

結論：單機架構足夠支撐當前需求
```

### 10x 擴展（10,000 讀取 QPS）

```
瓶頸分析：
✅ Redis：可支撐 100,000 QPS（遠未飽和）
❌ PostgreSQL：2,000 快取未命中 QPS（接近極限）
❌ API Server：單機網絡頻寬可能不足

方案 1：垂直擴展 PostgreSQL
- 升級為 16 core, 64 GB
- SSD → NVMe
- 效果：讀取 QPS × 3
- 成本：$500/月 → $1,500/月

方案 2：PostgreSQL 讀寫分離
- 1 主（寫入）+ 2 從（讀取）
- 快取未命中查從庫
- 效果：讀取 QPS × 3
- 成本：+$1,000/月（2 個從庫）

方案 3：API Server 水平擴展
- 3 個無狀態 API Server
- Nginx 負載均衡
- 效果：QPS × 3
- 成本：+$300/月

推薦組合：方案 2 + 方案 3
- 總成本：~$2,000/月
- 容量：15,000 QPS
```

### 100x 擴展（100,000 讀取 QPS）

```
需要架構升級：

1. API Server 集群
   - 10 個無狀態實例
   - 負載均衡：Nginx 或 AWS ALB
   - 自動擴展：根據 CPU/QPS 指標

2. Redis Cluster
   - 分片：16 個 master（按 shortCode hash）
   - 每個 shard：約 6,250 QPS
   - 總容量：100,000 QPS（快取命中率 80%）

3. PostgreSQL 集群
   - 分片：按 shortCode hash（與 Redis 對齊）
   - 8 個 shard（每個 1 主 + 2 從）
   - 每個 shard：2,500 QPS（快取未命中 20%）
   - 總容量：20,000 QPS

4. Snowflake ID 生成
   - 每個 API Server 分配唯一 machineID
   - 10 個實例：machineID 0-9
   - 無需協調，本地生成

5. CDN 加速（可選）
   - 靜態重定向頁面緩存到 CDN
   - 減少 API Server 壓力

架構：
Client
  ↓
CDN (optional)
  ↓
Load Balancer (Nginx/ALB)
  ↓
├─ API Server 1 (machineID=0)
├─ API Server 2 (machineID=1)
├─ ...
└─ API Server 10 (machineID=9)
  ↓
├─ Redis Cluster (16 shards)
│  └─ 按 hash(shortCode) 分片
  ↓
└─ PostgreSQL Cluster (8 shards)
   ├─ Shard 0: 1 master + 2 replicas
   ├─ ...
   └─ Shard 7: 1 master + 2 replicas

成本估算：
- API Servers：10 × $100 = $1,000/月
- Redis Cluster：16 × $100 = $1,600/月
- PG Cluster：8 × 3 × $150 = $3,600/月
- Load Balancer：$100/月
- CDN：$500/月（可選）
- 總計：約 $6,800/月（不含 CDN）
```

---

## 🔧 實現範圍標註

### ✅ 已實現（核心教學內容）

| 功能 | 檔案 | 教學重點 |
|------|------|----------|
| **Snowflake ID** | `snowflake/snowflake.go:62-152` | 分布式 ID 生成、位運算 |
| **Base62 編碼** | `base62/base62.go` | 進制轉換、URL 友好編碼 |
| **SSRF 防護** | `shorten.go:152-256` | 私有 IP 檢查、DNS 解析 |
| **指標深拷貝** | `shorten.go:109-116` | 防止指標共享問題 |
| **唯一約束** | `postgres.go` | 短碼唯一性、衝突處理 |

### ⚠️ 教學簡化（未實現）

| 功能 | 原因 | 生產環境建議 |
|------|------|-------------|
| **Bloom Filter** | 增加複雜度 | Redis + Bloom Filter，防快取穿透 |
| **熱門 URL 優化** | 聚焦核心邏輯 | 永不過期 + LRU 驅逐策略 |
| **點擊統計詳細** | 簡化示範 | 來源分析、地理位置、時間分布 |
| **URL 黑名單** | 安全功能簡化 | 釣魚網站黑名單、用戶舉報 |
| **DNS rebinding 防護** | 已知限制標註 | 固定 IP 請求、禁止重定向 |

### 🚀 生產環境額外需要

```
1. 安全加固
   - URL 黑名單：已知釣魚、惡意網站
   - 用戶舉報機制：標記可疑短碼
   - 訪問警告：顯示原始 URL（如 Google Safe Browsing）
   - 速率限制：防止批量生成短碼
   - CAPTCHA：防止自動化攻擊

2. 點擊分析
   - 來源追蹤：Referer、UTM 參數
   - 地理位置：根據 IP 解析國家/城市
   - 設備分析：User-Agent 解析
   - 時間分布：按小時/天/周統計
   - 轉換率：配合業務指標

3. 快取優化
   - Bloom Filter：防快取穿透（5 GB 內存）
   - 熱門 URL：永不過期（點擊數 > 10,000）
   - 預熱機制：啟動時加載熱門 URL
   - 多級快取：L1(本地) + L2(Redis)

4. 監控告警
   - QPS：寫入/讀取 QPS 監控
   - 延遲：P50/P95/P99 分位數
   - 快取命中率：目標 > 80%
   - 錯誤率：404 率、SSRF 攔截率
   - 資料庫慢查詢：> 10ms 的查詢

5. 業務功能
   - 短碼編輯：更新長 URL（需失效快取）
   - 短碼刪除：軟刪除（deleted_at）
   - 訪問密碼：私密短碼（輸入密碼後跳轉）
   - 二維碼生成：短碼 → QR Code
   - 批量生成：CSV 上傳批量縮短
```

---

## 💡 關鍵設計原則總結

### 1. 分布式唯一 ID（Snowflake）
```
時間戳（41 bit）+ 機器 ID（10 bit）+ 序列號（12 bit）

優勢：
- 全局唯一（無需協調）
- 趨勢遞增（B-Tree 索引友好）
- 高性能（本地生成）

容量：
- 69 年時間範圍
- 1024 台機器
- 每毫秒 4096 個 ID
```

### 2. Base62 編碼（URL 友好）
```
為什麼不用 Base64？
- Base64 包含 +/ 字符（需要轉義）
- Base62 僅 [a-zA-Z0-9]（無需轉義）

長度分析：
- 64-bit 整數 → 11 位 Base62
- 實際使用（從 2024）→ 7-8 位

犧牲 3% 壓縮率，換取更好兼容性
```

### 3. Cache-Aside 模式（Redis + DB）
```
讀取流程：
1. 先查 Redis（< 1ms）
2. 命中：直接返回
3. 未命中：查 DB → 寫 Redis

效果：
- 80% 請求命中快取（< 1ms）
- 20% 請求查資料庫（~5ms）
- 平均延遲：1.8ms

成本優化：
- 僅快取熱門 URL（73 GB vs 365 GB）
- 冷數據仍持久化（不丟失）
```

### 4. SSRF 防護（安全第一）
```
防護層次：
1. Scheme 驗證：僅 http/https
2. 私有 IP 拒絕：127.0.0.0/8, 10.0.0.0/8 等
3. DNS 解析檢查：防止域名解析到私有 IP

已知限制（教學標註）：
- DNS rebinding（TOCTOU）
- IPv6 私有範圍
- HTTP 重定向攻擊

生產環境應加強
```

---

## 📚 延伸閱讀

### 相關系統設計問題
- 如何設計一個**分布式 ID 生成器**？（Snowflake 詳解）
- 如何設計一個**高性能快取系統**？（Redis 優化）
- 如何設計一個**點擊統計系統**？（大數據分析）

### 系統設計模式
- **Cache-Aside Pattern**：快取旁路模式
- **Bloom Filter**：佈隆過濾器（快取穿透防護）
- **Read-Through Cache**：透寫快取（另一種快取模式）
- **Consistent Hashing**：一致性哈希（分片策略）

### 安全主題
- **SSRF 攻擊與防禦**：私有 IP、DNS rebinding
- **XSS 防護**：輸入驗證、Content Security Policy
- **釣魚防護**：URL 黑名單、用戶舉報

---

## 🎯 總結

URL Shortener 展示了**分布式系統**的經典設計模式：

1. **Snowflake ID**：分布式唯一 ID 生成，無需協調
2. **Base62 編碼**：URL 友好，平衡長度與容量
3. **Cache-Aside**：快取與資料庫結合，高性能低成本
4. **SSRF 防護**：安全第一，多層防禦

**核心思想：** 用分布式 ID 避免衝突，用快取優化讀多寫少場景，用多層檢查保證安全。

**適用場景：** 短網址、邀請碼、優惠券碼、任何需要將長標識符縮短的場景

**不適用：** 需要語義化 ID（如訂單號）、需要順序保證（Snowflake 僅趨勢遞增）

**與其他服務對比：**
| 維度 | URL Shortener | Counter Service | Room Management |
|------|---------------|-----------------|-----------------|
| **核心挑戰** | 全局唯一 ID | 高頻寫入 | 實時同步 |
| **讀寫比** | 100:1 | 10:1 | 50:1 |
| **快取策略** | Cache-Aside | Write-Behind | 無（內存） |
| **一致性** | 強一致性 | 最終一致性 | 強一致性 |
| **擴展瓶頸** | 資料庫分片 | 批量寫入 | WebSocket 連接 |
