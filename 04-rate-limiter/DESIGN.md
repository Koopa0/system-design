# Rate Limiter 系統設計文檔

## 場景：你是 Twitter API 的架構師

### 凌晨 3 點的緊急電話

你被電話吵醒：

> **運維團隊：** "API 服務器 CPU 100%！資料庫連接耗盡！網站快掛了！"

你迅速查看監控，發現：
```
異常請求源：
- IP 203.0.113.42
- 過去 1 分鐘：47,823 次請求
- 正常用戶平均：每分鐘 5 次請求

資料庫狀態：
- 活躍連接：1,000 (已達上限)
- 慢查詢：95% 的查詢超過 5 秒
- CPU：98%
```

**問題根源：** 有人（或機器人）在瘋狂刷你的 API！

你意識到需要一個**限流系統** (Rate Limiter) 來保護服務。

### 業務需求

產品經理給你的要求：

```
1. 防止過載
   - 免費用戶：100 次/小時
   - 付費用戶：1,000 次/小時
   - 企業用戶：10,000 次/小時

2. 防止暴力破解
   - 登錄接口：5 次/分鐘（同一 IP）
   - 超過後鎖定 15 分鐘

3. 允許突發流量
   - 正常用戶偶爾會發送連續請求
   - 不能因為短時間突發就拒絕合法用戶
```

### 你會問自己：如何設計限流算法？

---

## 第一次嘗試：固定窗口計數器

### 最直覺的想法

你想：「限流就是計數，超過就拒絕，很簡單！」

```go
type FixedWindowCounter struct {
    count       int64
    windowStart time.Time
    limit       int64  // 每分鐘 100 次
}

func (c *FixedWindowCounter) Allow() bool {
    now := time.Now()

    // 檢查是否需要重置窗口
    if now.Sub(c.windowStart) >= time.Minute {
        c.count = 0
        c.windowStart = now
    }

    // 檢查是否超限
    if c.count < c.limit {
        c.count++
        return true
    }
    return false  // 拒絕請求
}
```

你快速實現並部署上線。

### 時序範例

```
時間軸：
00:00:00 - 00:00:59 → 接收 100 個請求 ✅ (允許)
00:01:00           → 窗口重置，count = 0
00:01:00 - 00:01:59 → 接收 100 個請求 ✅ (允許)
```

看起來完美運作！

### 災難再現：邊界突刺問題

一週後，同樣的問題再次發生！你仔細查看日誌：

```
日誌分析：
00:00:50 - 00:00:59：100 個請求 ✅ (窗口 1，允許)
00:01:00           : 窗口重置
00:01:00 - 00:01:09：100 個請求 ✅ (窗口 2，允許)

結果：19 秒內處理了 200 個請求！
限制：每分鐘 100 次
實際：每 19 秒 200 次 = 每分鐘 631 次！
超限：531%
```

**問題發現：邊界突刺 (Boundary Burst)**

```
視覺化：
窗口 1: [──────────────────────────■■■■■■■■■■] (最後 10 秒 100 個)
窗口 2: [■■■■■■■■■■──────────────────────────] (最初 10 秒 100 個)
                                  ↑
                              邊界重置

問題：惡意用戶可以利用窗口邊界發送 2 倍流量！
```

### 你會問自己：

1. **為什麼會發生？**
   - 固定窗口在整點重置
   - 惡意用戶可以在窗口末尾 + 窗口開始各發滿額度
   - 1 秒內可以發送 200 個請求（2 倍限制）

2. **如何解決？**
   - 需要「滑動」的窗口，而不是「固定」的
   - 任意時刻統計過去 N 秒的請求數

3. **為什麼這麼嚴重？**
   - 惡意用戶可以持續利用邊界
   - 實際流量可能是限制的 2 倍
   - 資料庫仍然會過載

---

## 第二次嘗試：滑動窗口

### 設計思路

你改進設計：

> "如果記錄每個請求的時間戳，統計過去 60 秒內有多少個，就沒有邊界問題了！"

```go
type SlidingWindow struct {
    requests []time.Time    // 記錄所有請求時間戳
    limit    int64          // 100 次/分鐘
    window   time.Duration  // 60 秒
}

func (sw *SlidingWindow) Allow() bool {
    now := time.Now()
    windowStart := now.Add(-sw.window)  // 60 秒前

    // 移除 60 秒前的舊請求
    validIdx := 0
    for i, t := range sw.requests {
        if t.After(windowStart) {
            validIdx = i
            break
        }
    }
    sw.requests = sw.requests[validIdx:]

    // 檢查限制
    if len(sw.requests) < int(sw.limit) {
        sw.requests = append(sw.requests, now)
        return true
    }
    return false  // 拒絕請求
}
```

### 時序範例

```
時間軸（限制：100 次/分鐘）:
00:00:50 → 收到請求，檢查 23:59:50 - 00:00:50 的請求數 → 100 個 → 拒絕 ❌
00:01:00 → 收到請求，檢查 00:00:00 - 00:01:00 的請求數 → 10 個 → 允許 ✅
00:01:10 → 收到請求，檢查 00:00:10 - 00:01:10 的請求數 → 20 個 → 允許 ✅

完美！無論什麼時刻，任意 60 秒內最多 100 個請求。
```

### 完美解決邊界問題

```
測試結果：
- 邊界突刺：✅ 已解決
- 精度：✅ 完全符合限制
- 公平性：✅ 任意時刻都公平

你興奮地部署到生產環境！
```

### 新問題：內存爆炸

兩天後，運維團隊報告：

> **運維：** "服務器內存使用量暴增！每台機器 8GB 內存已用掉 6GB！"

你分析內存使用：

```
內存估算：
- 用戶數：100 萬活躍用戶
- 限制：每用戶 1000 次/小時
- 每個時間戳：24 bytes (time.Time)

內存用量：
- 每個用戶：1000 × 24 bytes = 24 KB
- 100 萬用戶：24 KB × 1,000,000 = 24 GB！

問題：
- 單機內存不夠
- 需要分布式存儲（Redis）
- Redis 成本：$500/月（16GB × 2 副本）
```

你陷入兩難：

| 方案 | 優勢 | 問題 |
|------|------|------|
| **固定窗口** | ✅ 內存小（2 個數字） | ❌ 邊界突刺（2 倍流量） |
| **滑動窗口** | ✅ 無邊界問題 | ❌ 內存大（N 個時間戳） |

**困境：** 有沒有兩全其美的方案？

---

## 第三次嘗試：漏桶算法

### 設計思路

你想到水龍頭接水：

> "如果請求是水，以固定速率流出，超過的水溢出（拒絕），不就能平滑流量嗎？"

```go
type LeakyBucket struct {
    queue      []Request      // 請求隊列
    capacity   int            // 桶容量
    leakRate   time.Duration  // 流出速率（如每 10ms 處理 1 個）
}

func (lb *LeakyBucket) Allow(req Request) bool {
    // 檢查桶是否已滿
    if len(lb.queue) >= lb.capacity {
        return false  // 桶滿，溢出（拒絕）
    }

    // 加入隊列
    lb.queue = append(lb.queue, req)
    return true
}

// 後台任務：以固定速率處理請求
func (lb *LeakyBucket) processRequests() {
    ticker := time.NewTicker(lb.leakRate)  // 每 10ms
    for range ticker.C {
        if len(lb.queue) > 0 {
            req := lb.queue[0]
            lb.queue = lb.queue[1:]
            processRequest(req)  // 實際處理請求
        }
    }
}
```

### 時序範例

```
限制：100 次/秒 = 每 10ms 處理 1 個請求

時間軸：
00:00:000 → 收到 10 個請求 → 加入隊列 [10 個]
00:00:010 → 處理 1 個 → 隊列剩 [9 個]
00:00:020 → 處理 1 個 → 隊列剩 [8 個]
...
00:00:090 → 隊列清空

結果：10 個請求花了 90ms 才全部處理完
```

### 問題：延遲增加

產品經理抱怨：

> **PM：** "用戶反饋 API 響應變慢了！原本 50ms，現在平均 200ms！"

你意識到問題：

```
問題分析：
- 漏桶：請求必須排隊
- 即使服務器空閒，也要等待「漏出」
- 延遲 = 處理時間 + 排隊時間

範例：
- 突發 50 個請求
- 處理速率：10ms/個
- 延遲：第 1 個 10ms，第 50 個 500ms

結論：
✅ 輸出流量平滑（對後端友好）
❌ 延遲增加（對用戶不友好）
❌ 不適合需要即時響應的 API
```

---

## 最終方案：令牌桶算法

### 靈感來源

你看著銀行提款機突然頓悟：

> **你：** "銀行不是限制你取款速度，而是限制你的餘額！"
>
> - 每秒存入 100 元（補充額度）
> - 取款時扣除餘額
> - 餘額不夠就拒絕
> - 餘額可以累積（允許突發）

這就是**令牌桶** (Token Bucket)！

### 設計思路

```
機制：
1. 桶裡存放「令牌」（代表可用額度）
2. 以固定速率填充令牌（如每秒 100 個）
3. 請求到達時：
   - 有令牌 → 取走 1 個，允許請求
   - 無令牌 → 拒絕請求
4. 桶有容量上限（如 200），多餘的令牌丟棄

視覺化：
   ┌─────────────────┐
   │  Tokens: 150    │  ← 當前令牌數
   │  Capacity: 200  │  ← 桶容量（允許突發）
   │  Rate: 100/s    │  ← 填充速率
   └─────────────────┘
         ↑      ↓
      補充    消費
   (100/s)  (請求到達時)
```

### 實現

```go
type TokenBucket struct {
    tokens     float64       // 當前令牌數
    capacity   float64       // 桶容量（200）
    refillRate float64       // 填充速率（100/s）
    lastRefill time.Time     // 上次填充時間
    mu         sync.Mutex
}

func (tb *TokenBucket) Allow() bool {
    tb.mu.Lock()
    defer tb.mu.Unlock()

    now := time.Now()
    elapsed := now.Sub(tb.lastRefill).Seconds()

    // 計算應該填充的令牌數
    tokensToAdd := elapsed * tb.refillRate

    // 填充令牌（不超過容量）
    tb.tokens = math.Min(tb.capacity, tb.tokens + tokensToAdd)
    tb.lastRefill = now

    // 嘗試消費 1 個令牌
    if tb.tokens >= 1 {
        tb.tokens -= 1
        return true  // 允許請求
    }
    return false  // 拒絕請求
}
```

### 時序範例

```
初始狀態：
- 令牌數：200（桶滿）
- 容量：200
- 填充速率：100/s

場景 1：正常流量
00:00:00 → 10 個請求 → 消費 10 個令牌 → 剩 190
00:00:01 → 補充 100 個 → 總計 290，但容量 200 → 剩 200
00:00:01 → 10 個請求 → 消費 10 個 → 剩 190

結論：正常流量下，令牌一直充足

場景 2：突發流量
00:00:00 → 150 個請求 → 消費 150 個 → 剩 50 ✅ (允許突發)
00:00:01 → 100 個請求 → 剩餘 50 個，只能處理 50 個 ⚠️
00:00:01 → 補充 100 個 → 總計 100
00:00:02 → 補充 100 個 → 總計 200（恢復）

結論：允許短時間突發，長期仍被限制
```

### 為什麼這是最佳選擇？

對比所有方案：

| 特性 | 固定窗口 | 滑動窗口 | 漏桶 | 令牌桶 |
|------|---------|---------|------|--------|
| **內存佔用** | ✅ 最小（2 數字） | ❌ 大（N timestamp） | ⚠️ 中（隊列） | ✅ 最小（2 數字） |
| **邊界突刺** | ❌ 有問題（2x） | ✅ 無問題 | ✅ 無問題 | ✅ 無問題 |
| **允許突發** | ❌ 不支持 | ❌ 不支持 | ❌ 不支持 | ✅ 支持（桶容量） |
| **響應延遲** | ✅ 無延遲 | ✅ 無延遲 | ❌ 增加延遲 | ✅ 無延遲 |
| **實現複雜度** | ✅ 簡單 | ⚠️ 中等 | ❌ 複雜（後台任務） | ✅ 簡單 |
| **CPU 開銷** | ✅ 極低 | ⚠️ 中等（清理） | ⚠️ 中等（定時任務） | ✅ 極低 |

**令牌桶勝出原因：**

1. ✅ **內存小**：只需 2 個數字（tokens, lastRefill）
2. ✅ **支持突發**：桶容量可調（如 2x 平均流量）
3. ✅ **無延遲**：請求立即處理或拒絕
4. ✅ **實現簡單**：不需要後台任務
5. ✅ **性能高**：O(1) 時間複雜度
6. ✅ **符合直覺**：類似銀行餘額

---

## 新挑戰：分布式限流

### 場景升級

你的服務擴展到 3 個實例：

```
                    Load Balancer
                          │
          ┌───────────────┼───────────────┐
          ▼               ▼               ▼
      Instance 1      Instance 2      Instance 3
     (限流 100/s)    (限流 100/s)    (限流 100/s)
```

產品經理要求：「全局限制 300 req/s」

### 第一次嘗試：每個實例獨立限流

最簡單的想法：

```
配置：
- 總限制：300 req/s
- 實例數：3
- 每個實例：300 / 3 = 100 req/s
```

看起來很合理！

### 災難場景：負載不均

一週後，你發現問題：

```
實際流量分布：
00:00:00 - 00:00:01
├─ Instance 1：50 req/s  → 限制 100，全部允許 ✅
├─ Instance 2：50 req/s  → 限制 100，全部允許 ✅
└─ Instance 3：250 req/s → 限制 100，拒絕 150 ❌

總計：
- 實際請求：350 req/s
- 允許：200 req/s
- 拒絕：150 req/s

問題：
1. 總流量超限（350 > 300）
2. 負載均衡失效時，大量合法請求被拒
3. 動態擴縮容會改變限制（擴到 4 個 = 400 req/s）
```

### 更嚴重的問題：動態擴縮容

```
場景：
09:00 → 高峰期，擴容到 6 個實例
      → 每個實例 300/6 = 50 req/s
      → 總限制變成 300 req/s ✅

12:00 → 流量下降，縮容到 2 個實例
      → 每個實例 300/2 = 150 req/s
      → 總限制變成 300 req/s ✅

12:05 → 突發流量到來，還未擴容
      → 2 個實例 × 150 = 300 req/s
      → 但實際來了 500 req/s → 拒絕 200 req/s ❌

結論：單機限流無法應對動態場景
```

---

## 分布式限流方案：Redis 集中存儲

### 設計思路

你意識到：

> "所有實例需要共享同一個「令牌桶」！"

```
架構：
      Client Request
            │
      Load Balancer
            │
    ┌───────┴───────┐
    ▼       ▼       ▼
  Inst1   Inst2   Inst3
    │       │       │
    └───────┼───────┘
            ▼
        Redis (共享狀態)
      ┌─────────────┐
      │ tokens: 150 │
      │ last: T0    │
      └─────────────┘
```

### 挑戰：原子性問題

如果用普通 Redis 命令：

```
錯誤實現：
func Allow() bool {
    tokens := redis.Get("tokens")
    if tokens > 0 {
        redis.Decr("tokens")  // ❌ 非原子！
        return true
    }
    return false
}

問題：
- 請求 A：讀取 tokens = 1
- 請求 B：讀取 tokens = 1（還沒來得及扣減）
- 請求 A：扣減 → tokens = 0
- 請求 B：扣減 → tokens = -1 ❌ (超限！)
```

### 解決方案：Lua 腳本（原子性）

Redis Lua 腳本保證原子執行：

```lua
-- token_bucket.lua
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

-- 獲取當前狀態（HMGET 一次性讀取多個字段）
local state = redis.call('HMGET', key, 'tokens', 'last_refill')
local tokens = tonumber(state[1]) or capacity
local last_refill = tonumber(state[2]) or now

-- 計算應補充的令牌
local elapsed = now - last_refill
local tokens_to_add = math.floor(elapsed * rate)
tokens = math.min(capacity, tokens + tokens_to_add)

-- 嘗試消費令牌
local allowed = 0
if tokens > 0 then
    tokens = tokens - 1
    allowed = 1
end

-- 更新狀態（HMSET 一次性寫入多個字段）
redis.call('HMSET', key, 'tokens', tokens, 'last_refill', now)
redis.call('EXPIRE', key, 60)  -- 60 秒後自動清理

return allowed
```

Go 客戶端調用：

```go
func (r *RedisRateLimiter) Allow(userID string) (bool, error) {
    script := `... Lua 腳本 ...`

    keys := []string{fmt.Sprintf("ratelimit:user:%s", userID)}
    args := []interface{}{
        r.capacity,    // 200
        r.rate,        // 100
        time.Now().Unix(),
    }

    result, err := r.client.Eval(script, keys, args...).Result()
    if err != nil {
        return false, err
    }

    return result.(int64) == 1, nil
}
```

### 性能分析

```
單次限流檢查：
- 網絡延遲：~1ms（同機房）
- Redis 執行：~0.1ms（Lua 腳本）
- 總延遲：~1-2ms

吞吐量：
- Redis 單實例：~50K QPS（限流檢查）
- 如果每個 API 請求都限流 → 支持 50K API req/s

成本：
- Redis 實例：$50/月（2GB 內存）
- 支持：50K req/s
- 成本效益：$0.001/百萬請求
```

### 權衡

| 方案 | 優勢 | 劣勢 |
|------|------|------|
| **單機限流** | • 極低延遲（< 1ms）<br>• 無外部依賴<br>• 零成本 | • 負載不均時失效<br>• 動態擴縮容問題<br>• 無全局視圖 |
| **Redis 分布式** | • 全局準確<br>• 動態擴縮容友好<br>• 實現相對簡單 | • 延遲增加（~2ms）<br>• Redis 故障影響限流<br>• 成本（Redis 基礎設施） |
| **混合方案** | • 低延遲（本地為主）<br>• 減少 Redis 壓力 | • 精度降低（~5%）<br>• 實現複雜<br>• 適用場景有限 |

---

## 多維度限流

### 場景

產品經理的新需求：

> "我們需要同時限制：
> 1. 每個用戶：100 req/hour
> 2. 每個 IP：1000 req/hour（防 DDoS）
> 3. 登錄接口：5 req/min（防暴力破解）"

### Key 設計

```
Redis Key 命名：
ratelimit:{dimension}:{value}:{window}

範例：
ratelimit:user:user_123:hour      → 用戶小時限制
ratelimit:ip:1.2.3.4:hour         → IP 小時限制
ratelimit:api:/login:min          → 登錄接口分鐘限制
ratelimit:user:user_123:api:/upload → 組合限制（用戶+API）
```

### 實現：多層檢查

```go
type MultiDimensionLimiter struct {
    limiters map[string]*RedisRateLimiter
}

func (m *MultiDimensionLimiter) Allow(ctx RequestContext) bool {
    // 檢查順序：從最嚴格到最寬鬆
    checks := []struct{
        key     string
        limiter *RedisRateLimiter
    }{
        // 1. API 限制（最嚴格）
        {
            key: fmt.Sprintf("api:%s", ctx.Endpoint),
            limiter: m.limiters["api"],
        },
        // 2. 用戶限制
        {
            key: fmt.Sprintf("user:%s", ctx.UserID),
            limiter: m.limiters["user"],
        },
        // 3. IP 限制（最寬鬆）
        {
            key: fmt.Sprintf("ip:%s", ctx.IP),
            limiter: m.limiters["ip"],
        },
    }

    // 短路求值：任一維度超限，直接拒絕
    for _, check := range checks {
        allowed, err := check.limiter.Allow(check.key)
        if err != nil {
            // Redis 故障，降級處理（允許或拒絕？根據業務決定）
            return true  // 或 false
        }
        if !allowed {
            return false  // 任一維度超限，拒絕請求
        }
    }

    return true  // 所有維度都通過
}
```

### 範例場景

```
請求：POST /api/upload
用戶：user_123
IP：203.0.113.42

檢查流程：
1. 檢查 ratelimit:api:/api/upload:min
   → 限制 10 req/min → 當前 3 → 通過 ✅

2. 檢查 ratelimit:user:user_123:hour
   → 限制 100 req/hour → 當前 95 → 通過 ✅

3. 檢查 ratelimit:ip:203.0.113.42:hour
   → 限制 1000 req/hour → 當前 998 → 通過 ✅

結果：允許請求 ✅

---

如果任一步驟超限：
2. 檢查 ratelimit:user:user_123:hour
   → 限制 100 req/hour → 當前 100 → 拒絕 ❌

結果：直接返回 429，無需檢查 IP 限制（短路優化）
```

### 優化：短路求值

```
為什麼按嚴格程度排序：
- API 限制最嚴格（10 req/min）
- 用戶限制次嚴格（100 req/hour）
- IP 限制最寬鬆（1000 req/hour）

如果先檢查嚴格的：
- 大部分請求在第 1 步就被拒絕
- 無需調用後續的 Redis（減少延遲）

性能提升：
- 原本：3 次 Redis 調用 = 6ms
- 優化：平均 1.2 次 Redis 調用 = 2.4ms
- 提升：60%
```

---

## 限流失敗處理

### HTTP 響應設計

遵循 RFC 6585 標準：

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 60
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1735689600
Content-Type: application/json

{
  "error": {
    "code": "RATE_LIMIT_EXCEEDED",
    "message": "Rate limit exceeded. Try again in 60 seconds.",
    "retry_after": 60
  }
}
```

Header 說明：

- **Retry-After**: 多少秒後可重試（60 秒）
- **X-RateLimit-Limit**: 限制額度（100 次/小時）
- **X-RateLimit-Remaining**: 剩餘額度（0）
- **X-RateLimit-Reset**: 重置時間戳（Unix timestamp）

### 客戶端自動重試

```python
import requests
import time

def api_call_with_retry(url, max_retries=3):
    for attempt in range(max_retries):
        response = requests.get(url)

        if response.status_code == 429:
            # 讀取 Retry-After header
            retry_after = int(response.headers.get('Retry-After', 60))
            print(f"Rate limited. Waiting {retry_after} seconds...")
            time.sleep(retry_after)
            continue  # 重試

        return response  # 成功

    raise Exception("Max retries exceeded")
```

---

## 擴展性分析

### 當前架構容量

```
單機限流：
├─ 算法：Token Bucket（內存）
├─ 性能：100K RPS
├─ 延遲：< 1ms
└─ 適用：單實例服務

分布式限流：
├─ 存儲：Redis 單實例
├─ 性能：50K RPS
├─ 延遲：< 5ms（網絡 + Redis）
└─ 適用：多實例服務，中等流量
```

### 10x 擴展（500K RPS）

**瓶頸分析：**
```
Redis 單實例極限：~80K QPS
每次限流檢查：3 個維度 × 1 次 = 3 次 Redis 調用
實際支持：80K / 3 ≈ 26K req/s（遠低於目標）
```

**方案：Redis Cluster 分片**

```
架構：
      API Servers (100 實例)
            │
      ┌─────┴─────┬─────────┬─────────┐
      ▼           ▼         ▼         ▼
  Redis 1     Redis 2   Redis 3   Redis 4
  (Shard 1)   (Shard 2) (Shard 3) (Shard 4)

分片策略：
- 按 Key hash 分片
- 用戶限流：hash(user_id) % 4
- IP 限流：hash(ip) % 4
- API 限流：hash(endpoint) % 4

容量：
- 4 個 shard × 80K QPS = 320K QPS
- 考慮 3 個維度：320K / 3 ≈ 100K req/s ✅

成本：
- 4 個 Redis 實例 × $50 = $200/月
- 支持：100K req/s
```

### 100x 擴展（5M RPS）

需要架構升級：

**方案：分層限流**

```
三層架構：
┌─────────────────────────────────────────┐
│ L1: 本地內存限流（99% 請求）               │
│  - Token Bucket 內存實現                 │
│  - 延遲：< 1ms                           │
│  - 容忍誤差：~5%                         │
└─────────────────────────────────────────┘
         │ 每 100ms 同步一次
         ▼
┌─────────────────────────────────────────┐
│ L2: Redis Cluster（1% 同步）             │
│  - 32 個 shard                          │
│  - 每個實例預取配額                      │
└─────────────────────────────────────────┘
         │ 每 10 秒結算一次
         ▼
┌─────────────────────────────────────────┐
│ L3: 資料庫（持久化配額）                  │
│  - PostgreSQL                           │
│  - 用於計費、審計                        │
└─────────────────────────────────────────┘

流程：
1. API Gateway 每 10 秒向 Redis 申請配額（如 1000 次）
2. 本地消費這 1000 次配額（內存限流）
3. 配額用盡或過期，重新申請
4. Redis 定期（每 10 秒）同步到資料庫

性能：
- 99% 請求：本地檢查（< 1ms）
- 1% 請求：Redis 同步（< 5ms）
- 平均延遲：~1.04ms

容量：
- 100 個 API Gateway × 50K local RPS = 5M RPS ✅

權衡：
- 精度：10 秒窗口內可能超限 ~5%
- 可接受：短時間超限對系統影響有限

成本：
- API Gateway：100 實例 × $50 = $5,000/月
- Redis Cluster：32 shard × $50 = $1,600/月
- PostgreSQL：2 實例 × $200 = $400/月
- 總計：~$7,000/月（$0.0014/百萬請求）
```

---

## 真實工業案例

### Stripe API (Token Bucket)

```
配置：
- 基礎限制：100 req/s
- 突發容量：200（允許 2x 突發）
- 窗口：滑動 1 秒

響應 Header：
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1735689600

為什麼選擇 Token Bucket：
- 支付 API 需要處理突發（批量支付）
- Token Bucket 允許短時間突發
- 長期平均仍被限制
```

### GitHub API (Token Bucket)

```
配置：
- 免費用戶：60 req/hour
- 認證用戶：5,000 req/hour
- GraphQL API：5,000 points/hour（不同 query 不同 point）

響應 Header：
X-RateLimit-Limit: 5000
X-RateLimit-Remaining: 4999
X-RateLimit-Reset: 1735689600
X-RateLimit-Used: 1
X-RateLimit-Resource: core

特殊設計：
- GraphQL 按複雜度計費（不是按請求數）
- 複雜查詢消耗更多 points
- 激勵用戶優化查詢
```

### Nginx (Leaky Bucket)

```nginx
http {
    # 定義漏桶
    limit_req_zone $binary_remote_addr zone=one:10m rate=1r/s;

    server {
        location /api/ {
            # 應用限流
            # burst=5：允許突發 5 個請求進入隊列
            # nodelay：不延遲處理（超過 burst 直接拒絕）
            limit_req zone=one burst=5 nodelay;
        }
    }
}

為什麼 Nginx 用 Leaky Bucket：
- 保護後端服務（平滑流量）
- 避免突發流量打垮後端
- 適合反向代理場景
```

### AWS API Gateway (Token Bucket)

```
配置：
- 穩態速率：10,000 req/s
- 突發容量：5,000（累積令牌）

CloudWatch 監控：
- Count: 請求總數
- 4XXError: 限流拒絕數（429）
- IntegrationLatency: 後端延遲
- Latency: 總延遲（含限流檢查）

特點：
- 按 API 階段（dev/prod）獨立限流
- 支持使用計劃（Usage Plans）
- 自動擴展（無需手動配置 Redis）
```

---

## 實現範圍標註

### 已實現（核心教學內容）

| 功能 | 檔案 | 教學重點 |
|------|------|----------|
| **Token Bucket** | `tokenbucket.go:37-111` | 突發流量處理、令牌填充算法 |
| **Sliding Window** | `slidingwindow.go:42-127` | 精確限流、邊界問題解決 |
| **Leaky Bucket** | `leakybucket.go` | 平滑輸出、隊列處理 |
| **並發安全** | 各算法 `sync.Mutex` | 互斥鎖保護共享狀態 |

### 教學簡化（未實現）

| 功能 | 原因 | 生產環境建議 |
|------|------|-------------|
| **分布式限流** | 聚焦算法本身 | Redis + Lua 腳本保證原子性 |
| **多維度限流** | 簡化示範 | 按 IP、用戶、API 組合限流 |
| **配額管理** | 增加複雜度 | 預分配配額 + 定期結算 |
| **降級處理** | 業務邏輯相關 | 限流時返回緩存數據 |

### 生產環境額外需要

```
1. 監控告警
   ├─ 限流觸發率：每秒被拒絕請求數
   ├─ 各維度統計：IP/用戶/API 分別統計
   ├─ 異常檢測：突然大量限流告警
   └─ 配額使用：實時監控配額消耗

2. 動態配置
   ├─ 熱更新：無需重啟修改限流規則
   ├─ A/B 測試：不同用戶不同限制
   ├─ 白名單：特定用戶免限流
   └─ 緊急開關：快速關閉限流（應急）

3. 高級功能
   ├─ 令牌預借：允許透支（後續扣除）
   ├─ 優先級隊列：重要請求優先處理
   ├─ 平滑擴容：新實例逐步接管流量
   └─ 跨機房同步：多機房配額共享

4. 客戶端優化
   ├─ 指數退避：限流後延遲重試
   ├─ 本地限流：客戶端預檢，減少無效請求
   └─ 批量請求：合併多個請求減少次數
```

---

## 你學到了什麼？

### 1. 從錯誤中學習

```
錯誤方案的價值：
✗ 固定窗口 → 發現邊界突刺問題
✗ 滑動窗口 → 發現內存問題
✗ 漏桶算法 → 發現延遲問題
✓ 令牌桶   → 綜合最佳方案

教訓：每個「失敗」都帶來洞察
```

### 2. 完美方案不存在

```
所有方案都有權衡：
- 固定窗口：內存小 vs 邊界突刺
- 滑動窗口：精確 vs 內存大
- 漏桶：平滑 vs 延遲
- 令牌桶：均衡，但允許短時超限

教訓：根據場景選擇，不要追求「完美」
```

### 3. 真實場景驅動設計

```
問題演進：
1. API 被刷爆 → 需要限流
2. 邊界突刺 → 改進算法
3. 內存爆炸 → 優化數據結構
4. 負載不均 → 分布式方案
5. 多維度需求 → 組合限流

教訓：從實際問題出發，逐步改進
```

### 4. 工業界如何選擇

| 場景 | 推薦算法 | 原因 |
|------|---------|------|
| **API Gateway** | Token Bucket | 支持突發，低延遲 |
| **反向代理** | Leaky Bucket | 保護後端，平滑流量 |
| **計費系統** | Sliding Window | 精確計費，公平性 |
| **DDoS 防護** | Token Bucket + IP 維度 | 快速響應，阻斷攻擊 |
| **秒殺系統** | Token Bucket + 預熱 | 應對極端突發 |

---

## 總結

Rate Limiter 展示了**從問題到方案的完整演進**：

1. **固定窗口**：最簡單，但有邊界突刺
2. **滑動窗口**：精確控制，但內存大
3. **漏桶**：平滑流量，但延遲增加
4. **令牌桶**：支持突發，綜合最優 ✅

**核心思想：** 用令牌桶處理突發流量，用 Redis 實現分布式，用多維度組合實現靈活控制。

**適用場景：** API 限流、DDoS 防護、秒殺系統、防暴力破解、資源配額管理

**不適用：** 不需要限流的內部服務、已有負載均衡的場景

**關鍵權衡：**
- 精度 vs 性能（滑動窗口 vs 令牌桶）
- 本地 vs 分布式（延遲 vs 準確性）
- 簡單 vs 靈活（單維度 vs 多維度）
