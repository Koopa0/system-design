# Counter Service 系統設計文檔

## 場景：你是社群平台的後端工程師

### 產品經理的新需求

星期一早上 10 點，產品經理走到你的座位：

> **產品經理：** "我們需要在首頁顯示即時在線人數，就像 Discord 那樣。預計下週上線。"

你查看目前的技術規格：

```
當前系統狀況：
- 註冊用戶：500 萬
- DAU (日活躍用戶)：50 萬
- 尖峰在線人數：約 10 萬人
- 用戶平均在線時長：30 分鐘

需要追蹤的計數類型：
1. 即時在線人數（用戶登入 +1，登出 -1）
2. 文章閱讀次數（每次瀏覽 +1）
3. DAU 統計（同一用戶一天只計算一次）
```

你陷入思考：

- 如何處理高頻率的計數更新（每秒數千次）？
- 如何確保計數準確（不重複、不遺漏）？
- 如何避免資料庫被打垮？

### 你會問自己：

1. **直接寫資料庫可行嗎？**
   - 每次用戶上線就 UPDATE 資料庫？

2. **需要即時一致嗎？**
   - 在線人數延遲 1 秒顯示可以接受嗎？

3. **如何處理故障？**
   - 資料庫掛了怎麼辦？

---

## 第一次嘗試：直接寫入 PostgreSQL

### 最簡單的方案

你想：「計數很簡單，就是加減運算，直接用資料庫處理！」

```sql
-- 建立資料表
CREATE TABLE counters (
    name VARCHAR(255) PRIMARY KEY,
    value BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP
);

-- 用戶上線
UPDATE counters SET value = value + 1 WHERE name = 'online_users';

-- 用戶下線
UPDATE counters SET value = value - 1 WHERE name = 'online_users';
```

你在開發環境測試，一切正常。

### Go 語言實現

```go
type PostgreSQLCounter struct {
    db *sql.DB
}

func (c *PostgreSQLCounter) Increment(ctx context.Context, name string, delta int64) error {
    query := `
        INSERT INTO counters (name, value, updated_at)
        VALUES ($1, $2, NOW())
        ON CONFLICT (name) DO UPDATE
        SET value = counters.value + $2, updated_at = NOW()
    `
    _, err := c.db.ExecContext(ctx, query, name, delta)
    return err
}
```

### 時序範例

```
時間軸：正常流量
10:00:00 → 用戶 A 上線 → UPDATE counters (value = 1)
10:00:01 → 用戶 B 上線 → UPDATE counters (value = 2)
10:00:02 → 用戶 C 上線 → UPDATE counters (value = 3)

資料庫負載：3 次 UPDATE / 3 秒 = 1 QPS
延遲：每次 UPDATE 約 5-10 ms
結論：看起來很完美！
```

你部署到測試環境，開始進行壓力測試。

### 災難場景：資料庫鎖爭用

你使用 Apache Bench 模擬高並發：

```bash
ab -n 10000 -c 100 http://localhost:8080/counter/increment
```

監控面板瞬間爆紅：

```
壓測結果（10,000 次請求，100 並發）：
- 成功：8,234 次
- 失敗：1,766 次（timeout）
- P50 延遲：45 ms
- P99 延遲：3,200 ms (3.2 秒！)
- 資料庫 CPU：98%

PostgreSQL 日誌：
2025-01-15 10:15:32 ERROR: deadlock detected
2025-01-15 10:15:33 DETAIL: Process 1234 waits for ShareLock on transaction 5678
2025-01-15 10:15:33 DETAIL: Process 5678 waits for ShareLock on transaction 1234
```

**問題發現：行級鎖 (Row-Level Lock) 爭用**

```
視覺化問題：

時刻 T1：
Thread 1 → UPDATE counter (嘗試獲取鎖) → 等待
Thread 2 → UPDATE counter (嘗試獲取鎖) → 等待
Thread 3 → UPDATE counter (嘗試獲取鎖) → 等待
...
Thread 100 → UPDATE counter (嘗試獲取鎖) → 等待

問題：所有執行緒都在等待同一個資料行的鎖！
```

### 瓶頸計算

```
PostgreSQL 行級鎖特性：
- 同一時間只有一個事務可以修改特定資料行
- 其他事務必須排隊等待

實測數據：
- 單行 UPDATE 平均耗時：5 ms
- 理論最大 QPS：1000 ms / 5 ms = 200 QPS

但我們需要：
- 尖峰在線人數變化：10 萬人 / 30 分鐘 = 55 人/秒
- 再加上文章閱讀、按讚等操作：估計 1,000-5,000 QPS

結論：PostgreSQL 直接 UPDATE 完全無法應對！
```

### 你會問自己：

1. **為什麼會這麼慢？**
   - 每次 UPDATE 都要寫磁碟（WAL 日誌）
   - 行級鎖導致並發能力極低
   - 網路往返延遲（應用 → 資料庫）

2. **如何解決？**
   - 能否用記憶體加速？
   - 能否批量處理減少資料庫壓力？

---

## 第二次嘗試：純記憶體計數

### 新的想法

你想：「既然資料庫太慢，那就用記憶體！」

```go
type MemoryCounter struct {
    counts map[string]*atomic.Int64  // 使用 atomic 保證並發安全
    mu     sync.RWMutex
}

func (c *MemoryCounter) Increment(ctx context.Context, name string, delta int64) (int64, error) {
    c.mu.RLock()
    counter, exists := c.counts[name]
    c.mu.RUnlock()

    if !exists {
        c.mu.Lock()
        counter = &atomic.Int64{}
        c.counts[name] = counter
        c.mu.Unlock()
    }

    newValue := counter.Add(delta)
    return newValue, nil
}

func (c *MemoryCounter) Get(ctx context.Context, name string) (int64, error) {
    c.mu.RLock()
    defer c.mu.RUnlock()

    if counter, exists := c.counts[name]; exists {
        return counter.Load(), nil
    }
    return 0, nil
}
```

### 壓測結果

```
壓測結果（100,000 次請求，1,000 並發）：
- 成功：100,000 次
- 失敗：0 次
- P50 延遲：0.3 ms
- P99 延遲：1.2 ms
- CPU：15%

結論：快如閃電！
```

你興奮地準備上線。

### 災難場景：服務重啟後資料消失

週五下午，你部署新版本，重啟服務：

```
部署前：
- 在線人數：8,234 人
- 今日文章閱讀：1,250,000 次

部署後（服務重啟）：
- 在線人數：0 人（錯誤！實際仍有 8,000+ 人在線）
- 今日文章閱讀：0 次（所有計數歸零！）

用戶回饋：
"為什麼我的文章閱讀數突然變成 0？"
"首頁顯示在線人數 0 人，但我明明看到很多人在線"
```

**問題發現：記憶體資料不持久化**

```
問題本質：
- 記憶體資料儲存在 Process 的 Heap
- 服務重啟 → Process 終止 → Heap 記憶體釋放 → 資料全部消失

其他風險：
- 伺服器當機 → 資料遺失
- OOM (記憶體不足) → 資料遺失
- 容器重新調度 (Kubernetes) → 資料遺失
```

### 你會問自己：

1. **如何兼顧速度和持久化？**
   - 寫入要快（記憶體級別）
   - 資料要持久（不怕重啟）

2. **能否分層處理？**
   - 記憶體做快取
   - 資料庫做持久化
   - 如何同步兩者？

---

## 靈感：銀行的批量轉帳

你想起銀行的處理方式：

```
銀行不會這樣做：
每筆轉帳 → 立即更新總帳 → 效率極低

銀行實際做法：
1. 每筆交易 → 記錄在交易流水
2. 每小時/每天 → 批量彙總到總帳
3. 客戶查詢餘額 → 查總帳 + 未結算流水

優勢：
- 交易記錄快（只是 append）
- 批量更新總帳（減少鎖爭用）
- 資料不遺失（流水永久保存）
```

**關鍵洞察：**
- 寫入快取（記憶體）→ 立即回應
- 批量同步（資料庫）→ 減少壓力
- 兩者結合 → 速度 + 持久化

這就是 **Write-Behind Caching** 模式！

---

## 第三次嘗試：Redis + 批量同步

### 設計思路

```
架構：
1. 所有寫入先到 Redis（微秒級延遲）
2. 批量同步器定期將 Redis 資料同步到 PostgreSQL
3. 查詢優先從 Redis 讀取（最新）
4. Redis 故障時降級到 PostgreSQL

資料流：
用戶操作 → Redis INCR → 立即回應 ✅
           ↓ (非同步)
        批量同步器
           ↓ (每秒/每 100 筆)
        PostgreSQL ← 持久化儲存
```

### Redis 原子操作

```go
func (c *RedisCounter) Increment(ctx context.Context, name string, delta int64) (int64, error) {
    key := fmt.Sprintf("counter:%s", name)

    // Redis INCR 是原子操作，無需擔心並發問題
    newValue, err := c.redis.IncrBy(ctx, key, delta).Result()
    if err != nil {
        return 0, fmt.Errorf("redis incr failed: %w", err)
    }

    // 發送到批量同步佇列
    c.batchWriter.Submit(&BatchWrite{
        Name:  name,
        Delta: delta,
    })

    return newValue, nil
}
```

### 批量同步機制

```go
type BatchWriter struct {
    buffer chan *BatchWrite
    db     *sql.DB

    batchSize     int           // 100 筆
    flushInterval time.Duration // 1 秒
}

func (w *BatchWriter) Start() {
    ticker := time.NewTicker(w.flushInterval)
    batch := make(map[string]int64) // 操作合併

    for {
        select {
        case write := <-w.buffer:
            // 合併相同 counter 的操作
            batch[write.Name] += write.Delta

            // 達到批量大小，立即刷新
            if len(batch) >= w.batchSize {
                w.flush(batch)
                batch = make(map[string]int64)
            }

        case <-ticker.C:
            // 定時刷新（即使未達批量大小）
            if len(batch) > 0 {
                w.flush(batch)
                batch = make(map[string]int64)
            }
        }
    }
}

func (w *BatchWriter) flush(batch map[string]int64) error {
    // 使用單一事務批量更新
    tx, _ := w.db.Begin()

    for name, delta := range batch {
        tx.Exec(`
            INSERT INTO counters (name, value, updated_at)
            VALUES ($1, $2, NOW())
            ON CONFLICT (name) DO UPDATE
            SET value = counters.value + $2, updated_at = NOW()
        `, name, delta)
    }

    return tx.Commit()
}
```

### 時序範例

```
時間軸：高並發場景

10:00:00.000 → 100 個請求到達
10:00:00.001 → Redis INCR × 100 (微秒級完成)
10:00:00.002 → 回應所有請求 ✅

批量同步器（背景運作）：
10:00:00.000-10:00:00.999 → 收集 5,000 個操作到記憶體
10:00:01.000 → 合併操作：
   - counter:online_users → +234, -189 = +45
   - counter:article:1001 → +523
   - counter:article:1002 → +412
   ... (假設 100 個不同 counter)

10:00:01.001 → 單一事務寫入 PostgreSQL (100 筆 UPDATE)
10:00:01.050 → 資料庫更新完成 (50 ms)

效果對比：
不使用批量：5,000 次 UPDATE × 5 ms = 25,000 ms (25 秒)
使用批量：  100 次 UPDATE × 0.5 ms (事務內) = 50 ms

效能提升：500 倍！
```

### 為什麼這是最佳選擇？

對比所有方案：

| 特性 | 直接寫 PostgreSQL | 純記憶體 | Redis + 批量同步 |
|------|------------------|---------|-----------------|
| 寫入延遲 | 5-50 ms | < 1 ms | < 1 ms |
| 並發能力 | 200 QPS | 100K+ QPS | 80K+ QPS |
| 資料持久化 | 立即 | 無 | 1 秒延遲 |
| 服務重啟 | 資料安全 | 資料全失 | 資料安全 |
| 資料庫壓力 | 極高 | 無 | 極低（1/100） |
| 故障恢復 | 無需 | 不可能 | 可降級 |

**勝出原因：**
1. 兼具高效能（Redis）與持久化（PostgreSQL）
2. 批量合併減少 100 倍資料庫壓力
3. 最終一致性（1 秒延遲）對計數場景可接受
4. 可優雅降級，保證高可用

---

## 新挑戰：DAU 去重統計

### 場景升級

產品經理又來找你：

> **產品經理：** "我們需要統計每日活躍用戶數（DAU），同一用戶一天只計算一次。"

你查看數據：
```
每日登入事件：
- DAU：50 萬人
- 平均每人登入：3 次/天
- 總登入事件：150 萬次/天
- 需要去重：同一用戶只計算一次
```

### 第一次嘗試：PostgreSQL 唯一索引

最簡單的想法：

```sql
CREATE TABLE daily_active_users (
    user_id BIGINT,
    date DATE,
    PRIMARY KEY (user_id, date)
);

-- 用戶登入時
INSERT INTO daily_active_users (user_id, date)
VALUES (123, '2025-01-15')
ON CONFLICT DO NOTHING;  -- 重複就忽略

-- 查詢 DAU
SELECT COUNT(*) FROM daily_active_users WHERE date = '2025-01-15';
```

看起來很合理！

### 災難場景：資料庫壓力過大

你部署後發現：

```
監控數據（尖峰時段）：
- 登入請求：1,000 次/秒
- 每次都要 INSERT（即使 CONFLICT）
- 資料庫負載：
  - CPU：85%
  - 磁碟 IOPS：12,000 (接近上限 15,000)
  - 慢查詢：20% 的 INSERT 超過 100 ms

問題：
每次登入都要檢查資料庫是否已存在！
即使用戶已經計算過，仍要執行 INSERT 並觸發 CONFLICT 檢查
```

### 解決方案：Redis Set 去重

你意識到：

> "去重檢查不應該每次都打資料庫，應該用記憶體快取！"

```go
func (c *RedisCounter) IncrementDAU(ctx context.Context, name string, userID string) (int64, error) {
    today := time.Now().Format("2006-01-02")

    // Redis Set 用於去重
    dauKey := fmt.Sprintf("counter:%s:users:%s", name, today)

    // SADD 是原子操作，返回值：1=新增成功，0=已存在
    added, err := c.redis.SAdd(ctx, dauKey, userID).Result()
    if err != nil {
        return 0, err
    }

    if added > 0 {
        // 這是今天第一次見到此用戶，計數 +1
        value, _ := c.Increment(ctx, name, 1)

        // 設置過期時間：明天凌晨自動清理
        tomorrow := time.Now().AddDate(0, 0, 1)
        midnight := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(),
                             0, 0, 0, 0, time.Local)
        c.redis.ExpireAt(ctx, dauKey, midnight)

        return value, nil
    } else {
        // 用戶今天已經計算過，返回當前值（不增加）
        return c.Get(ctx, name)
    }
}
```

### 時序範例

```
場景：用戶多次登入

08:00:00 → 用戶 A (ID=1001) 第一次登入
           SADD counter:dau:users:2025-01-15 1001 → 返回 1 (新增)
           INCR counter:dau → 1
           回應：DAU = 1 ✅

10:30:00 → 用戶 A (ID=1001) 第二次登入（刷新頁面）
           SADD counter:dau:users:2025-01-15 1001 → 返回 0 (已存在)
           不增加計數
           回應：DAU = 1 ✅

12:00:00 → 用戶 B (ID=1002) 第一次登入
           SADD counter:dau:users:2025-01-15 1002 → 返回 1 (新增)
           INCR counter:dau → 2
           回應：DAU = 2 ✅

次日 00:00:00 → Redis 自動清理 counter:dau:users:2025-01-15
                新的一天重新計數
```

### 效能對比

```
PostgreSQL 唯一索引方案：
- 每次登入：INSERT + CONFLICT 檢查 = 10-50 ms
- 1,000 次登入/秒 = 1,000 次資料庫操作

Redis Set 方案：
- SADD 檢查：< 1 ms
- 只有新用戶才 INCR counter
- 1,000 次登入/秒，假設 50% 是重複 → 只需 500 次 INCR

效能提升：10-50 倍
資料庫壓力：降低 50%
```

---

## 新挑戰：Redis 故障時如何處理？

### 災難場景

凌晨 2 點，Redis 伺服器因為記憶體不足（OOM）重啟：

```
監控告警：
02:00:00 → Redis connection refused
02:00:01 → Counter service error rate: 100%
02:00:02 → 用戶回報：無法登入、文章閱讀數不更新

日誌：
ERROR: Redis connection failed: dial tcp 127.0.0.1:6379: connection refused
ERROR: Failed to increment counter: redis unavailable
```

你的服務完全停擺了！

### 第一次想法：直接返回錯誤

```go
func (c *RedisCounter) Increment(ctx context.Context, name string, delta int64) (int64, error) {
    newValue, err := c.redis.IncrBy(ctx, key, delta).Result()
    if err != nil {
        return 0, err  // 直接返回錯誤
    }
    return newValue, nil
}
```

**問題：** 計數功能完全不可用，影響用戶體驗。

### 解決方案：自動降級到 PostgreSQL

你意識到：

> "降級總比掛掉好！即使慢一點，至少能用。"

```go
type CounterWithFallback struct {
    redis          *RedisCounter
    postgres       *PostgreSQLCounter

    fallbackMode   atomic.Bool
    errorCount     atomic.Int32
    errorThreshold int32  // 連續失敗 3 次觸發降級
}

func (c *CounterWithFallback) Increment(ctx context.Context, name string, delta int64) (int64, error) {
    // 檢查是否在降級模式
    if c.fallbackMode.Load() {
        return c.postgres.Increment(ctx, name, delta)
    }

    // 嘗試 Redis
    value, err := c.redis.Increment(ctx, name, delta)
    if err != nil {
        // 錯誤計數
        count := c.errorCount.Add(1)

        if count >= c.errorThreshold {
            // 觸發降級
            c.fallbackMode.Store(true)
            log.Warn("Entering fallback mode: Redis unavailable")

            // 啟動健康檢查協程
            go c.healthCheck()
        }

        // 立即使用 PostgreSQL 處理此次請求
        return c.postgres.Increment(ctx, name, delta)
    }

    // 成功，重置錯誤計數
    c.errorCount.Store(0)
    return value, nil
}

func (c *CounterWithFallback) healthCheck() {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    for c.fallbackMode.Load() {
        <-ticker.C

        // 嘗試 Ping Redis
        if err := c.redis.Ping(); err == nil {
            log.Info("Redis recovered, exiting fallback mode")
            c.fallbackMode.Store(false)
            c.errorCount.Store(0)
            return
        }
    }
}
```

### 時序範例

```
正常模式：
10:00:00 → 請求 #1 → Redis INCR → 成功 (1 ms) ✅
10:00:01 → 請求 #2 → Redis INCR → 成功 (1 ms) ✅

Redis 故障：
10:00:02 → 請求 #3 → Redis INCR → 失敗 (errorCount = 1)
                     → 自動切換 PostgreSQL → 成功 (10 ms)
10:00:03 → 請求 #4 → Redis INCR → 失敗 (errorCount = 2)
                     → 自動切換 PostgreSQL → 成功 (10 ms)
10:00:04 → 請求 #5 → Redis INCR → 失敗 (errorCount = 3)
                     → 觸發降級模式 ⚠
                     → 啟動健康檢查協程

降級模式（所有請求走 PostgreSQL）：
10:00:05 → 請求 #6 → PostgreSQL UPDATE → 成功 (15 ms)
10:00:06 → 請求 #7 → PostgreSQL UPDATE → 成功 (12 ms)
...

健康檢查：
10:00:10 → Ping Redis → 失敗
10:00:20 → Ping Redis → 失敗
10:00:30 → Ping Redis → 成功 ✅
         → 退出降級模式
         → 恢復使用 Redis

恢復正常：
10:00:31 → 請求 #100 → Redis INCR → 成功 (1 ms) ✅
```

### 已知限制

```
降級模式下的限制：

1. DAU 去重功能失效
   原因：DAU 去重依賴 Redis SADD
   影響：Redis 故障期間，同一用戶可能被重複計數

   緩解方案（教學未實現）：
   - PostgreSQL 實現 DAU 去重表（user_id + date 唯一索引）
   - 接受短暫的不準確性（Redis 恢復後自動修正）

2. 效能下降
   正常：Redis 80K QPS
   降級：PostgreSQL 200 QPS
   影響：高流量時可能出現延遲

3. 批量同步失效
   原因：Redis 不可用，無法做快取
   影響：每個請求直接打資料庫，壓力增加
```

---

## 擴展性分析

### 當前架構容量

```
單機配置：
├─ Redis (16 GB 記憶體)
│  ├─ 寫入 QPS：80,000
│  ├─ 讀取 QPS：100,000
│  └─ DAU 儲存：50 萬用戶 × 100 bytes = 50 MB
│
└─ PostgreSQL (4 core, 16 GB)
   ├─ 批量寫入：100 批/秒
   ├─ 每批 100 個操作：10,000 QPS
   └─ 讀取 QPS：5,000

適用場景：
- DAU < 100 萬
- 計數 QPS < 10,000
- 成本：約 $200/月（AWS）
```

### 10 倍擴展（100,000 QPS）

**瓶頸分析：**
```
Redis：
- 當前：80K 寫入 QPS
- 需求：100K QPS
- 結論：單機 Redis 不足

PostgreSQL：
- 當前：100 批/秒 × 100 操作 = 10K QPS
- 需求：100K QPS
- 結論：需要增大批量或分片
```

**方案 1：增大批量大小**

```
調整參數：
- 批量大小：100 → 500
- 刷新間隔：1 秒 → 2 秒

計算：
- 500 操作/批 × 200 批/秒 = 100K QPS ✅

權衡：
- 優點：配置簡單，成本低
- 缺點：一致性延遲增加（1 秒 → 2 秒）
```

**方案 2：Redis 垂直擴展**

```
升級配置：
- 16 GB → 64 GB
- 6 core → 16 core

容量提升：
- 寫入 QPS：80K → 150K ✅
- 成本：$200/月 → $500/月
```

**方案 3：應用層分片**

```go
type ShardedCounter struct {
    shards []*RedisCounter  // 4 個 Redis 實例
}

func (c *ShardedCounter) getShard(name string) *RedisCounter {
    hash := crc32.ChecksumIEEE([]byte(name))
    return c.shards[hash % len(c.shards)]
}

func (c *ShardedCounter) Increment(ctx context.Context, name string, delta int64) (int64, error) {
    shard := c.getShard(name)
    return shard.Increment(ctx, name, delta)
}
```

容量：
- 4 個 shard × 80K QPS = 320K QPS
- 成本：$200/月 × 4 = $800/月

### 100 倍擴展（1,000,000 QPS）

需要架構升級：

```
架構：Redis Cluster + PostgreSQL 分片

Redis Cluster：
├─ 16 個 master 節點
├─ 每個：80K QPS
└─ 總容量：1.28M QPS ✅

PostgreSQL 分片：
├─ 16 個 shard（按 counter name hash）
├─ 每個：100 批/秒 × 1000 操作/批 = 100K QPS
└─ 總容量：1.6M QPS ✅

應用層：
├─ 一致性雜湊路由
├─ 自動故障轉移
└─ 讀寫分離（PostgreSQL replica）

成本估算（AWS）：
- Redis Cluster：16 × cache.r6g.xlarge = $3,200/月
- PostgreSQL：16 × db.m6g.xlarge = $4,000/月
- 負載平衡器：$500/月
- 總計：約 $7,700/月
```

---

## 真實工業案例

### Reddit (計數系統)

```
技術選型：Redis + Cassandra

配置：
- Redis：計數快取層
- Cassandra：持久化儲存（最終一致性資料庫）
- 批量同步：每 5 秒

特點：
- 支援海量計數（貼文投票、瀏覽數）
- 容忍最終一致性（投票數延遲 5 秒顯示）

為什麼選擇：
- Reddit 每日數億次投票操作
- 使用 Cassandra 而非 PostgreSQL：更好的水平擴展能力
- 5 秒延遲對用戶體驗影響小
```

### Twitter (時間線計數)

```
技術選型：Redis + Manhattan (自研 KV 儲存)

配置：
- Redis：即時計數（追蹤者數、按讚數）
- Manhattan：持久化層
- 批量聚合：每分鐘

特點：
- 支援數億用戶的追蹤者計數
- 即時更新（< 100 ms）

為什麼選擇：
- 自研 Manhattan 以滿足超大規模需求
- Redis 提供毫秒級讀寫
- 計數資料量大但單筆資料小，適合 KV 儲存
```

### Discord (在線人數統計)

```
技術選型：Redis Cluster + ScyllaDB

配置：
- Redis Cluster：分散式計數
- ScyllaDB：Cassandra 相容（C++ 改寫，更高效能）
- 更新頻率：即時

特點：
- 數百萬同時在線用戶
- 每個伺服器獨立計數
- 高可用性要求

為什麼選擇：
- ScyllaDB 比 Cassandra 效能更好（10 倍吞吐）
- Redis Cluster 自動分片
- 即時性要求高，無法接受批量延遲
```

---

## 實現範圍標註

### 已實現（核心教學內容）

| 功能 | 檔案 | 教學重點 |
|------|------|----------|
| **Redis 快取** | `counter.go:166-265` | 原子操作（INCR）、Lua script |
| **批量同步** | `counter.go:414-471` | Channel、批量處理、操作合併 |
| **DAU 去重** | `counter.go:186-218` | Redis Set、SADD、TTL 設置 |
| **降級機制** | `counter.go:495-537` | 錯誤閾值、自動切換、健康檢查 |
| **並發安全** | `counter.go:78-97` | atomic.Bool、sync.WaitGroup |

### 教學簡化（未實現）

| 功能 | 原因 | 生產環境建議 |
|------|------|-------------|
| **記憶體快取層** | 增加複雜度 | 本地 LRU cache + TTL，降級時使用 |
| **降級模式 DAU 去重** | 需要額外表結構 | PostgreSQL 唯一索引（user_id + date） |
| **監控指標** | 聚焦核心邏輯 | Prometheus metrics、延遲百分位數 |
| **分散式追蹤** | 單機足夠示範 | OpenTelemetry、Jaeger |
| **完整背壓處理** | 示範基本思路 | 限流、熔斷、降級多層防護 |

### 生產環境額外需要

```
1. 可觀測性
   - Metrics：QPS、延遲百分位數（P50/P99/P999）、錯誤率
   - Logging：結構化日誌、請求追蹤 ID
   - Tracing：分散式追蹤鏈路（Jaeger/Zipkin）
   - Alerting：異常自動告警（PagerDuty）

2. 可靠性
   - Redis Sentinel/Cluster（高可用性）
   - PostgreSQL 主從複製（Read Replica）
   - 多區域部署（容災）
   - 限流熔斷（Circuit Breaker 保護下游）

3. 運維
   - 容量規劃：根據歷史資料預測
   - 成本優化：冷熱資料分離
   - 資料歸檔：歷史資料遷移到 S3
   - 災難演練：定期 Chaos Engineering 測試

4. 安全
   - 認證授權：API key、OAuth 2.0
   - 速率限制：防止 API 濫用
   - 審計日誌：操作追蹤
   - 資料加密：傳輸層 TLS、靜態資料加密
```

---

## 你學到了什麼？

### 1. 從錯誤中學習

```
錯誤方案的價值：

方案 A：直接寫 PostgreSQL
發現：行級鎖爭用，並發能力只有 200 QPS
教訓：高頻寫入不能直接用關聯式資料庫

方案 B：純記憶體計數
發現：服務重啟後資料全部遺失
教訓：持久化很重要，不能只靠記憶體

方案 C：Redis + 批量同步
成功：兼具高效能與持久化
教訓：分層架構，各層發揮所長
```

### 2. 完美方案不存在

```
所有方案都有權衡：

Redis + PostgreSQL：
優勢：高效能（80K QPS）、持久化、可降級
劣勢：最終一致性（1 秒延遲）、架構複雜

純 PostgreSQL：
優勢：架構簡單、強一致性
劣勢：效能差（200 QPS）、無法應對高並發

純記憶體：
優勢：效能極佳（100K+ QPS）
劣勢：資料不持久、無法容錯

教訓：根據業務需求選擇，沒有銀彈！
```

### 3. 真實場景驅動設計

```
問題演進：

第一階段：基本計數
→ 需求：在線人數統計
→ 方案：Redis INCR

第二階段：高並發
→ 需求：10,000 QPS 寫入
→ 方案：批量同步降低資料庫壓力

第三階段：去重需求
→ 需求：DAU 統計（同一用戶只算一次）
→ 方案：Redis Set + SADD

第四階段：容錯需求
→ 需求：Redis 故障時不能完全停擺
→ 方案：自動降級到 PostgreSQL

教訓：系統設計是逐步演進的，不是一步到位
```

### 4. 工業界如何選擇

| 場景 | 推薦方案 | 原因 |
|------|---------|------|
| **小型服務**<br>QPS < 1,000 | PostgreSQL | 架構簡單，成本低 |
| **中型服務**<br>QPS 1K-50K | Redis + PostgreSQL<br>（本章方案） | 效能與成本平衡 |
| **大型服務**<br>QPS > 100K | Redis Cluster +<br>NoSQL（Cassandra/ScyllaDB） | 水平擴展能力強 |
| **金融交易** | PostgreSQL<br>（強一致性） | 不能接受資料延遲 |
| **社群媒體** | Redis + NoSQL<br>（最終一致性） | 效能優先，可容忍延遲 |

---

## 總結

Counter Service 展示了**高並發計數系統**的演進過程：

1. **發現問題**：PostgreSQL 行級鎖導致並發能力只有 200 QPS
2. **嘗試方案**：純記憶體快（100K QPS）但不持久
3. **最終方案**：Redis（快取）+ PostgreSQL（持久化）+ 批量同步
4. **持續演進**：加入 DAU 去重、故障降級

**核心思想：** 用空間（Redis 記憶體）換時間（低延遲），用最終一致性換高吞吐量。

**適用場景：**
- 在線人數統計
- 文章閱讀次數
- 按讚、分享計數
- DAU/MAU 統計
- 任何高頻計數需求

**不適用：**
- 金融交易（需要強一致性，每筆都要立即持久化）
- 庫存扣減（不能超賣，需要原子性保證）
- 帳戶餘額（不能接受最終一致性）

**關鍵權衡：**
- 一致性 vs 效能（最終一致性換取高吞吐）
- 複雜度 vs 可擴展性（分層架構更複雜但更靈活）
- 成本 vs 可用性（降級機制增加成本但提升可用性）
