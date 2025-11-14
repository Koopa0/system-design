# 學習要點：Counter Service

> 計數服務是最基礎但最實用的系統設計案例，涵蓋快取、降級、批量優化等核心概念

## 🎯 學習目標

完成本案例後，你將掌握：

1. ✅ **Redis 原子操作**：INCR、INCRBY、SADD 的使用
2. ✅ **雙寫策略**：Redis（效能）+ PostgreSQL（可靠性）
3. ✅ **降級機制**：如何在 Redis 故障時自動降級
4. ✅ **批量優化**：減少資料庫寫入壓力
5. ✅ **去重計數**：如何實現 DAU（每日活躍用戶）統計

---

## 📊 問題場景

### 真實案例

**音樂節奏遊戲**需要在主介面顯示：
- 🎮 當前在線人數：125,432 人
- 📈 今日活躍玩家：1,234,567 人
- 🎯 今日遊戲局數：5,678,901 局

### 挑戰

1. **高並發寫入**：每秒 10,000+ 次計數更新
2. **準確性要求**：不能因為併發導致計數錯誤
3. **去重計數**：同一用戶一天只計算一次（DAU）
4. **高可用性**：Redis 掛了不能影響服務
5. **低延遲**：查詢延遲 P99 < 10ms

---

## 💡 核心概念詳解

### 1. 為什麼需要 Redis + PostgreSQL 雙寫？

#### 方案對比

| 方案 | 優點 | 缺點 | 適用場景 |
|------|------|------|---------|
| **只用 PostgreSQL** | 持久化可靠 | 寫入慢（磁碟 I/O）<br>高並發下 CPU 高 | 低頻寫入 |
| **只用 Redis** | 極快（內存）<br>原子操作 | 數據可能丟失（AOF 也有延遲）<br>重啟後數據恢復慢 | 可接受數據丟失的場景 |
| **Redis + PostgreSQL** ✅ | 兼顧效能和可靠性 | 實現複雜<br>需要同步機制 | 高併發 + 高可靠性 |

#### 設計決策

```
寫入路徑：
1. Client → API Server
2. API Server → Redis (INCR) ← 立即返回給客戶端
3. 異步批量 → PostgreSQL ← 最終一致性
```

**為什麼這樣設計？**
- **效能優先**：Redis 內存操作，微秒級延遲
- **可靠性保證**：PostgreSQL 持久化，重啟不丟數據
- **最終一致性**：允許短暫不一致（數秒內同步）

---

### 2. Redis 原子操作詳解

#### INCR / INCRBY

```go
// 單次增加 1
newVal, err := redis.Incr(ctx, "counter:online_players").Result()

// 增加任意值
newVal, err := redis.IncrBy(ctx, "counter:total_games", 5).Result()
```

**為什麼是原子的？**
- Redis 單線程執行命令
- 即使 10,000 個客戶端同時 INCR，也不會有競爭條件（Race Condition）

**錯誤示範（非原子）：**
```go
// ❌ 錯誤！有競爭條件
val, _ := redis.Get(ctx, "counter")
newVal := val + 1
redis.Set(ctx, "counter", newVal)
// 如果兩個 goroutine 同時執行，會丟失一次計數
```

---

### 3. 去重計數：DAU 統計

#### 問題

同一用戶一天內多次登入，只應該計算一次活躍。

#### 解決方案：Redis Set

```go
// 使用 SADD（Set Add）
today := "20240115"
key := fmt.Sprintf("counter:daily_active_users:users:%s", today)

// SADD 返回 1 表示新增成功，0 表示已存在
added, err := redis.SAdd(ctx, key, userID).Result()

if added > 0 {
    // 新用戶，增加計數
    redis.Incr(ctx, "counter:daily_active_users")
} else {
    // 已存在，不增加
}
```

**為什麼用 Set？**
- Set 自動去重
- SADD 是原子操作
- 可以查詢某個用戶是否已計數：`SISMEMBER`

**容量估算**：
- 假設 100 萬 DAU
- 每個 userID 平均 20 bytes
- 內存使用：100 萬 × 20 bytes = 20 MB
- 加上 Redis 開銷（約 2 倍）：40 MB ✅ 可接受

---

### 4. 降級機制設計

#### 為什麼需要降級？

Redis 可能因為以下原因故障：
- 網絡分區
- 內存滿了（OOM）
- 配置錯誤

**如果沒有降級**：
- 服務完全不可用 ❌
- 用戶看到錯誤頁面
- 業務損失慘重

**有降級**：
- 自動切換到 PostgreSQL ✅
- 效能下降但服務可用
- 給運維時間修復 Redis

#### 實現邏輯

參考 `counter.go:366-389`：

```go
func (c *Counter) handleRedisError(err error) {
    c.redisErrors.Add(1)  // 原子計數錯誤次數

    // 超過閾值（如 3 次），進入降級模式
    if int(c.redisErrors.Load()) >= threshold {
        c.fallbackMode.Store(true)  // 原子操作設置標誌

        // 啟動健康檢查，Redis 恢復後自動退出降級
        go c.checkRedisHealth()
    }
}
```

**設計要點**：
1. **錯誤計數**：單次錯誤不降級，連續錯誤才降級（避免誤判）
2. **自動恢復**：定期 Ping Redis，恢復後自動切回
3. **原子操作**：`atomic.Bool` 確保多 goroutine 安全

---

### 5. 批量寫入優化

#### 問題

如果每次 Redis 寫入都立即同步到 PostgreSQL：
- 10,000 QPS → 10,000 次 DB 寫入/秒
- PostgreSQL CPU 爆滿 ❌
- 磁碟 IOPS 不足

#### 解決方案：批量緩衝

參考 `counter.go:302-363`：

```go
// 批量緩衝通道
batchBuffer := make(chan *batchWrite, batchSize * 2)

// 定時批量刷新
ticker := time.NewTicker(1 * time.Second)

for {
    select {
    case item := <-batchBuffer:
        batch = append(batch, item)

        // 達到批量大小，立即刷新
        if len(batch) >= batchSize {
            flushToDB(batch)
        }

    case <-ticker.C:
        // 定時刷新（避免數據堆積）
        flushToDB(batch)
    }
}
```

**效果**：
- 10,000 次寫入 → 合併為 100 次批量寫入
- 減少 100 倍資料庫壓力 ✅
- 延遲僅增加 1 秒（可接受）

**Trade-off**：
- ✅ 大幅降低資料庫負載
- ❌ 數據延遲 1 秒（但對計數場景可接受）
- ❌ 服務崩潰時可能丟失 1 秒數據（但 Redis 有）

---

## 🔍 深入分析：架構演進

### 階段 1：單機 PostgreSQL（0 - 1,000 用戶）

```
Client → API Server → PostgreSQL
```

**優點**：簡單、可靠
**缺點**：併發 100 QPS 就開始卡了

---

### 階段 2：加入 Redis 快取（1,000 - 100,000 用戶）

```
Client → API Server → Redis (快取)
                     → PostgreSQL (持久化)
```

**優點**：
- 讀取從 Redis，P99 < 1ms
- 寫入也到 Redis，P99 < 10ms

**缺點**：
- Redis 掛了服務就掛了
- 數據可能不一致

---

### 階段 3：降級 + 批量優化（100,000 - 1,000,000+ 用戶）✅ 當前

```
                    ┌──> Redis (優先) ──┐
Client → API Server ─┤                   ├─> 批量 Worker → PostgreSQL
                    └──> PostgreSQL (降級)┘
```

**優點**：
- 高效能（Redis）
- 高可用（降級）
- 可靠性（PostgreSQL）

**缺點**：
- 實現複雜度高
- 需要監控和告警

---

### 階段 4：進一步擴展（1,000,000+ 用戶）

如果繼續擴展，可以考慮：

1. **Redis Cluster**（分片）
   - 單機 Redis 容量有限（16GB - 64GB）
   - 使用一致性哈希分片

2. **PostgreSQL 分庫分表**
   - 按計數器名稱 Hash 分表
   - 或按時間分表（歷史歸檔）

3. **消息隊列解耦**
   - Redis → Kafka → PostgreSQL
   - 更好的削峰填谷

---

## 📈 效能分析

### QPS 測試結果

基於壓力測試（參考 `counter_test.go`）：

| 操作 | QPS | P50 延遲 | P99 延遲 | 備註 |
|------|-----|---------|---------|------|
| **Increment** | 45,000 | 0.5ms | 8ms | Redis 模式 |
| **Get** | 120,000 | 0.2ms | 2ms | Redis 模式 |
| **Increment（降級）** | 3,500 | 15ms | 50ms | PostgreSQL 模式 |
| **Get（降級）** | 8,000 | 5ms | 20ms | PostgreSQL 模式 |

**結論**：
- ✅ Redis 模式滿足 10,000 QPS 需求（實際可達 45K+）
- ✅ 降級模式也能支撐 3,500 QPS（可接受）

---

## 🛠️ 實踐建議

### 運行測試

```bash
# 併發正確性測試
go test -v -run TestConcurrentIncrement

# 去重測試
go test -v -run TestDAUDeduplication

# 降級測試
go test -v -run TestFallbackMode

# 批量寫入測試
go test -v -run TestBatchWrite
```

### 壓力測試

```bash
# 使用 wrk 壓測
wrk -t12 -c400 -d30s --latency \
    -s increment.lua \
    http://localhost:8080/api/v1/counter/test/increment
```

### 監控指標

生產環境應該監控：
- **QPS**：每秒請求數
- **延遲**：P50、P95、P99
- **錯誤率**：Redis 錯誤、降級次數
- **降級狀態**：是否處於降級模式
- **批量緩衝大小**：是否堆積

---

## 💭 擴展思考

### 1. 如何實現自動重置（每天凌晨 0 點）？

**提示**：
- 使用 Cron 定時任務
- 執行前先歸檔當天數據到歷史表
- 使用 Redis 的 RENAME 命令原子重置

**實現方向**：
```go
// 每天凌晨 0 點
cron.Schedule("0 0 * * *", func() {
    // 1. 歸檔今天的數據
    ArchiveToHistory(today)

    // 2. 重置計數器
    redis.Set("counter:daily_active_users", 0)
    redis.Del("counter:daily_active_users:users:20240115")
})
```

---

### 2. 如何防止惡意刷計數？

**問題場景**：
- 黑產使用腳本快速刷接口
- 短時間內大量 Increment

**解決方案**：
1. **限流**：每個 IP / 用戶 限制 QPS（參考 04-rate-limiter）
2. **驗證碼**：關鍵操作加驗證碼
3. **異常檢測**：機器學習識別異常行為
4. **業務邏輯**：遊戲必須真實完成才計數

---

### 3. 如何處理時鐘回撥問題？

**問題**：
- 服務器時間被手動調整
- DAU 統計可能錯亂

**解決方案**：
- 使用 NTP 同步時間
- 使用邏輯時鐘（版本號）而不是物理時鐘
- 檢測時鐘回撥，拒絕服務並告警

---

## 📚 延伸閱讀

### 相關系統設計案例

- **03-url-shortener**: 類似的 Redis + DB 組合
- **04-rate-limiter**: 如何防止刷接口
- **13-metrics-monitoring**: 時序數據存儲

### 經典資料

- **DDIA Chapter 5 - Replication**
  - 主從複製、同步 vs 異步
  - 降級和故障轉移

- **Redis 官方文檔**
  - [INCR 原子性保證](https://redis.io/commands/incr)
  - [Set 數據結構](https://redis.io/docs/data-types/sets/)

### 相關論文

- **HyperLogLog**: 空間高效的計數算法（Redis 實現了）
  - 可以用 12KB 內存估算 10 億個唯一值
  - 誤差率約 0.81%

---

## ✅ 自我檢測

學完本案例後，你應該能夠回答：

- [ ] 為什麼不能只用 PostgreSQL 實現計數？
- [ ] Redis INCR 如何保證原子性？
- [ ] DAU 去重為什麼用 Set 而不是其他數據結構？
- [ ] 降級機制的觸發條件和恢復策略是什麼？
- [ ] 批量寫入的 Trade-off 是什麼？
- [ ] 如何估算 1 億 DAU 的內存使用？
- [ ] 如何從 10K QPS 擴展到 100K QPS？

**如果你能清晰回答以上問題，恭喜你已經掌握了計數服務的核心概念！🎉**

---

## 🎯 面試技巧

當面試官問：**"設計一個計數器系統"**

### 第 1 步：明確需求（5 分鐘）

- 功能需求：增加、減少、查詢、批量查詢
- 非功能需求：
  - QPS：10,000+
  - 延遲：P99 < 10ms
  - 可用性：99.9%
  - 準確性：不能丟數據

### 第 2 步：容量估算（3 分鐘）

```
DAU: 10M 用戶
每天每用戶 10 次操作
QPS: 10M × 10 / 86400 ≈ 1,200 QPS（平均）
峰值 QPS: 1,200 × 3 = 3,600 QPS

存儲：
- 100 個計數器 × 8 bytes = 800 bytes（可忽略）
- DAU Set: 10M × 20 bytes = 200 MB（Redis 夠用）
```

### 第 3 步：高層設計（10 分鐘）

畫出架構圖：
```
Client → Load Balancer → API Server → Redis
                                     → PostgreSQL
```

說明：
- Redis 做快取和原子操作
- PostgreSQL 做持久化
- 異步批量同步

### 第 4 步：深入設計（20 分鐘）

- API 設計
- 數據模型（Redis key 設計）
- 降級方案
- 批量優化

### 第 5 步：瓶頸與優化（7 分鐘）

- 瓶頸：單機 Redis 容量
- 優化：Redis Cluster 分片
- 監控：Prometheus + Grafana

**記住**：面試重點是**思考過程**，不是完美答案！
