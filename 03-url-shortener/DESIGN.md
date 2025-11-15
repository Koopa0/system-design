# URL Shortener 系統設計文檔

## 場景：你是新創公司的技術負責人

### 創辦人的需求

星期三下午，創辦人興奮地走進辦公室：

> **創辦人：** "我們要做一個短網址服務，就像 bit.ly 那樣！現在每次在社群媒體分享連結都太長，用戶體驗很差。我們的產品要簡潔、快速、可靠！"

你查看市場現況：

```
競爭對手分析：
- bit.ly：全球最大短網址服務
- TinyURL：老牌服務，介面簡單
- Rebrandly：支援自訂網域

技術需求：
1. 將長 URL 縮短為短碼（如 https://our.site/aB3xD9）
2. 快速重定向（用戶點擊短碼立即跳轉）
3. 追蹤點擊次數
4. 支援自訂短碼（品牌推廣）

預期規模：
- 第一年：100 萬個 URL
- 每個 URL 平均被點擊：100 次
- 總重定向：1 億次/年 ≈ 1,160 次/秒
```

你陷入思考：

- 如何生成短碼？隨機？自增？Hash？
- 如何確保短碼全局唯一？
- 如何處理高頻讀取（讀寫比 100:1）？
- 如何防止被濫用（SSRF、釣魚）？

### 你會問自己：

1. **短碼如何生成？**
   - 直接用自增 ID？
   - 對 URL 做 Hash？
   - 隨機生成？

2. **短碼要多長？**
   - 越短越好，但容量夠嗎？
   - 6 位？7 位？8 位？

3. **如何應對高並發讀取？**
   - 每秒 1,000+ 次重定向
   - 直接查資料庫可行嗎？

---

## 第一次嘗試：自增 ID + Base62 編碼

### 最直覺的想法

你想：「ID 生成很簡單，資料庫自增就好了！」

```go
// 資料表設計
CREATE TABLE urls (
    id BIGSERIAL PRIMARY KEY,  -- PostgreSQL 自增 ID
    long_url TEXT NOT NULL,
    short_code VARCHAR(10) UNIQUE NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

// 生成短碼邏輯
func ShortenURL(longURL string) (string, error) {
    // 1. 插入資料庫，獲取自增 ID
    var id int64
    err := db.QueryRow(`
        INSERT INTO urls (long_url, short_code)
        VALUES ($1, '')
        RETURNING id
    `, longURL).Scan(&id)

    // 2. 將 ID 編碼為 Base62
    shortCode := base62.Encode(id)  // 如：12345 → "3d7"

    // 3. 更新短碼
    db.Exec(`UPDATE urls SET short_code = $1 WHERE id = $2`, shortCode, id)

    return shortCode, nil
}
```

### Base62 編碼

```
為什麼用 Base62？

Base10（十進制）：0-9
- ID 12345 → "12345"（5 位數字）
- 太長，不適合短 URL

Base64：A-Z, a-z, 0-9, +, /
- ID 12345 → "MDQ="（4 位字符）
- 問題：+ 和 / 在 URL 中需要轉義
  - https://our.site/aB+3x/D9
  - 實際變成：https://our.site/aB%2B3x%2FD9
- 不美觀，用戶體驗差

Base62：A-Z, a-z, 0-9（僅字母和數字）
- ID 12345 → "3d7"（3 位字符）
- URL 友好，無需轉義
- 範例：https://our.site/3d7
```

### 時序範例

```
正常運作：

用戶 A 提交：https://example.com/very/long/url/page1
→ 資料庫插入 → ID = 1
→ Base62 編碼：1 → "1"
→ 返回短碼：https://our.site/1

用戶 B 提交：https://another.com/article/123
→ 資料庫插入 → ID = 2
→ Base62 編碼：2 → "2"
→ 返回短碼：https://our.site/2

用戶 C 提交：第 100 個 URL
→ ID = 100
→ Base62 編碼：100 → "1C"
→ 返回短碼：https://our.site/1C
```

你部署到測試環境，一切運作正常。

### 災難場景：短碼可預測導致安全問題

三週後，安全團隊發現嚴重問題：

```
漏洞報告：

攻擊者發現短碼是連續的：
- https://our.site/1
- https://our.site/2
- https://our.site/3
...

攻擊腳本（Python）：
for i in range(1, 1000000):
    url = f"https://our.site/{base62_encode(i)}"
    response = requests.get(url)
    if response.status_code == 200:
        print(f"ID {i}: {response.url}")

結果：
- 10 分鐘內爬取所有 URL
- 洩露所有用戶提交的連結
- 包含私密文件、內部系統連結

實際案例：
ID 12345 → 短碼 "3d7"
攻擊者猜測：
- ID 12344 → 短碼 "3d6"（上一個）
- ID 12346 → 短碼 "3d8"（下一個）
```

**問題發現：可預測性（Predictability）**

```
問題本質：
自增 ID 是連續的 → Base62 編碼後仍連續
攻擊者可以枚舉所有短碼

視覺化：
ID:         1    2    3    4    5    6    ...
Base62:     1    2    3    4    5    6    ...
可預測性：  完全可預測

風險：
1. 隱私洩露：私密連結被爬取
2. 競爭情報：競爭對手獲取業務數據
3. 安全風險：內部系統連結暴露
```

### 容量分析問題

你繼續分析，發現另一個問題：

```
短碼長度計算：

Base62 每位可表示：62 種字符（0-9, a-z, A-Z）

不同長度的容量：
- 1 位：62¹ = 62
- 2 位：62² = 3,844
- 3 位：62³ = 238,328
- 4 位：62⁴ = 14,776,336
- 5 位：62⁵ = 916,132,832
- 6 位：62⁶ = 56,800,235,584（568 億）
- 7 位：62⁷ = 3,521,614,606,208（3.5 兆）

我們的需求：
- 第一年：100 萬個 URL
- 10 年：3,650 萬個 URL

自增 ID 的短碼長度：
- ID 1-62：短碼 1 位
- ID 63-3,844：短碼 2 位
- ID 3,845-238,328：短碼 3 位
- ID 238,329-14,776,336：短碼 4 位
- ID 14,776,337 以後：短碼 5 位

問題：短碼長度不一致！
- 第 1 個用戶：https://our.site/1（1 位）
- 第 100 萬個用戶：https://our.site/4c92（4 位）
- 第 1000 萬個用戶：https://our.site/1LY7（4 位）

品牌推廣問題：
行銷團隊：「為什麼我們的短碼越來越長？」
```

### 你會問自己：

1. **如何避免可預測性？**
   - 加入隨機性？
   - 打亂順序？

2. **如何保證短碼長度一致？**
   - 固定從某個大數字開始？
   - 預留位數？

3. **多資料庫如何處理？**
   - 兩台資料庫的自增 ID 會衝突嗎？

---

## 第二次嘗試：隨機生成 + 衝突檢查

### 新的想法

你想：「隨機生成就不可預測了！」

```go
const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func GenerateRandomCode(length int) string {
    b := make([]byte, length)
    for i := range b {
        b[i] = charset[rand.Intn(len(charset))]
    }
    return string(b)
}

func ShortenURL(longURL string) (string, error) {
    maxRetries := 3

    for i := 0; i < maxRetries; i++ {
        // 生成 6 位隨機短碼
        shortCode := GenerateRandomCode(6)

        // 檢查是否已存在
        var exists bool
        db.QueryRow(`
            SELECT EXISTS(SELECT 1 FROM urls WHERE short_code = $1)
        `, shortCode).Scan(&exists)

        if !exists {
            // 插入資料庫
            db.Exec(`
                INSERT INTO urls (long_url, short_code)
                VALUES ($1, $2)
            `, longURL, shortCode)
            return shortCode, nil
        }

        // 衝突，重試
    }

    return "", errors.New("failed to generate unique code after retries")
}
```

### 時序範例

```
正常情況：

請求 #1：生成 "aB3xD9"
→ 檢查資料庫：不存在 ✓
→ 插入成功
→ 返回：https://our.site/aB3xD9

請求 #2：生成 "kQ7mN2"
→ 檢查資料庫：不存在 ✓
→ 插入成功
→ 返回：https://our.site/kQ7mN2

短碼特點：
- 完全隨機，無法預測
- 長度固定（6 位）
- 美觀一致
```

看起來完美！你準備上線。

### 災難場景：生日悖論（Birthday Paradox）

半年後，系統開始出現異常：

```
監控告警：
2025-06-15 14:32:15 WARNING: Short code generation retry (attempt 2/3)
2025-06-15 14:32:18 WARNING: Short code generation retry (attempt 3/3)
2025-06-15 14:32:21 ERROR: Failed to generate unique code after 3 retries

問題統計（過去 24 小時）：
- 總請求：10,000 次
- 衝突次數：1,856 次（18.5%！）
- 失敗次數：127 次（1.27%）
- P99 延遲：250 ms（正常應該 < 50 ms）

當前資料：
- 資料庫已有：1,000 萬個短碼
- 6 位 Base62 總容量：568 億
- 理論衝突率：1,000 萬 / 568 億 = 0.017%（很低）
- 實際衝突率：18.5%（很高！）

為什麼？
```

**問題發現：生日悖論（Birthday Paradox）**

```
生日悖論：
問題：23 個人中，至少兩人生日相同的機率是多少？
直覺：很低（365 天，只有 23 人）
實際：50.7%（超過一半！）

原理：
不是「某個人」與「特定人」生日相同
而是「任意兩人」生日相同
組合數：C(23,2) = 253 對

套用到短碼：
- 總容量：N = 62⁶ = 568 億
- 已有短碼：n = 1,000 萬
- 衝突機率：1 - e^(-n²/(2N))

計算：
n = 10,000,000
N = 56,800,235,584
衝突機率 = 1 - e^(-(10,000,000)² / (2 × 56,800,235,584))
         = 1 - e^(-0.88)
         = 58.5%

接近六成的機率至少有一對短碼衝突！
```

### 效能問題

```
每次生成的開銷：

第一次嘗試：
- 生成隨機碼：0.01 ms
- 查詢資料庫：5 ms（B-Tree 索引查詢）
- 插入資料庫：3 ms
- 總計：8 ms

衝突時（18.5% 機率）：
- 第一次嘗試：8 ms（衝突）
- 第二次嘗試：8 ms（可能再次衝突）
- 第三次嘗試：8 ms
- 最壞情況：24 ms

平均延遲計算：
- 無衝突（81.5%）：8 ms
- 1 次衝突（18.5% × 81.5%）：16 ms
- 2 次衝突（18.5% × 18.5%）：24 ms
- 平均：約 10 ms

問題：
1. 資料庫壓力：每次都要查詢（衝突時多次）
2. 延遲不穩定：P99 可能達到 24 ms
3. 失敗風險：3 次都衝突時報錯
```

### 擴展問題

```
分散式環境下：

假設有 3 台 API Server：

Server 1：生成 "aB3xD9" → 檢查不存在 → 準備插入
Server 2：生成 "aB3xD9" → 檢查不存在 → 準備插入
Server 3：生成 "kQ7mN2" → 檢查不存在 → 準備插入

時間軸：
T0: Server 1 檢查 "aB3xD9" → 不存在
T1: Server 2 檢查 "aB3xD9" → 不存在（Server 1 還沒插入）
T2: Server 1 插入 "aB3xD9" → 成功
T3: Server 2 插入 "aB3xD9" → 失敗！（唯一約束衝突）

問題：競態條件（Race Condition）
即使資料庫有唯一約束，仍會浪費資源
```

### 你會問自己：

1. **如何避免衝突檢查？**
   - 能否生成「保證唯一」的 ID？

2. **如何避免資料庫壓力？**
   - 不要每次都查詢

3. **如何應對分散式環境？**
   - 多台伺服器如何協調？

---

## 靈感：Twitter 的 Snowflake

你想起 Twitter 開源的 Snowflake 算法：

```
Snowflake ID 結構（64 bit）：

+--------------------------------------------------------------------------+
| 1 bit    | 41 bit           | 10 bit     | 12 bit      |
| 符號位   | 時間戳（毫秒）    | 機器 ID    | 序列號      |
| (未使用) | (可用 69 年)     | (1024 台)  | (4096/毫秒) |
+--------------------------------------------------------------------------+

範例：
二進制：0001100100010101011010101010101010101010101000000001100100101011
        ↑         ↑                     ↑          ↑
     符號位     時間戳                機器ID      序列號

十進制：1234567890123456789
Base62: 8M0kX（7 位字符）
```

**關鍵洞察：**
- 時間戳 → 保證趨勢遞增（不同時間生成的 ID 不同）
- 機器 ID → 保證不同機器生成的 ID 不同
- 序列號 → 保證同一毫秒內生成的 ID 不同
- 組合結果 → **全局唯一，無需協調**

這就是 **分散式唯一 ID 生成器**！

---

## 最終方案：Snowflake ID + Base62

### 設計思路

```
架構：
1. 每台 API Server 配置唯一的 Machine ID（0-1023）
2. 本地生成 Snowflake ID（無需查詢資料庫）
3. Base62 編碼為短碼
4. 直接插入資料庫（無需檢查衝突）

資料流：
用戶請求 → API Server
           ↓
        Snowflake 生成器（本地）
           ↓
        ID: 1234567890123456789
           ↓
        Base62 編碼
           ↓
        短碼: "8M0kX"
           ↓
        PostgreSQL（直接插入，無檢查）
           ↓
        返回：https://our.site/8M0kX
```

### Snowflake 實現

```go
type SnowflakeGenerator struct {
    machineID      int64  // 10 bit，範圍 0-1023
    sequence       int64  // 12 bit，範圍 0-4095
    lastTimestamp  int64

    epoch int64  // 起始時間戳（如 2024-01-01 00:00:00）
    mu    sync.Mutex
}

func (g *SnowflakeGenerator) Generate() int64 {
    g.mu.Lock()
    defer g.mu.Unlock()

    // 獲取當前時間戳（毫秒）
    now := time.Now().UnixMilli()

    if now < g.lastTimestamp {
        // 時鐘回撥，拒絕生成
        panic("clock moved backwards")
    }

    if now == g.lastTimestamp {
        // 同一毫秒內，序列號 +1
        g.sequence = (g.sequence + 1) & 0xFFF  // 12 bit mask

        if g.sequence == 0 {
            // 序列號用完，等待下一毫秒
            for now <= g.lastTimestamp {
                now = time.Now().UnixMilli()
            }
        }
    } else {
        // 新的毫秒，序列號重置
        g.sequence = 0
    }

    g.lastTimestamp = now

    // 組裝 64-bit ID
    timestamp := (now - g.epoch) << 22  // 41 bit 時間戳
    machine := g.machineID << 12        // 10 bit 機器 ID
    sequence := g.sequence              // 12 bit 序列號

    return timestamp | machine | sequence
}
```

### Base62 編碼

```go
const base62Chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func EncodeBase62(num int64) string {
    if num == 0 {
        return "0"
    }

    result := ""
    for num > 0 {
        remainder := num % 62
        result = string(base62Chars[remainder]) + result
        num = num / 62
    }
    return result
}

func DecodeBase62(str string) int64 {
    var num int64
    for _, char := range str {
        num = num * 62

        if char >= '0' && char <= '9' {
            num += int64(char - '0')
        } else if char >= 'a' && char <= 'z' {
            num += int64(char - 'a' + 10)
        } else if char >= 'A' && char <= 'Z' {
            num += int64(char - 'A' + 36)
        }
    }
    return num
}
```

### 完整流程

```go
func ShortenURL(longURL string) (string, error) {
    // 1. 生成 Snowflake ID（本地，無需資料庫）
    id := snowflakeGen.Generate()
    // 範例：1234567890123456789

    // 2. Base62 編碼
    shortCode := base62.Encode(id)
    // 範例："8M0kX"（7 位）

    // 3. 直接插入資料庫（無需檢查）
    _, err := db.Exec(`
        INSERT INTO urls (id, long_url, short_code, created_at)
        VALUES ($1, $2, $3, NOW())
    `, id, longURL, shortCode)

    if err != nil {
        return "", err
    }

    return shortCode, nil
}
```

### 時序範例

```
高並發場景（3 台 API Server，同一毫秒）：

時刻：2025-01-15 10:00:00.123

Server 1 (machineID=0)：
→ 生成 ID: 1111111111000000000000000000（時間戳相同，機器 ID=0，序列=0）
→ Base62: "aB3xD9"
→ 插入資料庫 ✓

Server 2 (machineID=1)：
→ 生成 ID: 1111111111000010000000000000（時間戳相同，機器 ID=1，序列=0）
→ Base62: "aB3xE1"
→ 插入資料庫 ✓

Server 3 (machineID=2)：
→ 生成 ID: 1111111111000100000000000000（時間戳相同，機器 ID=2，序列=0）
→ Base62: "aB3xF8"
→ 插入資料庫 ✓

結果：三個不同的短碼，無衝突！

同一台伺服器，同一毫秒內生成多個：
Server 1 (machineID=0)：
→ 請求 #1：序列號=0 → ID 結尾 ...000 → "aB3xD9"
→ 請求 #2：序列號=1 → ID 結尾 ...001 → "aB3xDa"
→ 請求 #3：序列號=2 → ID 結尾 ...010 → "aB3xDb"
...
→ 請求 #4096：序列號=4095 → ID 結尾 ...FFF → 等待下一毫秒
```

### 為什麼這是最佳選擇？

對比所有方案：

| 特性 | 自增 ID | 隨機生成 | Hash(URL) | Snowflake ID |
|------|---------|---------|-----------|-------------|
| 全局唯一 | 單機可保證 | 需檢查衝突 | 需處理衝突 | 演算法保證 |
| 可預測性 | 完全可預測 | 不可預測 | 可預測 | 趨勢遞增 |
| 生成速度 | 需查 DB | 需查 DB | 本地計算 | 本地計算 |
| 資料庫壓力 | 寫入 | 查詢 + 寫入 | 查詢 + 寫入 | 僅寫入 |
| 分散式友好 | 需協調 | 競態條件 | 競態條件 | 無需協調 |
| 短碼長度 | 不固定 | 固定 | 固定 | 固定 |
| 擴展性 | 差（單點） | 中等 | 中等 | 優秀 |

**勝出原因：**
1. 全局唯一：時間戳 + 機器 ID + 序列號組合
2. 高效能：本地生成，無需查詢資料庫
3. 無衝突：演算法保證，不需要重試
4. 可擴展：支援 1024 台機器，每毫秒 4096 個 ID
5. 趨勢遞增：有利於資料庫 B-Tree 索引效能

---

## 新挑戰：高頻讀取最佳化

### 場景升級

產品上線三個月後，創辦人很興奮：

> **創辦人：** "我們現在每天有 100 萬次重定向！但我注意到有時候點擊短碼會延遲 1-2 秒，能優化嗎？"

你查看監控數據：

```
重定向效能（過去 24 小時）：
- 總請求：1,000,000 次
- QPS：平均 12，尖峰 150
- P50 延遲：8 ms
- P95 延遲：45 ms
- P99 延遲：1,200 ms（超過 1 秒！）

資料庫查詢日誌：
2025-04-15 14:32:15 SELECT long_url FROM urls WHERE short_code = 'aB3xD9' (12 ms)
2025-04-15 14:32:16 SELECT long_url FROM urls WHERE short_code = 'kQ7mN2' (8 ms)
2025-04-15 14:32:17 SELECT long_url FROM urls WHERE short_code = 'aB3xD9' (11 ms)
...

問題發現：
同一個短碼 "aB3xD9" 在 10 分鐘內被查詢 523 次
每次都打資料庫！
```

### 第一次想法：全部放 Redis

最簡單的想法：

```
方案：所有 URL 都存在 Redis

配置：
- 資料庫：100 萬個 URL
- 平均長度：100 bytes
- 總記憶體：100 MB（看起來不大）

實現：
redis.Set("short:aB3xD9", "https://example.com/long/url", 0)  // TTL=0 永不過期
```

看起來很合理！

### 問題分析

```
一年後的情況：

資料增長：
- 第一年：100 萬個 URL × 100 bytes = 100 MB
- 第二年：200 萬個 URL × 100 bytes = 200 MB
- 第五年：500 萬個 URL × 100 bytes = 500 MB
- 第十年：1,000 萬個 URL × 100 bytes = 1 GB

實際記憶體使用（Redis overhead）：
- 每個 key-value 額外開銷：約 100 bytes
- 實際使用：1,000 萬 × (100 + 100) = 2 GB

成本分析（AWS ElastiCache）：
- 2 GB Redis：約 $50/月
- 10 GB Redis（預留空間）：約 $150/月

問題：
1. 記憶體成本持續增長
2. 80% 的 URL 很少被訪問（冷資料浪費記憶體）
3. Redis 重啟風險（雖然有持久化，但恢復慢）
```

### 解決方案：Cache-Aside 模式

你意識到：

> "應該只快取熱門 URL！"

```go
func Redirect(ctx context.Context, shortCode string) (string, error) {
    cacheKey := "short:" + shortCode

    // 1. 先查 Redis
    longURL, err := redisClient.Get(ctx, cacheKey).Result()
    if err == nil {
        // 快取命中
        return longURL, nil
    }

    // 2. 快取未命中，查資料庫
    var url URL
    err = db.QueryRow(`
        SELECT long_url FROM urls WHERE short_code = $1
    `, shortCode).Scan(&url.LongURL)

    if err != nil {
        return "", err
    }

    // 3. 寫入 Redis（非同步，不阻塞回應）
    go func() {
        // TTL = 24 小時 ± 1 小時（隨機，防雪崩）
        ttl := 24*time.Hour + time.Duration(rand.Intn(3600))*time.Second
        redisClient.Set(context.Background(), cacheKey, url.LongURL, ttl)
    }()

    return url.LongURL, nil
}
```

### 80/20 法則

```
Pareto 原理（帕累托法則）：
80% 的流量來自 20% 的 URL

實際數據（我們的系統）：
- 總 URL：100 萬個
- 總重定向：1 億次
- 熱門 URL（前 20%）：20 萬個
- 熱門 URL 流量：8,000 萬次（80%）

快取策略：
- 僅快取被訪問過的 URL（自動篩選熱門）
- TTL = 24 小時（不活躍的 URL 自動過期）
- 預期快取大小：約 20-30 萬個 URL

記憶體使用：
- 30 萬個 URL × 200 bytes = 60 MB
- 比全量快取省 95% 記憶體！
```

### 時序範例

```
場景：熱門 URL 被多次訪問

第一次訪問：
10:00:00.000 → 請求 /aB3xD9
10:00:00.001 → 查 Redis → 未命中
10:00:00.002 → 查 PostgreSQL → 命中（8 ms）
10:00:00.010 → 返回 long_url
10:00:00.011 → 非同步寫入 Redis（TTL=24h）

第二次訪問（10 秒後）：
10:00:10.000 → 請求 /aB3xD9
10:00:10.001 → 查 Redis → 命中！（< 1 ms）
10:00:10.002 → 返回 long_url

第三次訪問（1 分鐘後）：
10:01:00.000 → 請求 /aB3xD9
10:01:00.001 → 查 Redis → 命中！（< 1 ms）
10:01:00.002 → 返回 long_url

...（後續 500 次訪問都命中快取）

場景：冷門 URL 只被訪問一次

11:00:00.000 → 請求 /xY9zW1
11:00:00.001 → 查 Redis → 未命中
11:00:00.002 → 查 PostgreSQL → 命中（8 ms）
11:00:00.010 → 返回 long_url
11:00:00.011 → 寫入 Redis（TTL=24h）

次日（24 小時後）：
11:00:00.000 → Redis TTL 過期，key 自動刪除
結果：冷門 URL 不佔用長期記憶體
```

### 效能對比

```
純資料庫方案：
- 每次請求：查詢 PostgreSQL（5-10 ms）
- 1,000 次請求：1,000 次資料庫查詢
- 資料庫負載：高

Cache-Aside 方案（假設 80% 命中率）：
- 快取命中（80%）：查 Redis（< 1 ms）
- 快取未命中（20%）：查 PostgreSQL（5-10 ms）+ 寫 Redis
- 1,000 次請求：800 次 Redis + 200 次 PostgreSQL
- 資料庫負載：降低 80%

延遲分析：
- P50（中位數）：< 1 ms（快取命中）
- P95：< 1 ms（仍是快取）
- P99：8 ms（少數未命中）
- 最壞情況：10 ms（資料庫慢查詢）
```

---

## 新挑戰：快取穿透攻擊

### 災難場景

上線半年後，凌晨 3 點收到告警：

```
監控告警：
03:00:00 → PostgreSQL CPU: 95%
03:00:05 → PostgreSQL 慢查詢激增
03:00:10 → API 回應延遲 P99: 5,000 ms

查看日誌：
03:00:00.123 GET /abc123 → 404 (資料庫查詢 8 ms)
03:00:00.145 GET /xyz789 → 404 (資料庫查詢 9 ms)
03:00:00.167 GET /qwe456 → 404 (資料庫查詢 8 ms)
...
03:00:10.000 累計 10,000 次 404 請求

攻擊分析：
所有請求都是不存在的短碼
→ Redis 無快取（因為不存在）
→ 每次都查資料庫
→ 資料庫壓力暴增
```

**問題發現：快取穿透（Cache Penetration）**

```
問題本質：
查詢不存在的 key → 快取無效 → 每次都打資料庫

攻擊腳本範例：
import random
import string
import requests

while True:
    # 生成隨機短碼（不存在）
    code = ''.join(random.choices(string.ascii_letters, k=6))
    requests.get(f"https://our.site/{code}")

效果：
- 每秒生成 1,000 個隨機短碼
- 每個都查詢資料庫
- 資料庫負載爆炸
```

### 方案 1：快取空值

```go
func Redirect(ctx context.Context, shortCode string) (string, error) {
    cacheKey := "short:" + shortCode

    // 查 Redis
    longURL, err := redisClient.Get(ctx, cacheKey).Result()
    if err == nil {
        if longURL == "NULL" {
            // 快取的空值
            return "", errors.New("not found")
        }
        return longURL, nil
    }

    // 查資料庫
    var url URL
    err = db.QueryRow(`
        SELECT long_url FROM urls WHERE short_code = $1
    `, shortCode).Scan(&url.LongURL)

    if err != nil {
        // 不存在，快取空值（TTL 短一點，如 5 分鐘）
        redisClient.Set(ctx, cacheKey, "NULL", 5*time.Minute)
        return "", errors.New("not found")
    }

    // 正常快取
    redisClient.Set(ctx, cacheKey, url.LongURL, 24*time.Hour)
    return url.LongURL, nil
}
```

**問題：攻擊者仍可生成大量無效 key，佔用記憶體**

### 方案 2：Bloom Filter（布隆過濾器）

你想起資料結構課學過的 Bloom Filter：

```
Bloom Filter 原理：

特點：
- 空間效率極高
- 可能誤判（說存在但實際不存在）
- 不會漏判（說不存在就一定不存在）

結構：
一個 bit 陣列 + 多個 hash 函數

新增元素：
hash1(element) = 3 → bits[3] = 1
hash2(element) = 7 → bits[7] = 1
hash3(element) = 11 → bits[11] = 1

檢查元素：
hash1(element) = 3 → bits[3] == 1? ✓
hash2(element) = 7 → bits[7] == 1? ✓
hash3(element) = 11 → bits[11] == 1? ✓
→ 可能存在（繼續查 Redis/DB）

hash1(element) = 5 → bits[5] == 0? ✗
→ 一定不存在（直接返回 404）
```

### 實現（教學簡化版）

```go
// 生產環境建議使用 Redis Bloom Filter 模組
// 這裡展示概念

type BloomFilter struct {
    bits []bool
    size int
    hashFuncs int
}

func (bf *BloomFilter) Add(shortCode string) {
    for i := 0; i < bf.hashFuncs; i++ {
        hash := bf.hash(shortCode, i) % bf.size
        bf.bits[hash] = true
    }
}

func (bf *BloomFilter) MayContain(shortCode string) bool {
    for i := 0; i < bf.hashFuncs; i++ {
        hash := bf.hash(shortCode, i) % bf.size
        if !bf.bits[hash] {
            return false  // 一定不存在
        }
    }
    return true  // 可能存在
}

func Redirect(ctx context.Context, shortCode string) (string, error) {
    // 1. 先檢查 Bloom Filter
    if !bloomFilter.MayContain(shortCode) {
        // 一定不存在，直接返回 404
        return "", errors.New("not found")
    }

    // 2. 可能存在，查 Redis
    cacheKey := "short:" + shortCode
    longURL, err := redisClient.Get(ctx, cacheKey).Result()
    if err == nil {
        return longURL, nil
    }

    // 3. 查資料庫
    var url URL
    err = db.QueryRow(`
        SELECT long_url FROM urls WHERE short_code = $1
    `, shortCode).Scan(&url.LongURL)

    if err != nil {
        // 誤判（Bloom Filter 說存在但實際不存在）
        // 機率很低（< 1%）
        return "", errors.New("not found")
    }

    redisClient.Set(ctx, cacheKey, url.LongURL, 24*time.Hour)
    return url.LongURL, nil
}
```

### Bloom Filter 容量計算

```
參數設計：
- 預期元素數量：n = 1,000 萬（10 年積累）
- 期望誤判率：p = 0.01（1%）

計算公式：
bit 陣列大小：m = -n × ln(p) / (ln(2)²)
hash 函數數量：k = (m/n) × ln(2)

代入數值：
m = -(10,000,000) × ln(0.01) / (ln(2)²)
  = 10,000,000 × 4.6 / 0.48
  = 95,850,000 bits
  = 11.5 MB

k = (95,850,000 / 10,000,000) × ln(2)
  = 9.585 × 0.693
  = 7 個 hash 函數

結論：
- 記憶體：11.5 MB（相比 1,000 萬個 URL 的 1 GB，省 99%）
- 誤判率：1%（可接受）
- 攻擊防護：完全阻擋不存在的短碼
```

### 效果對比

```
場景：攻擊者發送 10,000 個不存在的短碼

不使用 Bloom Filter：
- 10,000 次 Redis 查詢（未命中）
- 10,000 次 PostgreSQL 查詢
- 資料庫負載爆炸

使用 Bloom Filter：
- 10,000 次 Bloom Filter 檢查（記憶體，微秒級）
- 100 次 PostgreSQL 查詢（1% 誤判）
- 資料庫負載降低 99%
```

---

## 新挑戰：SSRF 安全問題

### 災難場景

安全團隊發現嚴重漏洞：

```
漏洞報告：SSRF (Server-Side Request Forgery)

攻擊者提交：
POST /shorten
{
  "long_url": "http://169.254.169.254/latest/meta-data/iam/security-credentials/"
}

系統回應：
{
  "short_code": "aB3xD9",
  "short_url": "https://our.site/aB3xD9"
}

攻擊者訪問短碼：
GET /aB3xD9
→ 重定向到：http://169.254.169.254/latest/meta-data/...
→ 瀏覽器顯示：AWS IAM credentials

危害：
- 洩露 AWS 臨時憑證
- 攻擊者可以操作 AWS 資源
- 可能造成資料外洩、資源盜用
```

### 其他攻擊向量

```
攻擊類型 1：內網掃描
POST /shorten {"long_url": "http://192.168.1.1/admin"}
POST /shorten {"long_url": "http://10.0.0.5:3306"}
POST /shorten {"long_url": "http://172.16.0.10:22"}
→ 繞過防火牆存取內部服務

攻擊類型 2：XSS
POST /shorten {"long_url": "javascript:alert(document.cookie)"}
→ 用戶點擊短碼時執行惡意腳本

攻擊類型 3：本地檔案讀取
POST /shorten {"long_url": "file:///etc/passwd"}
→ 嘗試讀取伺服器檔案

攻擊類型 4：DNS Rebinding
1. 攻擊者控制 DNS：evil.com
2. 提交時 DNS 解析：evil.com → 8.8.8.8（公網，通過驗證）
3. 用戶訪問時 DNS 解析：evil.com → 192.168.1.1（內網）
→ TOCTOU (Time-of-Check-Time-of-Use) 攻擊
```

### 防護方案

```go
// 私有 IP 範圍
var privateIPBlocks = []net.IPNet{
    {IP: net.IPv4(10, 0, 0, 0), Mask: net.IPv4Mask(255, 0, 0, 0)},         // 10.0.0.0/8
    {IP: net.IPv4(172, 16, 0, 0), Mask: net.IPv4Mask(255, 240, 0, 0)},     // 172.16.0.0/12
    {IP: net.IPv4(192, 168, 0, 0), Mask: net.IPv4Mask(255, 255, 0, 0)},    // 192.168.0.0/16
    {IP: net.IPv4(127, 0, 0, 0), Mask: net.IPv4Mask(255, 0, 0, 0)},        // 127.0.0.0/8
    {IP: net.IPv4(169, 254, 0, 0), Mask: net.IPv4Mask(255, 255, 0, 0)},    // 169.254.0.0/16 (AWS metadata)
}

func ValidateURL(urlStr string) error {
    // 1. 解析 URL
    u, err := url.Parse(urlStr)
    if err != nil {
        return fmt.Errorf("invalid URL format")
    }

    // 2. 檢查 scheme（僅允許 http/https）
    if u.Scheme != "http" && u.Scheme != "https" {
        return fmt.Errorf("only http/https allowed, got: %s", u.Scheme)
    }

    // 3. 解析主機名
    host := u.Hostname()

    // 4. DNS 解析
    ips, err := net.LookupIP(host)
    if err != nil {
        return fmt.Errorf("DNS resolution failed")
    }

    // 5. 檢查所有解析結果（防止部分 IP 是私有）
    for _, ip := range ips {
        if isPrivateIP(ip) {
            return fmt.Errorf("private IP not allowed: %s", ip)
        }
    }

    return nil
}

func isPrivateIP(ip net.IP) bool {
    // 檢查是否為私有 IP
    for _, block := range privateIPBlocks {
        if block.Contains(ip) {
            return true
        }
    }

    // 檢查 localhost
    if ip.IsLoopback() {
        return true
    }

    // 檢查 link-local
    if ip.IsLinkLocalUnicast() {
        return true
    }

    return false
}
```

### 時序範例

```
合法 URL：

POST /shorten {"long_url": "https://example.com/article"}
→ 解析：scheme=https, host=example.com
→ DNS 解析：93.184.216.34（公網 IP）
→ 檢查：非私有 IP ✓
→ 通過驗證，生成短碼

惡意 URL（內網）：

POST /shorten {"long_url": "http://192.168.1.1/admin"}
→ 解析：scheme=http, host=192.168.1.1
→ DNS 解析：192.168.1.1
→ 檢查：私有 IP ✗
→ 拒絕：400 Bad Request "private IP not allowed"

惡意 URL（AWS metadata）：

POST /shorten {"long_url": "http://169.254.169.254/latest/meta-data"}
→ 解析：scheme=http, host=169.254.169.254
→ DNS 解析：169.254.169.254
→ 檢查：link-local IP ✗
→ 拒絕：400 Bad Request "private IP not allowed"

惡意 URL（XSS）：

POST /shorten {"long_url": "javascript:alert(1)"}
→ 解析：scheme=javascript
→ 檢查：非 http/https ✗
→ 拒絕：400 Bad Request "only http/https allowed"
```

### 已知限制（教學標註）

```
未實現的防護（生產環境應加強）：

1. DNS Rebinding 防護
   問題：驗證時 DNS → 公網 IP，訪問時 DNS → 私有 IP
   方案：
   - 固定 DNS 解析結果（快取 IP）
   - 重定向時禁止 DNS 查詢，直接用快取 IP
   - 或：禁止重定向（301/302）

2. HTTP 重定向攻擊
   問題：https://evil.com → 重定向到 http://192.168.1.1
   方案：
   - 禁止跟隨重定向
   - 或：檢查重定向目標 URL

3. IPv6 私有範圍
   問題：未檢查 IPv6 私有地址（如 fc00::/7）
   方案：
   - 增加 IPv6 私有範圍檢查

4. URL 黑名單
   問題：已知釣魚網站未阻擋
   方案：
   - 維護釣魚網站黑名單
   - 整合 Google Safe Browsing API
```

---

## 擴展性分析

### 當前架構容量

```
單機配置：
├─ API Server (4 core, 8 GB)
│  ├─ Snowflake ID 生成：本地，無瓶頸
│  └─ 處理能力：約 5,000 QPS
│
├─ Redis (16 GB)
│  ├─ 讀取 QPS：100,000+
│  ├─ 快取容量：約 30 萬個 URL（熱門資料）
│  └─ Bloom Filter：11.5 MB
│
└─ PostgreSQL (4 core, 100 GB SSD)
   ├─ 寫入：12 QPS（輕鬆應對）
   ├─ 讀取：約 200 QPS（快取未命中 20%）
   └─ 儲存：1,000 萬個 URL ≈ 5 GB

效能分析：
- 寫入（生成短碼）：12 QPS
  - Snowflake 生成：< 0.1 ms
  - 資料庫插入：5 ms
  - 總延遲：< 10 ms

- 讀取（重定向）：1,160 QPS
  - Bloom Filter：< 0.01 ms（100% 請求）
  - Redis 命中（80%）：< 1 ms
  - PostgreSQL 未命中（20%）：5 ms
  - 平均延遲：約 2 ms

結論：當前架構足夠
```

### 10 倍擴展（10,000 讀取 QPS）

**瓶頸分析：**
```
API Server：
- 當前：1 台，約 5,000 QPS
- 需求：10,000 QPS
- 結論：需要 2-3 台

Redis：
- 當前：100K+ QPS 能力
- 需求：8,000 QPS（80% 命中）
- 結論：單機足夠

PostgreSQL：
- 當前：約 1,000 QPS 讀取能力
- 需求：2,000 QPS（20% 未命中）
- 結論：接近瓶頸
```

**方案：API Server 水平擴展 + PostgreSQL 讀寫分離**

```
架構升級：

API Server：
- 3 台無狀態實例（machineID 0-2）
- Nginx 負載平衡
- 每台：約 3,300 QPS
- 總容量：10,000 QPS

Redis：
- 單機（無需改變）
- 快取命中：8,000 QPS

PostgreSQL：
- 1 主（寫入）+ 2 從（讀取）
- 主：12 QPS 寫入
- 從：各 1,000 QPS 讀取
- 總讀取容量：2,000 QPS

配置範例：
// 讀寫分離
func Redirect(ctx context.Context, shortCode string) (string, error) {
    // ...（快取邏輯）

    // 查詢使用從庫（讀副本）
    err = replicaDB.QueryRow(`
        SELECT long_url FROM urls WHERE short_code = $1
    `, shortCode).Scan(&url.LongURL)

    return url.LongURL, nil
}

成本：
- API Server：3 × $100 = $300/月
- Redis：$100/月
- PostgreSQL：1 主 + 2 從 = $400/月
- Nginx：$50/月
- 總計：約 $850/月
```

### 100 倍擴展（100,000 讀取 QPS）

**需要架構升級：**

```
1. API Server 集群（10 台）
   - machineID 0-9
   - AWS ALB 負載平衡
   - 自動擴展（根據 CPU/QPS）
   - 總容量：50,000 QPS

2. Redis Cluster（分片）
   - 8 個 master（按 shortCode hash）
   - 每個：12,500 QPS（80,000 快取命中 / 8）
   - 總容量：100,000 QPS
   - Bloom Filter 複製到每個節點

3. PostgreSQL 集群（分片）
   - 8 個 shard（按 shortCode hash，與 Redis 對齊）
   - 每個 shard：1 主 + 2 從
   - 每個從庫：2,500 QPS（20,000 未命中 / 8）
   - 總容量：20,000 QPS

4. 應用層路由
   func getShard(shortCode string) int {
       hash := crc32.ChecksumIEEE([]byte(shortCode))
       return int(hash % 8)
   }

   func Redirect(shortCode string) (string, error) {
       shard := getShard(shortCode)
       redis := redisCluster[shard]
       db := pgCluster[shard]

       // ...（查詢邏輯）
   }

5. CDN 整合（可選）
   - CloudFlare / AWS CloudFront
   - 快取熱門短碼的重定向回應
   - 減少 API Server 壓力
   - 全球低延遲

架構圖：
Client
  ↓
CDN (optional, 快取熱門短碼)
  ↓
Load Balancer (AWS ALB)
  ↓
├─ API Server 0-9 (machineID 0-9)
   ↓
   ├─ Redis Cluster (8 shards)
   │  └─ Bloom Filter (每個節點)
   └─ PostgreSQL Cluster (8 shards)
      ├─ Shard 0: 1 主 + 2 從
      ├─ Shard 1: 1 主 + 2 從
      └─ ...

成本估算（AWS）：
- API Servers：10 × $100 = $1,000/月
- Redis Cluster：8 × $150 = $1,200/月
- PG Cluster：8 × (1 主 + 2 從) × $150 = $3,600/月
- Load Balancer：$100/月
- CDN：$500/月（可選）
- 總計：約 $6,400/月（不含 CDN）
```

---

## 真實工業案例

### Bitly（全球最大短網址服務）

```
技術選型：
- ID 生成：自研分散式 ID 生成器（類似 Snowflake）
- 資料庫：MongoDB（分片）
- 快取：Redis Cluster
- CDN：Fastly

規模：
- 每月 60 億次點擊
- 每秒約 2,300 次重定向
- 儲存數十億個連結

架構特點：
- 地理分佈：全球多個資料中心
- 智慧路由：根據地理位置選擇最近節點
- 即時分析：點擊數據即時處理（Kafka + Spark）

為什麼選擇：
- MongoDB 水平擴展能力強
- Redis Cluster 支援大規模快取
- CDN 降低全球延遲
```

### TinyURL（老牌短網址服務）

```
技術選型：
- ID 生成：自增 ID + Base62（早期架構）
- 資料庫：MySQL（主從複製）
- 快取：Memcached

架構特點：
- 簡單可靠：單一資料中心
- 低成本：自增 ID 無需協調
- 功能單純：專注短網址核心功能

為什麼選擇：
- 早期流量不大，單機足夠
- MySQL 成熟穩定
- 自增 ID 實現簡單
```

### Google Firebase Dynamic Links

```
技術選型：
- ID 生成：Google 內部分散式 ID 系統
- 資料庫：Cloud Firestore（Google 自研 NoSQL）
- CDN：Google Cloud CDN

架構特點：
- 深度整合：與 Firebase Analytics 整合
- App 友好：支援 iOS/Android deep linking
- 智慧重定向：根據設備類型跳轉（App 或網頁）

為什麼選擇：
- Google 基礎設施優勢
- Firestore 全球分佈
- 與 Firebase 生態系統整合
```

---

## 實現範圍標註

### 已實現（核心教學內容）

| 功能 | 檔案 | 教學重點 |
|------|------|----------|
| **Snowflake ID** | `snowflake/snowflake.go:62-152` | 分散式 ID 生成、位運算、時鐘回撥處理 |
| **Base62 編碼** | `base62/base62.go` | 進制轉換、編碼解碼 |
| **SSRF 防護** | `shorten.go:152-256` | 私有 IP 檢查、DNS 解析、scheme 驗證 |
| **Cache-Aside** | `redirect.go` | Redis 快取、資料庫回源、TTL 設置 |
| **唯一約束** | `postgres.go` | 短碼唯一性、資料庫索引 |

### 教學簡化（未實現）

| 功能 | 原因 | 生產環境建議 |
|------|------|-------------|
| **Bloom Filter** | 增加複雜度，聚焦核心流程 | Redis Bloom Filter 模組，防快取穿透 |
| **DNS Rebinding 防護** | 已知安全限制 | 固定 IP、禁止重定向 |
| **URL 黑名單** | 簡化安全功能 | Google Safe Browsing API、用戶舉報 |
| **點擊分析** | 聚焦核心功能 | 地理位置、設備分析、來源追蹤 |
| **自訂短碼** | 簡化業務邏輯 | 品牌短碼、衝突檢查、付費功能 |

### 生產環境額外需要

```
1. 安全加固
   - URL 黑名單：釣魚網站、惡意網站資料庫
   - 用戶舉報：標記可疑短碼
   - 訪問警告：顯示原始 URL（如 bit.ly 預覽頁）
   - 速率限制：防止批量生成攻擊
   - CAPTCHA：防止自動化濫用

2. 點擊分析
   - 來源追蹤：Referer、UTM 參數
   - 地理位置：根據 IP 解析國家/城市
   - 設備分析：User-Agent 解析（iOS/Android/Desktop）
   - 時間分布：按小時/天/週統計
   - 轉換率：配合業務指標（如購買轉換）

3. 快取最佳化
   - Bloom Filter：防快取穿透（11.5 MB）
   - 熱門 URL 永不過期：點擊數 > 10,000 的 URL
   - 預熱機制：啟動時載入 Top 1000 熱門 URL
   - 多層快取：L1（本地記憶體）+ L2（Redis）

4. 監控告警
   - QPS：寫入/讀取 QPS、趨勢預測
   - 延遲：P50/P95/P99 百分位數
   - 快取命中率：目標 > 80%
   - 錯誤率：404 率、SSRF 攔截率
   - Bloom Filter 誤判率：< 1%

5. 業務功能
   - 自訂短碼：bit.ly/my-brand-link
   - 短碼編輯：更新長 URL（需失效快取）
   - 短碼刪除：軟刪除（deleted_at）
   - 訪問密碼：私密短碼保護
   - 二維碼生成：短碼 → QR Code
   - 批量生成：CSV 上傳批量縮短
   - 過期時間：限時短碼（活動推廣）
```

---

## 你學到了什麼？

### 1. 從錯誤中學習

```
錯誤方案的價值：

方案 A：自增 ID + Base62
發現：可預測性導致安全問題，短碼長度不一致
教訓：安全和用戶體驗同樣重要

方案 B：隨機生成 + 衝突檢查
發現：生日悖論導致高衝突率，資料庫壓力大
教訓：機率論很重要，直覺不可靠

方案 C：Snowflake ID + Base62
成功：全局唯一、高效能、無需協調
教訓：分散式系統需要分散式演算法
```

### 2. 完美方案不存在

```
所有方案都有權衡：

Snowflake ID：
優勢：全局唯一、高效能、可擴展
劣勢：需配置機器 ID、時鐘回撥問題、趨勢遞增（輕微可預測）

隨機生成：
優勢：完全不可預測、實現簡單
劣勢：衝突檢查開銷、不確定性

Hash(URL)：
優勢：相同 URL 生成相同短碼（去重）
劣勢：衝突率高、仍需檢查

教訓：根據業務需求選擇，評估權衡
```

### 3. 真實場景驅動設計

```
問題演進：

第一階段：基本短網址
→ 需求：生成短碼、重定向
→ 方案：自增 ID

第二階段：安全問題
→ 需求：防止枚舉攻擊
→ 方案：隨機生成

第三階段：效能問題
→ 需求：避免衝突檢查
→ 方案：Snowflake ID

第四階段：高並發讀取
→ 需求：1,000+ QPS 重定向
→ 方案：Cache-Aside + Bloom Filter

第五階段：安全加固
→ 需求：防止 SSRF
→ 方案：URL 驗證、私有 IP 檢查

教訓：系統設計是持續演進的過程
```

### 4. 工業界如何選擇

| 場景 | 推薦方案 | 原因 |
|------|---------|------|
| **小型服務**<br>< 100 萬 URL | 自增 ID + Base62 | 簡單可靠，成本低 |
| **中型服務**<br>100 萬 - 1 億 URL | Snowflake ID + Base62<br>（本章方案） | 可擴展，效能好 |
| **大型服務**<br>> 1 億 URL | Snowflake + Redis Cluster<br>+ PostgreSQL 分片 | 水平擴展，高可用 |
| **安全敏感** | 隨機生成 + 唯一性檢查 | 完全不可預測 |
| **去重需求** | Hash(URL) + 衝突處理 | 相同 URL 生成相同短碼 |

---

## 總結

URL Shortener 展示了**分散式系統設計**的核心挑戰：

1. **唯一性**：從自增 ID → 隨機生成 → Snowflake ID
2. **效能**：從直接查資料庫 → Cache-Aside → Bloom Filter
3. **安全**：從無驗證 → SSRF 防護 → 多層檢查
4. **可擴展性**：從單機 → 讀寫分離 → 分片集群

**核心思想：** 用分散式 ID 避免協調，用快取最佳化讀取，用多層防護保證安全。

**適用場景：**
- 短網址服務
- 邀請碼生成
- 優惠券碼系統
- 任何需要將長識別碼縮短的場景

**不適用：**
- 需要語義化 ID（如訂單號要包含日期）
- 需要嚴格順序（Snowflake 僅趨勢遞增）
- 需要去重（相同輸入生成相同輸出）

**關鍵權衡：**
- 唯一性 vs 可預測性（Snowflake 趨勢遞增，輕微可預測）
- 一致性 vs 效能（快取最終一致性 vs 資料庫強一致性）
- 安全 vs 易用性（SSRF 防護可能誤擋合法 URL）
- 記憶體 vs 準確性（Bloom Filter 有誤判但省記憶體）
