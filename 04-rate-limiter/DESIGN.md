# Rate Limiter 系統設計文檔

## 問題定義

### 業務需求
構建限流系統（Rate Limiter），保護服務免受過載：
- **防止過載**：限制每個用戶/IP 的請求頻率
- **保證公平性**：防止單一用戶占用所有資源
- **支持突發流量**：允許短時間內的流量峰值
- **多維度限流**：支持按 IP、用戶、API 端點限流
- **分布式支持**：多服務實例共享限流狀態

### 技術指標
| 指標 | 目標值 | 挑戰 |
|------|--------|------|
| **限流精度** | 誤差 < 5% | 如何精確控制流量？ |
| **延遲開銷** | < 1ms (單機) | 如何保證低延遲？ |
| **分布式延遲** | < 5ms (Redis) | 如何保證原子性？ |
| **吞吐量** | 100K RPS (單機) | 如何承受高並發？ |
| **突發容忍** | 2x 平均流量 | 如何處理突發？ |

### 使用場景
```
場景 1：API 限流
- 免費用戶：100 req/hour
- 付費用戶：1000 req/hour
- 企業用戶：10000 req/hour

場景 2：防止暴力破解
- 登錄接口：5 次/分鐘
- 超過後鎖定 15 分鐘

場景 3：保護資料庫
- 寫入接口：1000 QPS
- 超過後返回 429 Too Many Requests
```

---

## 設計決策樹

### 決策 1：選擇哪種限流算法？

```
需求：限制 API 每秒最多處理 100 個請求

方案 A：計數器（Fixed Window Counter）
   機制：每秒鐘重置計數器

   時序範例：
   00:00:00.0 - 00:00:00.9：接收 100 個請求 
   00:00:01.0：計數器重置為 0
   00:00:01.0 - 00:00:01.1：接收 100 個請求 

   問題：邊界突發
   - 00:00:00.9 - 00:00:01.1 之間（0.2 秒）
   - 處理了 200 個請求（超過限制 2 倍）

   計算：
   - 限制：100 req/s
   - 最壞情況：200 req/s（邊界突發）
   - 誤差：100%

方案 B：Leaky Bucket（漏桶）
   機制：請求進入桶，以固定速率流出

   流程：
   1. 請求到達 → 加入隊列
   2. 以固定速率處理請求（如每 10ms 處理 1 個）
   3. 隊列滿 → 拒絕請求

   優勢：
   - 輸出流量平滑（嚴格控制）
   - 防止突發打到後端

   問題：
   - 延遲增加：請求需要排隊
   - 不適合需要即時響應的場景
   - 實現複雜：需要後台任務定期消費

   範例（不適用）：
   - 限制 100 req/s → 每 10ms 處理 1 個
   - 突發 10 個請求 → 需要等待 100ms
   - 用戶體驗差（延遲高）

選擇方案 C：Token Bucket（令牌桶）
   機制：桶內存放令牌，以固定速率填充

   流程：
   1. 桶以固定速率填充令牌（如每秒 100 個）
   2. 請求到達 → 嘗試取令牌
   3. 有令牌 → 允許請求
   4. 無令牌 → 拒絕請求

   優勢：
   - 支持突發流量：桶內可累積令牌（如容量 200）
   - 實現簡單：只需計時器 + 計數器
   - 無延遲：請求立即處理或拒絕
   - 性能高：O(1) 時間複雜度

   突發處理：
   - 桶容量：200（允許 2x 突發）
   - 填充速率：100/s
   - 平時累積令牌，突發時可用

   權衡：
   - 允許短時突發（符合實際需求）
   - 長期平均流量仍被限制

選擇方案 D：Sliding Window（滑動窗口）
   機制：統計滑動時間窗口內的請求數

   流程：
   1. 記錄每個請求的時間戳
   2. 統計過去 N 秒內的請求數
   3. 超過限制 → 拒絕

   優勢：
   - 精確限流：無邊界突發問題
   - 符合直覺：任意 1 秒內最多 100 個

   問題：
   - 內存占用高：需存儲所有時間戳
   - 性能略低：需遍歷清理過期請求

   內存估算：
   - 限制 1000 req/s
   - 每個時間戳 24 bytes
   - 內存：1000 × 24 = 24 KB（可接受）

   優化：Sliding Window Counter
   - 將窗口分段（如 60 個 1 秒桶）
   - 只存儲計數器（而非每個時間戳）
   - 內存：60 × 8 bytes = 480 bytes
   - 精度略降，但足夠實用
```

**選擇：Token Bucket（通用場景）+ Sliding Window（精確限流場景）**

**實現細節：**
```go
// Token Bucket
type TokenBucket struct {
    capacity   int64         // 桶容量（支持突發）
    tokens     int64         // 當前令牌數
    refillRate int64         // 填充速率（每秒）
    lastRefill time.Time     // 上次填充時間
}

func (tb *TokenBucket) Allow() bool {
    // 計算應填充的令牌數
    elapsed := time.Since(tb.lastRefill).Seconds()
    tokensToAdd := int64(elapsed * float64(tb.refillRate))

    // 填充令牌（不超過容量）
    tb.tokens = min(tb.capacity, tb.tokens + tokensToAdd)
    tb.lastRefill = time.Now()

    // 嘗試取令牌
    if tb.tokens > 0 {
        tb.tokens--
        return true
    }
    return false
}

// Sliding Window
type SlidingWindow struct {
    requests []time.Time    // 請求時間戳列表
    limit    int64          // 限制
    window   time.Duration  // 窗口大小
}

func (sw *SlidingWindow) Allow() bool {
    now := time.Now()
    windowStart := now.Add(-sw.window)

    // 清理過期請求
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
    return false
}
```

---

### 決策 2：單機 vs 分布式限流？

```
問題：多個服務實例如何共享限流狀態？

場景：3 個服務實例，限制 300 req/s

方案 A：每個實例獨立限流（100 req/s）
   問題：
   - 負載不均時失效：
     - 實例 1：50 req/s（限制 100，通過）
     - 實例 2：50 req/s（限制 100，通過）
     - 實例 3：250 req/s（限制 100，拒絕 150）
     - 總計：350 req/s（超過 300）

   - 動態擴縮容問題：
     - 擴容到 4 個實例 → 限制變為 400 req/s
     - 縮容到 2 個實例 → 限制變為 200 req/s
     - 需要動態調整每個實例的限制

選擇方案 B：分布式限流（Redis 集中存儲）
   機制：
   - 所有實例共享 Redis 中的計數器
   - 使用 Lua 腳本保證原子性

   流程：
   1. 請求到達實例 1
   2. 實例 1 調用 Redis Lua 腳本
   3. Lua 腳本原子性地：
      - 檢查當前計數
      - 計數 +1（如果未超限）
      - 返回是否允許
   4. 實例根據結果處理請求

   優勢：
   - 全局準確：所有實例共享狀態
   - 動態擴縮容友好：無需調整配置
   - 實現相對簡單

   權衡：
   - 延遲增加：需要網絡調用 Redis（~1-2ms）
   - 單點依賴：Redis 故障影響限流
   - 成本：需要 Redis 基礎設施

方案 C：本地限流 + 定期同步（混合）
   機制：
   - 每個實例維護本地計數器
   - 定期（如每 100ms）同步到 Redis
   - 根據全局狀態調整本地限制

   優勢：
   - 低延遲：大部分請求只查本地
   - 減少 Redis 壓力：同步頻率低

   問題：
   - 精度降低：100ms 內可能超限
   - 實現複雜：需要協調邏輯
   - 適用場景有限：對精度要求不高時
```

**選擇：方案 B（分布式限流）用於生產環境**
**教學實現：方案 A（單機）+ 方案 B（分布式）都展示**

**Redis + Lua 實現：**
```lua
-- Redis Lua 腳本（Token Bucket）
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

-- 獲取當前狀態
local state = redis.call('HMGET', key, 'tokens', 'last_refill')
local tokens = tonumber(state[1]) or capacity
local last_refill = tonumber(state[2]) or now

-- 計算填充令牌
local elapsed = now - last_refill
local tokens_to_add = math.floor(elapsed * rate)
tokens = math.min(capacity, tokens + tokens_to_add)

-- 嘗試取令牌
local allowed = 0
if tokens > 0 then
    tokens = tokens - 1
    allowed = 1
end

-- 更新狀態
redis.call('HMSET', key, 'tokens', tokens, 'last_refill', now)
redis.call('EXPIRE', key, 60)  -- 60 秒過期

return allowed
```

---

### 決策 3：如何處理限流維度？

```
問題：需要按不同維度限流（IP、用戶、API 端點）

場景：
- 免費用戶：100 req/hour（按用戶 ID）
- 單個 IP：1000 req/hour（防止 DDoS）
- 登錄接口：5 req/min（按 IP，防暴力破解）

方案 A：單一維度限流
   問題：無法滿足多維度需求

選擇方案 B：多維度限流（組合 key）
   機制：根據不同維度組合 key

   Key 設計：
   - 用戶限流：ratelimit:user:{user_id}
   - IP 限流：ratelimit:ip:{ip_address}
   - API 限流：ratelimit:api:{endpoint}
   - 組合限流：ratelimit:user:{user_id}:api:{endpoint}

   範例：
   POST /api/upload，用戶 user_123，IP 1.2.3.4

   需要檢查：
   1. ratelimit:user:user_123 → 用戶總限制
   2. ratelimit:ip:1.2.3.4 → IP 限制
   3. ratelimit:api:/api/upload → API 限制

   只有都通過才允許請求

   優勢：
   - 靈活：可按需組合
   - 精細控制：不同場景不同策略

   權衡：
   - 延遲增加：需要多次檢查
   - 複雜度高：需要協調多個限流器

優化：短路求值
   機制：按嚴格程度排序，優先檢查

   範例：
   - IP 限流：1000 req/hour（最寬鬆）
   - 用戶限流：100 req/hour（次嚴格）
   - API 限流：10 req/hour（最嚴格）

   檢查順序：API → 用戶 → IP
   - 如果 API 限流已觸發，直接拒絕（無需檢查其他）
   - 減少不必要的 Redis 調用
```

**選擇：多維度限流 + 短路優化**

**實現細節：**
```go
type RateLimiter struct {
    limiters map[string]*TokenBucket
}

func (rl *RateLimiter) Allow(ctx Context) bool {
    // 檢查多個維度（按嚴格程度排序）
    checks := []struct{
        key string
        limiter *TokenBucket
    }{
        {"api:" + ctx.Endpoint, rl.limiters["api"]},
        {"user:" + ctx.UserID, rl.limiters["user"]},
        {"ip:" + ctx.IP, rl.limiters["ip"]},
    }

    for _, check := range checks {
        if !check.limiter.Allow() {
            return false  // 短路：任一維度超限，直接拒絕
        }
    }
    return true
}
```

---

### 決策 4：如何處理限流失敗？

```
問題：請求被限流時如何處理？

方案 A：直接返回 429
   問題：用戶體驗差，無法預估恢復時間

選擇方案 B：返回 429 + Retry-After header
   機制：告訴客戶端何時可以重試

   HTTP 響應：
   HTTP/1.1 429 Too Many Requests
   Retry-After: 60
   X-RateLimit-Limit: 100
   X-RateLimit-Remaining: 0
   X-RateLimit-Reset: 1609459200

   Body:
   {
     "error": "Rate limit exceeded",
     "retry_after": 60
   }

   優勢：
   - 客戶端可以自動重試
   - 避免無意義的重複請求

方案 C：請求排隊（Leaky Bucket）
   問題：增加延遲，不適合實時場景

方案 D：降級處理
   機制：
   - 限流時返回緩存數據（如果可以）
   - 或返回簡化版本（減少計算）

   範例：
   - 正常：返回個性化推薦（需要計算）
   - 限流時：返回熱門推薦（緩存）
```

**選擇：方案 B（標準響應）+ 方案 D（降級，可選）**

---

## 擴展性分析

### 當前架構容量

```
單機限流：
- 算法：Token Bucket（內存）
- 性能：100K RPS
- 延遲：< 1ms
- 適用：單實例服務

分布式限流：
- 存儲：Redis
- 性能：50K RPS（受 Redis 限制）
- 延遲：< 5ms（網絡開銷）
- 適用：多實例服務

結論：單機足夠支撐大部分場景
```

### 10x 擴展（500K RPS）

```
瓶頸分析：
Redis 單實例：~80K RPS 極限
網絡延遲：每次檢查 ~2ms

方案 1：Redis 主從複製
- 讀寫分離：讀從庫，寫主庫
- 問題：限流需要讀寫原子性（不適用）

方案 2：Redis Cluster 分片
- 按 key 分片（如按用戶 ID hash）
- 16 個 shard × 50K RPS = 800K RPS
- 成本：$1,600/月（16 個 Redis 實例）

方案 3：本地限流 + 定期同步
- 99% 請求查本地（< 1ms）
- 1% 同步到 Redis（更新全局狀態）
- 精度略降（~5% 誤差），性能大幅提升
- 成本：無需額外基礎設施

推薦：方案 3（精度要求不極致時）
```

### 100x 擴展（5M RPS）

```
需要架構升級：

1. 分層限流
   - L1：本地內存（99.9% 請求）
   - L2：Redis（0.1% 同步）
   - L3：資料庫（持久化配額）

2. 限流策略優化
   - 粗粒度限流：按 IP 段而非單 IP
   - 預分配配額：每個實例領取一批配額
   - 定期結算：避免實時同步

3. 分布式配額管理
   架構：
   Client Request
     ↓
   API Gateway (L1: 本地限流)
     ↓
   Quota Service (L2: Redis Cluster)
     ↓
   Billing DB (L3: 持久化)

   流程：
   - API Gateway 每 10 秒向 Quota Service 申請配額
   - Quota Service 從 Redis 分配配額
   - Redis 定期同步到 Billing DB

   優勢：
   - 極低延遲：本地限流 < 1ms
   - 高吞吐：減少 99.9% 的遠程調用
   - 可控誤差：配額窗口內（10 秒）可能超限 ~5%

4. 成本估算
   - API Gateway：100 實例 × $50 = $5,000/月
   - Redis Cluster：32 shard × $100 = $3,200/月
   - Quota Service：10 實例 × $100 = $1,000/月
   - 總計：~$9,200/月
```

---

## 實現範圍標註

### 已實現（核心教學內容）

| 功能 | 檔案 | 教學重點 |
|------|------|----------|
| **Token Bucket** | `tokenbucket.go:37-111` | 突發流量處理、令牌填充算法 |
| **Sliding Window** | `slidingwindow.go:42-127` | 精確限流、邊界問題解決 |
| **Leaky Bucket** | `leakybucket.go` | 平滑輸出、隊列處理 |
| **並發安全** | 各算法 `sync.Mutex` | 互斥鎖保護 |

### 教學簡化（未實現）

| 功能 | 原因 | 生產環境建議 |
|------|------|-------------|
| **分布式限流** | 聚焦算法本身 | Redis + Lua 腳本保證原子性 |
| **多維度限流** | 簡化示範 | 按 IP、用戶、API 組合限流 |
| **配額管理** | 增加複雜度 | 預分配配額 + 定期結算 |
| **降級處理** | 業務邏輯相關 | 限流時返回緩存數據 |

### 生產環境額外需要

```
1. 分布式限流
   - Redis Cluster：分片提升性能
   - Lua 腳本：保證操作原子性
   - 連接池：減少連接開銷
   - 熔斷器：Redis 故障時降級

2. 監控告警
   - 限流觸發率：每秒被拒絕的請求數
   - 各維度統計：IP/用戶/API 分別統計
   - 異常檢測：突然大量限流告警
   - 配額使用：實時監控配額消耗

3. 動態配置
   - 熱更新：無需重啟修改限流規則
   - A/B 測試：不同用戶不同限制
   - 白名單：特定用戶免限流
   - 緊急開關：快速關閉限流（應急）

4. 高級功能
   - 令牌預借：允許透支（後續扣除）
   - 優先級隊列：重要請求優先處理
   - 平滑擴容：新實例逐步接管流量
   - 跨機房同步：多機房配額共享

5. 客戶端優化
   - 指數退避：限流後延遲重試
   - 本地限流：客戶端預檢，減少無效請求
   - 批量請求：合併多個請求減少次數
```

---

## 關鍵設計原則總結

### 1. Token Bucket（支持突發）
```
令牌桶 vs 漏桶：
- 令牌桶：允許突發，適合大部分場景
- 漏桶：嚴格平滑，適合需要恆定速率的場景

容量設計：
- 平均流量：100 req/s
- 突發容量：200（2x 平均）
- 填充速率：100 令牌/s

突發處理：
- 平時累積令牌（最多 200 個）
- 突發時消耗累積的令牌
- 長期平均仍是 100 req/s
```

### 2. Sliding Window（精確限流）
```
固定窗口 vs 滑動窗口：
- 固定窗口：邊界突發問題（最壞 2x）
- 滑動窗口：精確控制，無邊界問題

內存優化：
- 精確版：存儲所有時間戳（24 KB/1000 req）
- 計數器版：分段計數（480 bytes/60 段）
- 權衡：精度 vs 內存

適用場景：
- 需要精確限流（如付費 API）
- QPS 不是極高（< 10K）
```

### 3. 分布式限流（全局一致）
```
單機 vs 分布式：
- 單機：低延遲（< 1ms），但無法共享
- 分布式：全局準確，延遲增加（~5ms）

Redis + Lua：
- 原子性：整個腳本原子執行
- 減少網絡：多個操作一次調用
- 性能：~50K RPS（單實例）

混合方案：
- 本地限流：99% 請求
- 定期同步：1% 請求同步到 Redis
- 精度略降（~5%），性能大幅提升
```

### 4. 多維度限流（靈活控制）
```
維度組合：
- 按用戶：防止單用戶濫用
- 按 IP：防止 DDoS
- 按 API：保護關鍵接口

短路優化：
- 按嚴格程度排序
- 優先檢查最嚴格的
- 任一維度超限，直接拒絕

Key 設計：
ratelimit:{dimension}:{value}:{window}
例如：ratelimit:user:123:hour
```

---

## 延伸閱讀

### 相關系統設計問題
- 如何設計一個 **API Gateway**？（限流是核心功能）
- 如何設計一個 **DDoS 防護系統**？（大規模限流）
- 如何設計一個 **秒殺系統**？（極端流量限制）

### 限流算法詳解
- **Token Bucket**：Google Guava RateLimiter
- **Leaky Bucket**：Nginx 限流模塊
- **Sliding Window**：Redis ZSET 實現
- **固定窗口**：Redis INCR + EXPIRE

### 工業實現參考
- **Nginx limit_req**: 基於 Leaky Bucket
- **Kong Rate Limiting**: 支持多種算法
- **AWS API Gateway**: Token Bucket
- **Google Cloud Armor**: 分布式限流

---

## 總結

Rate Limiter 展示了**流量控制**的經典設計模式：

1. **Token Bucket**：支持突發，適合通用場景
2. **Sliding Window**：精確限流，無邊界問題
3. **分布式限流**：Redis + Lua 保證全局一致
4. **多維度限流**：靈活組合，精細控制

**核心思想：** 用令牌桶處理突發流量，用滑動窗口保證精確性，用 Redis 實現分布式，用多維度組合實現靈活控制。

**適用場景：** API 限流、DDoS 防護、秒殺系統、防暴力破解、資源配額管理

**不適用：** 不需要限流的內部服務、已有負載均衡的場景

**與其他服務對比：**
| 維度 | Rate Limiter | URL Shortener | Counter Service |
|------|--------------|---------------|-----------------|
| **核心挑戰** | 流量控制 | 全局唯一 ID | 高頻寫入 |
| **時間窗口** | 秒級/分鐘級 | 無 | 小時級/天級 |
| **精度要求** | 高（< 5%） | 絕對（100%） | 可接受（~1%） |
| **分布式** | Redis + Lua | Snowflake | Redis + PG |
| **延遲要求** | < 5ms | < 10ms | < 50ms |
