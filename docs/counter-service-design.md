# Counter Service 系統設計文檔

## 📋 問題定義

### 業務需求
構建一個高並發計數服務，用於追蹤：
- **在線人數統計**：實時顯示當前在線用戶
- **DAU 統計**：每日活躍用戶數（需去重）
- **點擊計數**：文章閱讀、按讚等

### 技術指標
| 指標 | 目標值 | 挑戰 |
|------|--------|------|
| **QPS** | 10,000 | 如何承受高頻寫入？ |
| **準確性** | 100% | 併發環境下不丟失計數 |
| **延遲** | P99 < 10ms | 如何保持低延遲？ |
| **可用性** | 99.9% | Redis 故障時如何處理？ |
| **去重** | DAU 準確 | 同一用戶一天只計算一次 |

---

## 🤔 設計決策樹

### 決策 1：如何處理 10,000 QPS 寫入？

```
需求：每秒 10,000 次計數更新

❌ 方案 A：直接寫入 PostgreSQL
   問題：PostgreSQL 單機寫入 ~1,000-5,000 QPS
   結果：資料庫成為瓶頸，延遲暴增

❌ 方案 B：只用記憶體（map + mutex）
   問題：服務重啟後資料全部遺失
   結果：不可接受的資料遺失

✅ 方案 C：Redis + PostgreSQL 雙寫
   優勢：
   - Redis 內存操作，微秒級延遲
   - PostgreSQL 持久化，重啟不丟數據
   - 批量同步降低 DB 壓力

   權衡：最終一致性（數秒延遲）vs 強一致性
```

**選擇：方案 C（Redis + PostgreSQL）**

---

### 決策 2：如何保證資料持久化？

```
問題：Redis 的資料如何同步到 PostgreSQL？

❌ 方案 A：每次 INCR 後同步寫 DB
   計算：10,000 QPS → 10,000 次 DB 寫入
   問題：PostgreSQL 無法承受

❌ 方案 B：定期全量同步（如每分鐘）
   問題：Redis 故障時會丟失整分鐘的資料
   計算：故障時最多丟失 60 * 10,000 = 600,000 次操作

✅ 方案 C：批量異步同步
   機制：
   - 緩衝 channel：收集操作
   - 定時刷新：每秒或達到批量大小
   - 操作合併：相同 counter 的多次操作合併

   效果：10,000 QPS → 約 100 次 DB 寫入（降低 100 倍）
```

**選擇：方案 C（批量異步同步）**

**實現細節：**
```go
// 批量大小：100（平衡延遲與吞吐）
// 刷新間隔：1 秒（最終一致性延遲 < 1 秒）
// 緩衝區：200（允許 2x 突發流量）

batchBuffer: make(chan *batchWrite, config.BatchSize*2)
ticker := time.NewTicker(config.FlushInterval)

// 操作合併範例
// counter:online +1, +1, -1 → 最終只寫一次 DB (value=1)
```

---

### 決策 3：如何實現 DAU 去重？

```
需求：同一用戶一天內多次登入，只計算一次

❌ 方案 A：PostgreSQL 唯一索引（user_id + date）
   問題：每次檢查都要查 DB，延遲高
   計算：10,000 QPS → 10,000 次 SELECT 查詢

❌ 方案 B：應用層記憶體 Set（Go map）
   問題：多實例部署時無法共享狀態
   結果：用戶可能在不同伺服器被重複計數

✅ 方案 C：Redis Set（SADD）
   機制：
   - Key: counter:{name}:users:{date}
   - SADD 原子操作，天然去重
   - TTL：次日凌晨自動清理

   優勢：
   - O(1) 時間複雜度
   - 原子性保證
   - 自動過期清理
```

**選擇：方案 C（Redis Set + SADD）**

**實現細節：**
```go
// 去重邏輯
dauKey := fmt.Sprintf("counter:%s:users:%s", name, today)
added, _ := redis.SAdd(ctx, dauKey, userID).Result()

if added > 0 {
    // 新用戶，設置 TTL 到次日凌晨
    midnight := time.Date(tomorrow, 0, 0, 0, 0, location)
    redis.ExpireAt(ctx, dauKey, midnight)
    value = added
} else {
    // 重複用戶，返回當前值
    return getCurrentValue(ctx, name)
}
```

---

### 決策 4：Redis 故障時如何處理？

```
場景：Redis 網路故障、OOM、重啟

❌ 方案 A：返回錯誤，服務不可用
   影響：計數功能完全掛掉

❌ 方案 B：丟棄計數，假裝成功
   問題：資料遺失，統計不準確

✅ 方案 C：降級到 PostgreSQL
   機制：
   - 連續錯誤閾值：3-5 次（避免單次抖動）
   - 自動切換：fallbackMode = true
   - 自動恢復：定期 Ping Redis，恢復後切回

   權衡：
   - 性能下降：Redis < 1ms → PostgreSQL 10-50ms
   - 吞吐下降：10,000 QPS → 1,000 QPS
   - 但保證可用：降級總比掛掉好
```

**選擇：方案 C（自動降級機制）**

**已知限制：**
```
⚠️ 降級模式下 DAU 去重被繞過
   原因：DAU 去重依賴 Redis SADD
   影響：Redis 故障期間，同一用戶可能被重複計數

   緩解方案（教學未實現）：
   1. PostgreSQL 實現 DAU 去重表（user_id + date 唯一索引）
   2. 應用層記憶體快取暫存已計數的用戶
   3. 接受短暫的不準確性（Redis 恢復後自動修正）
```

---

## 📈 擴展性分析

### 當前架構容量

```
單機 Redis：
- QPS: ~100,000（讀）/ ~80,000（寫）
- 記憶體：DAU 100 萬用戶 × 100 bytes = 100 MB
- 結論：10,000 QPS 輕鬆應對

單機 PostgreSQL：
- 批量寫入：100 次/秒
- 每批 100 個操作 = 10,000 次計數操作/秒
- 結論：當前需求下足夠
```

### 10x 擴展（100K QPS）

```
瓶頸分析：
✅ Redis：仍可承受（80K 寫入 QPS）
❌ PostgreSQL：1,000 批/秒，需優化

方案 1：增大批量大小
- 批量：100 → 500
- 效果：5,000 操作/批 × 20 批/秒 = 100K QPS
- 權衡：一致性延遲增加（1s → 5s）

方案 2：PostgreSQL 垂直擴展
- 增加 CPU、記憶體
- SSD → NVMe
- 效果：寫入能力 × 2-3

方案 3：PostgreSQL 分片
- 按 counter name hash 分片
- 8 個 shard = 8x 寫入能力
- 複雜度：查詢需要聚合
```

### 100x 擴展（1M QPS）

```
需要架構升級：

1. Redis Cluster
   - 分片：16 個 master
   - 每個 shard：60K QPS
   - 總容量：960K QPS

2. PostgreSQL 分片集群
   - 16 個 shard
   - 每個：1,000 批/秒 × 500 操作/批 = 500K QPS
   - 總容量：8M QPS（超過需求）

3. 應用層分片路由
   - 一致性雜湊決定 Redis/PG shard
   - 自動故障轉移
   - 監控告警

成本：
- Redis Cluster：16 × 16GB = 256GB 記憶體
- PG Cluster：16 × 4 core = 64 core
- 估算：~$5,000/月（AWS）
```

---

## 🔧 實現範圍標註

### ✅ 已實現（核心教學內容）

| 功能 | 檔案 | 教學重點 |
|------|------|----------|
| **Redis 快取** | `counter.go:166-265` | 原子操作（INCR）、Lua script |
| **批量同步** | `counter.go:414-471` | Channel、批量處理、操作合併 |
| **DAU 去重** | `counter.go:186-218` | Redis Set、SADD、TTL 設置 |
| **降級機制** | `counter.go:495-537` | 錯誤閾值、自動切換、健康檢查 |
| **並發安全** | `counter.go:78-97` | atomic.Bool、sync.WaitGroup |

### ⚠️ 教學簡化（未實現）

| 功能 | 原因 | 生產環境建議 |
|------|------|-------------|
| **記憶體快取** | 增加複雜度 | LRU cache + TTL，降級時使用 |
| **降級模式 DAU 去重** | 需要額外表結構 | PostgreSQL 唯一索引（user_id + date） |
| **監控指標** | 聚焦核心邏輯 | Prometheus metrics、分位數延遲 |
| **分散式追蹤** | 單機足夠示範 | OpenTelemetry、Jaeger |
| **背壓完整處理** | 示範基本思路 | 限流、熔斷、降級多層防護 |

### 🚀 生產環境額外需要

```
1. 可觀測性
   - Metrics：QPS、延遲分位數、錯誤率
   - Logging：結構化日誌、請求追蹤
   - Tracing：分散式追蹤鏈路
   - Alerting：異常自動告警

2. 可靠性
   - Redis Sentinel/Cluster（高可用）
   - PostgreSQL 主從複製（備份）
   - 多區域部署（容災）
   - 限流熔斷（保護下游）

3. 運維
   - 容量規劃：根據歷史資料預測
   - 成本優化：冷熱資料分離
   - 資料歸檔：歷史資料遷移到 S3
   - 災難演練：定期故障注入測試

4. 安全
   - 認證授權：API key、OAuth
   - 速率限制：防止濫用
   - 審計日誌：操作追蹤
   - 資料加密：傳輸層 TLS
```

---

## 💡 關鍵設計原則總結

### 1. 分層快取策略
```
請求 → Redis（微秒）→ PostgreSQL（毫秒）
      ↓ 批量合併
      減少 100x DB 壓力
```

### 2. 最終一致性權衡
```
強一致性：每次操作都寫 DB（慢、低吞吐）
      vs
最終一致性：批量異步同步（快、高吞吐、數秒延遲）

選擇：計數場景可接受數秒延遲，優先吞吐量
```

### 3. 優雅降級
```
100% 功能 + Redis → 核心功能 + PostgreSQL → 返回錯誤
  (正常)            (降級，性能下降)          (最後防線)

降級總比掛掉好！
```

### 4. 原子操作利用
```
Redis INCR：原子性、高性能
Redis SADD：去重 + 原子性

避免應用層實現複雜邏輯，善用資料庫特性
```

---

## 📚 延伸閱讀

### 相關系統設計問題
- 如何設計一個**限流服務**？（參考：Rate Limiter）
- 如何設計一個**分散式鎖**？
- 如何設計一個**實時排行榜**？（Redis Sorted Set）

### 系統設計模式
- **Write-Behind Caching**：批量異步寫入
- **Circuit Breaker**：熔斷器模式（降級）
- **Graceful Degradation**：優雅降級

### 容量規劃範例
```
日活 1,000 萬用戶
平均每人操作 20 次 = 2 億次操作/天
2 億 / 86,400 秒 = ~2,300 QPS（平均）
峰值（平均 × 5）= 11,500 QPS

→ 選型：單機 Redis + PostgreSQL（批量）
→ 成本：~$500/月（AWS）
→ 冗餘：2x 容量（應對突發）
```

---

## 🎯 總結

Counter Service 展示了**高並發寫入**的經典解決方案：

1. **多層快取**：Redis + PostgreSQL，各司其職
2. **批量處理**：降低 100x 資料庫壓力
3. **優雅降級**：保證高可用性
4. **原子操作**：利用 Redis 特性實現去重

**核心思想：** 用空間（Redis 記憶體）換時間（低延遲），用最終一致性換高吞吐量。

**適用場景：** 任何需要高頻計數的場景（點讚、閱讀、在線人數等）

**不適用：** 需要強一致性的金融交易（每筆都要立即持久化）
