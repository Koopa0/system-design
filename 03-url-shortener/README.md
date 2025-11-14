# URL Shortener (短網址服務)

> 經典的系統設計案例，涵蓋分布式 ID 生成、Base62 編碼、快取策略等核心概念

## 📋 目錄

- [問題定義](#問題定義)
- [需求分析](#需求分析)
- [容量估算](#容量估算)
- [API 設計](#api-設計)
- [數據模型](#數據模型)
- [高層架構](#高層架構)
- [詳細設計](#詳細設計)
- [擴展性討論](#擴展性討論)

---

## 🎯 問題定義

設計一個類似 **bit.ly** 或 **TinyURL** 的短網址服務。

### 功能

給定一個長網址，生成一個短網址：
```
輸入: https://www.example.com/very/long/url/with/many/parameters?id=12345&ref=abc
輸出: https://short.url/aB3xD9
```

點擊短網址時，重定向到原始長網址。

### 使用場景

1. **社交媒體**：Twitter 字數限制（280 字）
2. **營銷追蹤**：追蹤點擊來源、轉換率
3. **美觀易記**：短網址更容易分享和記憶
4. **防止釣魚**：顯示短網址來源，用戶更信任

---

## 📊 需求分析

### 功能需求

1. **縮短網址**
   - 用戶提交長網址
   - 系統返回唯一的短網址
   - 短網址格式：`https://short.url/{shortCode}`

2. **重定向**
   - 用戶訪問短網址
   - 302 重定向到原始長網址

3. **自定義短網址**（可選）
   - 用戶指定自定義 shortCode
   - 例如：`https://short.url/google` → `https://www.google.com`

4. **過期機制**（可選）
   - 設置過期時間
   - 過期後返回 404

5. **統計分析**（可選）
   - 點擊次數
   - 訪問來源（Referer）
   - 地理位置

### 非功能需求

1. **高可用性**：99.9% uptime
2. **低延遲**：重定向延遲 < 100ms
3. **高吞吐量**：支持 10,000+ QPS（讀多寫少）
4. **可擴展性**：輕鬆擴展到更高 QPS
5. **唯一性**：短網址不能重複
6. **不可預測性**：無法猜測下一個短網址（安全性）

---

## 🔢 容量估算

### 假設

- **DAU（每日活躍用戶）**：100M 用戶
- **每日新 URL**：100M 條
- **讀寫比例**：100:1（每個短網址平均被訪問 100 次）

### QPS 計算

```
寫入 QPS（創建短網址）:
100M / 86400 秒 ≈ 1,160 QPS

讀取 QPS（重定向）:
1,160 × 100 = 116,000 QPS

峰值 QPS（假設 3 倍）:
寫入: 3,500 QPS
讀取: 350,000 QPS
```

### 存儲估算

```
每條記錄:
- short_code: 7 字節
- long_url: 平均 200 字節
- created_at: 8 字節
- 其他元數據: 50 字節
總計: ~265 字節

每年存儲:
100M × 365 天 = 36.5B 條 URL
36.5B × 265 bytes ≈ 9.7 TB

5 年存儲:
9.7 TB × 5 = 48.5 TB
```

### 短網址長度計算

使用 Base62 編碼（[a-zA-Z0-9]，62 個字符）：

```
6 位: 62^6 = 56.8B（568 億）
7 位: 62^7 = 3.5T （3.5 兆）
8 位: 62^8 = 218T（218 兆）
```

**決策**：使用 **7 位** Base62 編碼
- 容量：3.5 兆條 URL
- 按每天 100M 條，可用 35,000 天（約 96 年）✅

---

## 🔌 API 設計

### 1. 創建短網址

**請求**：
```http
POST /api/v1/urls
Content-Type: application/json

{
  "long_url": "https://www.example.com/very/long/url",
  "custom_alias": "mylink",     // 可選
  "expire_at": "2024-12-31"     // 可選
}
```

**響應**：
```json
{
  "short_url": "https://short.url/aB3xD9",
  "short_code": "aB3xD9",
  "long_url": "https://www.example.com/very/long/url",
  "created_at": "2024-01-15T10:30:00Z",
  "expire_at": null
}
```

### 2. 重定向

**請求**：
```http
GET /{shortCode}
```

**響應**：
```http
HTTP/1.1 302 Found
Location: https://www.example.com/very/long/url
```

**為什麼用 302 而不是 301？**
- **301（永久重定向）**：瀏覽器會快取，後續訪問不經過服務器（無法統計）
- **302（臨時重定向）**：每次都經過服務器，可以統計點擊 ✅

### 3. 獲取統計信息（可選）

**請求**：
```http
GET /api/v1/urls/{shortCode}/stats
```

**響應**：
```json
{
  "short_code": "aB3xD9",
  "clicks": 12345,
  "created_at": "2024-01-15T10:30:00Z",
  "last_accessed": "2024-01-20T15:45:00Z"
}
```

---

## 💾 數據模型

### PostgreSQL Schema

```sql
CREATE TABLE urls (
    id BIGSERIAL PRIMARY KEY,
    short_code VARCHAR(10) UNIQUE NOT NULL,
    long_url TEXT NOT NULL,
    custom_alias BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT NOW(),
    expire_at TIMESTAMP,
    clicks BIGINT DEFAULT 0,

    INDEX idx_short_code (short_code),
    INDEX idx_expire_at (expire_at)
);
```

### Redis 快取

```
Key: "url:aB3xD9"
Value: "https://www.example.com/very/long/url"
TTL: 3600 (1 小時)
```

**快取策略**：Cache-Aside
1. 讀取時先查 Redis
2. Redis Miss → 查 PostgreSQL → 寫入 Redis
3. 寫入時直接寫 PostgreSQL，不寫 Redis（避免一致性問題）

---

## 🏗️ 高層架構

```
                           ┌──────────────┐
                           │  CDN / DNS   │
                           └──────┬───────┘
                                  │
                    ┌─────────────┴─────────────┐
                    │     Load Balancer         │
                    └─────────────┬─────────────┘
                                  │
                    ┌─────────────┴─────────────┐
        ┌───────────┴──────────┐    ┌───────────┴──────────┐
        │   API Server 1       │    │   API Server 2       │
        │  (Stateless)         │    │  (Stateless)         │
        └───────┬──────────────┘    └──────────┬───────────┘
                │                               │
        ┌───────┴───────────────────────────────┴────────┐
        │                                                 │
        │   ┌──────────┐            ┌──────────┐        │
        └──▶│  Redis   │            │PostgreSQL│◀───────┘
            │ (Cache)  │            │ (Master) │
            └──────────┘            └────┬─────┘
                                         │
                                    ┌────┴─────┐
                                    │PostgreSQL│
                                    │(Replicas)│
                                    └──────────┘
```

### 組件說明

1. **CDN / DNS**
   - 將請求路由到最近的數據中心
   - 減少延遲

2. **Load Balancer**
   - 分發流量到多個 API Server
   - 健康檢查

3. **API Server**
   - 無狀態設計，易於水平擴展
   - 處理創建和重定向邏輯

4. **Redis**
   - 快取熱門短網址（80/20 法則）
   - 減少資料庫負載

5. **PostgreSQL**
   - 主從架構
   - 主庫：寫入
   - 從庫：讀取（分擔壓力）

---

## 🔍 詳細設計

### 核心挑戰：如何生成唯一的短網址？

有三種主要方案：

---

### 方案 1：哈希函數（MD5/SHA256）

**流程**：
```
long_url → MD5 → 128 bit → 取前 7 位 → Base62 編碼
```

**優點**：
- ✅ 簡單、無需協調
- ✅ 相同 URL 生成相同短碼（天然去重）

**缺點**：
- ❌ 哈希碰撞風險
- ❌ 只取前 7 位碰撞概率更高
- ❌ 需要處理碰撞（重新哈希 or 添加鹽）
- ❌ 短碼可能可預測（安全性差）

**碰撞概率**：
```
生日問題（Birthday Problem）
當有 77,000 個 URL 時，碰撞概率 > 50%
（62^7 的平方根）
```

**判斷**：❌ 不推薦（碰撞處理複雜）

---

### 方案 2：自增 ID + Base62 編碼

**流程**：
```
PostgreSQL AUTO_INCREMENT → 12345 → Base62("12345") → "3D7"
```

**優點**：
- ✅ 簡單、無碰撞
- ✅ 短碼短（數字小時）

**缺點**：
- ❌ 單點故障（單一自增序列）
- ❌ 可預測（安全性差）
- ❌ 暴露業務量（競爭對手可推算）
- ❌ 難以水平擴展

**判斷**：❌ 不推薦（單機限制、可預測）

---

### 方案 3：Snowflake ID + Base62 編碼 ✅ 推薦

**Snowflake ID 結構**（64 bit）：
```
1 bit    41 bit          10 bit     12 bit
unused   timestamp       machine    sequence
  0    |xxxxxxxxxxx|  |xxxxxxxx| |xxxxxxxxxx|
       ↑                ↑           ↑
    毫秒時間戳      機器ID      序列號（同毫秒內自增）
```

**流程**：
```go
snowflakeID := snowflake.Generate()  // 1234567890123
shortCode := base62.Encode(snowflakeID)  // "aB3xD9"
```

**優點**：
- ✅ **分布式**：每台機器獨立生成，無需協調
- ✅ **唯一性**：時間戳 + 機器 ID + 序列號保證唯一
- ✅ **高效能**：本地生成，無 I/O
- ✅ **有序性**：基於時間戳，大致有序（有利於資料庫索引）
- ✅ **不可預測**：Base62 編碼後看起來隨機

**缺點**：
- ❌ 依賴時鐘同步（NTP）
- ❌ 時鐘回撥問題（需要處理）
- ❌ 機器 ID 需要分配

**判斷**：✅ **最佳方案**

---

### Base62 編碼

**為什麼用 Base62 而不是 Base64？**

| 編碼 | 字符集 | 問題 |
|------|-------|------|
| Base64 | A-Z, a-z, 0-9, +, / | `+` 和 `/` 在 URL 中有特殊含義 ❌ |
| Base62 | A-Z, a-z, 0-9 | 安全，無特殊字符 ✅ |

**實現**：
```go
// pkg/base62/base62.go
const base62Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func Encode(num uint64) string {
    if num == 0 {
        return "0"
    }

    result := ""
    for num > 0 {
        result = string(base62Chars[num%62]) + result
        num /= 62
    }
    return result
}

func Decode(str string) uint64 {
    result := uint64(0)
    for _, char := range str {
        result = result*62 + charToNum(char)
    }
    return result
}
```

**示例**：
```
數字: 123456789
Base62: "8M0kX"
長度: 5 位
```

---

### 完整流程

#### 創建短網址

```
1. Client → POST /api/v1/urls {"long_url": "..."}

2. API Server:
   a. 生成 Snowflake ID: 1234567890123
   b. Base62 編碼: "aB3xD9"
   c. 檢查是否已存在（SELECT short_code）
   d. 插入 PostgreSQL: (short_code, long_url, ...)

3. 返回: {"short_url": "https://short.url/aB3xD9"}
```

#### 重定向

```
1. Client → GET /aB3xD9

2. API Server:
   a. 查詢 Redis: GET "url:aB3xD9"
   b. 如果 Hit → 直接返回 302
   c. 如果 Miss:
      - 查詢 PostgreSQL: SELECT long_url WHERE short_code = 'aB3xD9'
      - 寫入 Redis: SET "url:aB3xD9" "https://..."
      - 返回 302

3. 異步更新點擊數（可選）:
   - Kafka → Analytics Service → UPDATE clicks
```

---

## 📈 擴展性討論

### 從 1 到 100 萬 QPS 的演進

#### 階段 1：單機（0 - 1,000 QPS）

```
API Server → PostgreSQL
```

**瓶頸**：資料庫讀寫壓力

---

#### 階段 2：加入快取（1,000 - 10,000 QPS）

```
API Server → Redis (Cache)
           → PostgreSQL
```

**改進**：
- 80% 請求命中快取
- 資料庫壓力降低 80%

---

#### 階段 3：主從複製（10,000 - 50,000 QPS）

```
API Server → Redis
           → PostgreSQL (Master) ← 寫入
           → PostgreSQL (Replica) ← 讀取
```

**改進**：
- 讀寫分離
- 多個從庫分擔讀壓力

---

#### 階段 4：分片（50,000 - 1,000,000+ QPS）

```
                       ┌─> Shard 1 (a-g)
API Server → Redis →  ─┼─> Shard 2 (h-n)
                       └─> Shard 3 (o-z)
```

**分片策略**：
- 按短碼首字母分片
- 或者按哈希（shortCode）% N

**改進**：
- 水平擴展資料庫
- 單個分片壓力更小

---

### 其他優化

1. **CDN**
   - 靜態資源（重定向頁面）
   - 減少延遲

2. **異步寫入**
   - 點擊統計用 Kafka 異步處理
   - 不阻塞重定向

3. **布隆過濾器**
   - 快速判斷短碼是否存在
   - 減少無效查詢

---

## 🤔 常見問題

### Q1: 如何處理自定義短網址？

**問題**：用戶想要 `https://short.url/google` → `https://www.google.com`

**解決**：
1. 檢查自定義 alias 是否已被使用
2. 保留關鍵字（如 api、admin）
3. 限制長度（3-20 字符）
4. 只允許字母、數字、連字符

```go
if isReserved(customAlias) {
    return errors.New("reserved keyword")
}

if exists(customAlias) {
    return errors.New("alias already taken")
}

// 插入時標記為 custom
INSERT INTO urls (short_code, long_url, custom_alias)
VALUES ('google', 'https://www.google.com', TRUE)
```

---

### Q2: 如何防止惡意濫用？

**問題**：
- 用戶短時間創建大量短網址
- 惡意重定向到釣魚網站

**解決**：
1. **限流**：每個 IP/用戶限制每分鐘創建次數
2. **URL 黑名單**：檢查 long_url 是否在黑名單
3. **驗證碼**：高頻用戶需要驗證碼
4. **URL 檢查**：Google Safe Browsing API

---

### Q3: 如何處理過期 URL？

**解決**：
1. **數據庫字段**：`expire_at TIMESTAMP`
2. **定時任務**：每天清理過期 URL
3. **惰性刪除**：訪問時檢查是否過期

```go
if url.ExpireAt != nil && time.Now().After(*url.ExpireAt) {
    return 404, "URL expired"
}
```

---

### Q4: 如何保證高可用？

**解決**：
1. **多數據中心**：部署在不同地區
2. **故障轉移**：Redis 掛了降級到 DB
3. **監控告警**：Prometheus + Grafana
4. **降級開關**：關閉非核心功能（如統計）

---

## 📚 參考資料

- **ByteByteGo**: [System Design Interview – URL Shortener](https://bytebytego.com/)
- **Grokking**: Chapter 2 - Designing a URL Shortening Service
- **DDIA**: Chapter 6 - Partitioning
- **論文**: [Snowflake](https://blog.twitter.com/engineering/en_us/a/2010/announcing-snowflake)

---

## 🚀 快速開始

```bash
# 啟動依賴服務
docker-compose up -d

# 運行服務
go run cmd/server/main.go

# 創建短網址
curl -X POST http://localhost:8080/api/v1/urls \
  -H "Content-Type: application/json" \
  -d '{"long_url": "https://www.example.com"}'

# 訪問短網址
curl -L http://localhost:8080/aB3xD9
```

---

**下一步**：閱讀 [LEARNING.md](./LEARNING.md) 深入學習核心概念！
