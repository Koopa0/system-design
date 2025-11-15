# Message Queue 系統設計文檔

## 問題定義

### 業務需求
構建分布式消息隊列系統，用於：
- **異步任務處理**：發送郵件、生成報表、圖片處理
- **服務解耦**：訂單服務 → 庫存服務、通知服務
- **削峰填谷**：秒殺系統、高峰流量緩衝
- **事件驅動架構**：微服務之間的事件通訊

### 技術指標
| 指標 | 目標值 | 挑戰 |
|------|--------|------|
| **吞吐量** | 100K msg/s | 如何達到高吞吐？ |
| **延遲** | P99 < 10ms | 如何保持低延遲？ |
| **可靠性** | 至少一次送達 | 消息如何不丟失？ |
| **可擴展** | 水平擴展 | 如何分布式部署？ |
| **持久化** | 重啟不丟失 | 如何平衡性能與持久化？ |

### 容量估算
```
假設：
- 日活用戶：1000 萬
- 每用戶操作：10 次/天（觸發異步任務）
- 峰值係數：5x（高峰時段）

計算：
- 日均消息數：1000 萬 × 10 = 1 億
- 平均 QPS：1 億 / 86400 ≈ 1,160 msg/s
- 峰值 QPS：1,160 × 5 = 5,800 msg/s
- 單機容量：NATS 可達 100K+ msg/s → 單機足夠

存儲估算（假設消息保留 7 天）：
- 單條消息大小：1 KB（平均）
- 7 天總量：1 億 × 7 = 7 億條
- 存儲需求：7 億 × 1 KB ≈ 700 GB
```

---

## 設計決策樹

### 決策 1：選擇哪種 Message Queue？

```
需求：輕量級、高性能、易於運維的消息隊列

方案 A：Redis List/Pub-Sub
   機制：
   - List：LPUSH/BRPOP 實現簡單隊列
   - Pub-Sub：發布訂閱模式

   問題：
   - 不是專業 MQ：無 ACK 機制、無重試
   - 持久化限制：AOF 性能影響大
   - 功能受限：無消費者組、無消息回溯
   - 記憶體限制：所有消息必須在記憶體中

   範例（丟失消息場景）：
   - T1: Consumer BRPOP 獲取消息
   - T2: Consumer 處理中崩潰
   - 結果：消息永久丟失（無 ACK 機制）

方案 B：RabbitMQ
   機制：AMQP 協議，Exchange + Queue 模型

   優勢：
   - 功能完整：路由、優先級、延遲隊列
   - 成熟穩定：工業界廣泛使用

   問題：
   - 過度複雜：Exchange 類型（Direct、Topic、Fanout、Headers）
   - Erlang 生態：與 Go 專案語言不一致
   - 學習曲線陡峭：概念多、配置複雜
   - 性能一般：~20K-50K msg/s（單機）

   適用場景：複雜路由、企業集成

方案 C：Kafka
   機制：分布式日誌，Topic + Partition 模型

   優勢：
   - 超高吞吐：百萬級 msg/s
   - 消息持久化：磁盤順序寫入
   - 消息回溯：Consumer 可重複消費

   問題：
   - 設計目標不同：為大數據日誌收集設計
   - 重量級：依賴 ZooKeeper、配置複雜
   - 延遲較高：批量寫入導致 P99 延遲 ~50-100ms
   - 過度設計：對於簡單異步任務過於複雜

   適用場景：日誌聚合、事件流、大數據管道

選擇方案 D：NATS + JetStream
   機制：
   - Core NATS：輕量級 Pub-Sub（火後即忘）
   - JetStream：持久化層（At-least-once、Exactly-once）

   優勢：
   - 輕量級：單一二進制檔案，Go 原生實現
   - 高性能：百萬級 msg/s、微秒級延遲
   - 簡單易用：API 直觀、配置簡單
   - 功能完整：消費者組、ACK、重試、消息回溯
   - 易於運維：docker-compose 一行啟動
   - 漸進式：Core NATS（簡單場景）→ JetStream（需持久化）

   權衡：
   - 生態較小：相比 Kafka/RabbitMQ 社區較小
   - 企業採用：新興技術，大企業案例較少

   適用場景：
   - 微服務異步通訊 ✅
   - 事件驅動架構 ✅
   - 任務隊列 ✅
   - 實時通訊 ✅
```

**選擇：方案 D（NATS + JetStream）**

**為何是最優解？**
1. **與專案一致**：Go 實現，代碼風格統一
2. **性能卓越**：滿足 100K+ msg/s 需求
3. **學習曲線平緩**：API 簡潔，適合教學
4. **功能完整**：涵蓋 MQ 核心需求
5. **運維簡單**：單一容器啟動

---

### 決策 2：如何保證消息不丟失？

```
問題：Consumer 接收消息後崩潰，如何避免消息丟失？

方案 A：Fire-and-Forget（Core NATS）
   機制：發送後立即返回，不保證送達

   問題：
   - Publisher 崩潰 → 消息丟失
   - Consumer 離線 → 消息丟失
   - 無持久化 → 重啟後消息全部丟失

   適用場景：即時通訊、日誌收集（可容忍丟失）

選擇方案 B：JetStream（At-least-once）
   機制：
   1. Publisher 發送 → JetStream 持久化（磁盤）
   2. JetStream 回覆 ACK → Publisher 確認送達
   3. Consumer 消費 → 手動 ACK
   4. 未 ACK 的消息 → 自動重試

   保證：
   - Publisher 崩潰 → 消息已持久化，重試發送
   - Consumer 崩潰 → 消息未 ACK，自動重新投遞
   - Server 重啟 → 磁盤恢復消息

   流程：
   T1: Publisher.Publish("order.created", msg)
   T2: JetStream 寫入磁盤 + 回覆 ACK
   T3: Consumer 接收消息
   T4: Consumer 處理業務邏輯
   T5: Consumer.Ack() ← 明確確認
   T6: JetStream 標記消息已消費

   重試機制（Consumer 未 ACK）：
   - 超時時間：30 秒（可配置）
   - 重試次數：無限次（直到 ACK）
   - 重試策略：指數退避

   權衡：
   - 可能重複消費 → Consumer 需冪等性設計
   - 性能略降 → 磁盤寫入開銷
```

**選擇：方案 B（JetStream At-least-once）**

**實現細節：**
```go
// Publisher 端：同步發送，等待 ACK
pubAck, err := js.Publish("order.created", orderData)
if err != nil {
    // 發送失敗，重試邏輯
    return err
}
// pubAck.Sequence：消息序號（用於追蹤）

// Consumer 端：手動 ACK
sub, _ := js.Subscribe("order.created", func(msg *nats.Msg) {
    // 1. 處理業務邏輯
    if err := processOrder(msg.Data); err != nil {
        // 2a. 處理失敗 → NAK，觸發重試
        msg.Nak()
        return
    }
    // 2b. 處理成功 → ACK
    msg.Ack()
})
```

---

### 決策 3：如何實現消費者組（負載均衡）？

```
問題：多個 Consumer 實例如何分擔負載？

方案 A：每個 Consumer 獨立訂閱
   機制：所有 Consumer 都收到相同消息

   問題：
   - 重複處理：N 個 Consumer → 消息處理 N 次
   - 浪費資源：所有實例做相同的事

   適用場景：廣播（如快取失效通知）

選擇方案 B：Queue Groups
   機制：
   - 多個 Consumer 加入同一個 Queue Group
   - JetStream 自動負載均衡（Round-Robin）
   - 每條消息只被一個 Consumer 處理

   範例：
   Queue Group "order-processor"
   ├─ Consumer 1 ──> 處理消息 1, 4, 7...
   ├─ Consumer 2 ──> 處理消息 2, 5, 8...
   └─ Consumer 3 ──> 處理消息 3, 6, 9...

   效果：
   - 3 個 Consumer → 吞吐量 3x
   - 自動容錯：Consumer 崩潰 → 消息重新分配

   權衡：
   - 順序性：無法保證全局順序（只能保證 Partition 內順序）
```

**選擇：方案 B（Queue Groups）**

**實現細節：**
```go
// Consumer 1
js.QueueSubscribe("order.created", "order-processor", handler)

// Consumer 2（同一個 Queue Group）
js.QueueSubscribe("order.created", "order-processor", handler)

// 結果：消息自動在兩個 Consumer 之間負載均衡
```

---

### 決策 4：消息如何持久化？

```
問題：如何平衡性能與持久化可靠性？

方案 A：記憶體存儲（Memory Storage）
   優勢：極致性能（微秒級延遲）
   問題：重啟後消息全部丟失

   適用場景：臨時數據、可容忍丟失

方案 B：檔案存儲（File Storage）
   優勢：持久化、重啟後恢復
   問題：性能略降（磁盤 I/O）

   適用場景：重要業務消息

選擇方案 C：分層存儲（教學簡化）
   機制：
   - 熱數據：記憶體（最近 1 小時）
   - 冷數據：磁盤（1 小時以上）

   優勢：平衡性能與成本

   教學簡化：
   - 當前實現：檔案存儲（簡單可靠）
   - 生產環境可優化：分層存儲、SSD 加速
```

**選擇：方案 B（檔案存儲）**

**JetStream 配置：**
```go
// 創建 Stream（定義消息存儲規則）
js.AddStream(&nats.StreamConfig{
    Name:     "ORDERS",
    Subjects: []string{"order.*"},
    Storage:  nats.FileStorage,  // 檔案持久化
    MaxAge:   7 * 24 * time.Hour, // 保留 7 天
    MaxBytes: 10 * 1024 * 1024 * 1024, // 10 GB 上限
})
```

---

### 決策 5：如何處理消息順序？

```
問題：訂單系統要求消息嚴格按順序處理

方案 A：全局順序（Single Consumer）
   機制：只用一個 Consumer，串行處理

   問題：
   - 無法擴展：吞吐量受限於單機
   - 效能低：無法並行處理

   計算：
   - 單 Consumer 處理速度：1,000 msg/s
   - 峰值需求：5,800 msg/s
   - 結果：無法滿足需求 ❌

選擇方案 B：分區順序（Partitioned）
   機制：
   - 相同 User ID 的消息 → 同一個 Partition
   - 不同 User 的消息 → 可並行處理

   範例：
   User 123 的消息：
   - order.created (seq=1) → Consumer A
   - order.paid (seq=2) → Consumer A
   - order.shipped (seq=3) → Consumer A
   → 保證順序 ✅

   User 456 的消息：
   - 可由 Consumer B 並行處理

   實現：
   - Subject 設計：order.{userID}.created
   - Consumer 綁定：每個 Consumer 訂閱特定用戶範圍

   效果：
   - 保證同一用戶的消息順序
   - 不同用戶可並行處理
   - 可水平擴展

   權衡：
   - 熱點問題：某些用戶消息過多 → 需一致性哈希分散
```

**選擇：方案 B（分區順序）**

**實現細節：**
```go
// Publisher 端：按 userID 路由
userID := order.UserID
subject := fmt.Sprintf("order.%d.created", userID % 10) // 10 個分區
js.Publish(subject, orderData)

// Consumer 端：訂閱特定分區
js.Subscribe("order.0.>", handler) // Consumer 1 處理分區 0
js.Subscribe("order.1.>", handler) // Consumer 2 處理分區 1
```

---

## 架構設計

### 整體架構

```
┌─────────────────────────────────────────────────────────────┐
│                        Application Layer                     │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐    │
│  │ Order    │  │ Payment  │  │ Inventory│  │ Notify   │    │
│  │ Service  │  │ Service  │  │ Service  │  │ Service  │    │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘    │
│       │ Publish     │ Publish     │ Subscribe   │ Subscribe │
└───────┼─────────────┼─────────────┼─────────────┼───────────┘
        │             │             │             │
        └─────────────┴─────────────┴─────────────┘
                          ↓
        ┌─────────────────────────────────────────┐
        │         NATS JetStream Server           │
        │  ┌───────────────────────────────────┐  │
        │  │  Stream: ORDERS                   │  │
        │  │  - Subjects: order.*              │  │
        │  │  - Storage: File (7 days)         │  │
        │  │  - Replication: 3x (生產環境)      │  │
        │  └───────────────────────────────────┘  │
        │  ┌───────────────────────────────────┐  │
        │  │  Stream: NOTIFICATIONS            │  │
        │  │  - Subjects: notify.*             │  │
        │  └───────────────────────────────────┘  │
        └─────────────────────────────────────────┘
                          ↓
        ┌─────────────────────────────────────────┐
        │       Persistent Storage (Disk)          │
        │  - WAL (Write-Ahead Log)                 │
        │  - Message Segments                      │
        │  - Consumer State                        │
        └─────────────────────────────────────────┘
```

### 消息流程

```
1. 發送消息（At-least-once）
   ┌─────────┐                ┌──────────┐
   │Publisher│                │JetStream │
   └────┬────┘                └────┬─────┘
        │ Publish(msg)             │
        │─────────────────────────>│
        │                          │ 1. 寫入 WAL
        │                          │ 2. 持久化磁盤
        │                          │ 3. 更新索引
        │      PubAck{Seq: 123}    │
        │<─────────────────────────│
        │                          │

2. 消費消息（Manual ACK）
   ┌─────────┐                ┌──────────┐
   │Consumer │                │JetStream │
   └────┬────┘                └────┬─────┘
        │ Subscribe("order.*")     │
        │─────────────────────────>│
        │                          │
        │      Msg{Seq: 123}       │
        │<─────────────────────────│
        │                          │
        │ Process(msg)             │
        │                          │
        │ Ack()                    │
        │─────────────────────────>│
        │                          │ 標記已消費
        │                          │

3. 重試機制（未 ACK）
   ┌─────────┐                ┌──────────┐
   │Consumer │                │JetStream │
   └────┬────┘                └────┬─────┘
        │      Msg{Seq: 124}       │
        │<─────────────────────────│
        │                          │
        │ Process(msg) → 失敗       │
        │ [未 ACK，30 秒超時]       │
        │                          │
        │      Msg{Seq: 124}       │ ← 自動重試
        │<─────────────────────────│
        │                          │
        │ Ack()                    │
        │─────────────────────────>│
```

---

## 擴展性分析

### 當前架構（10K msg/s）

**配置：**
- NATS Server: 1 個實例
- CPU: 2 核心
- 記憶體: 4 GB
- 磁盤: 100 GB SSD

**性能測試：**
```bash
# 發送測試
$ nats bench orders --msgs 1000000 --size 1024 --pub 10

Pub Stats: 102,345 msgs/sec ~ 100 MB/sec
```

**結論：** 單機 NATS 可處理 100K+ msg/s，當前需求 10K → 單機足夠 ✅

---

### 10x 擴展（100K msg/s）

**瓶頸分析：**
- NATS Server: 仍可處理（能力 100K+）
- 網絡帶寬：100K × 1KB = 100 MB/s（千兆網卡足夠）
- 磁盤 I/O：SSD 順序寫入 ~500 MB/s（足夠）

**方案 1：垂直擴展**
- CPU: 4 核心
- 記憶體: 8 GB
- 成本：$50/月（AWS c5.xlarge）
- 結論：**推薦** ✅

**方案 2：JetStream 叢集（3 節點）**
- 機制：Raft 共識，3 副本
- 優勢：高可用、自動容錯
- 成本：$150/月（3 × $50）
- 結論：若需高可用則採用

**方案 3：分層存儲**
- 機制：記憶體 + SSD + 冷存儲（S3）
- 優勢：降低成本
- 成本：$30/月（記憶體）+ $10/月（S3）
- 結論：消息量大時採用

**推薦：方案 1（垂直擴展）**

---

### 100x 擴展（1M msg/s）

**架構升級：**

```
                      ┌─── Load Balancer (HAProxy) ────┐
                      │                                  │
        ┌─────────────┴────────────┬───────────────────┴──────┐
        │                          │                           │
   ┌────▼────┐                ┌───▼─────┐              ┌─────▼──┐
   │ NATS    │◄──── Raft ────►│ NATS    │◄──── Raft ──►│ NATS   │
   │ Node 1  │                │ Node 2  │               │ Node 3 │
   │(Leader) │                │(Follower)│              │(Follower)│
   └────┬────┘                └────┬────┘              └────┬────┘
        │                          │                         │
        └──────────────┬───────────┴─────────────────────────┘
                       │
        ┌──────────────▼──────────────────┐
        │  Distributed Storage (Optional)  │
        │  - S3 (冷存儲)                    │
        │  - EBS (熱存儲)                   │
        └─────────────────────────────────┘
```

**核心變更：**

1. **JetStream 叢集（Raft）**
   - 3 個 NATS 節點
   - 自動 Leader 選舉
   - 消息複製 3 份

2. **Super Cluster（Leafnode）**
   - 地理分布：美國、歐洲、亞洲
   - 區域內高速通訊
   - 跨區域自動路由

3. **存儲優化**
   - 分層：Memory（1h）→ SSD（7d）→ S3（90d）
   - 壓縮：LZ4 壓縮（降低 50% 存儲）

**性能指標：**
- 吞吐量：1M+ msg/s（3 節點叢集）
- 延遲：P99 < 5ms（區域內）
- 可用性：99.99%（自動容錯）

**成本估算：**
- 3 × c5.2xlarge（8 核心、16 GB）：$450/月
- 1 TB SSD（EBS）：$100/月
- 10 TB S3 冷存儲：$230/月
- **總計：$780/月**

**對比 Kafka 方案：**
| 項目 | NATS 叢集 | Kafka 叢集 |
|------|-----------|-----------|
| 節點數 | 3 | 3 Kafka + 3 ZooKeeper |
| 運維複雜度 | 低 | 高 |
| 月成本 | $780 | $1,200+ |
| 延遲 | <5ms | ~50ms |
| 學習曲線 | 平緩 | 陡峭 |

---

## 實現範圍標註

### 已實現

| 功能 | 檔案 | 教學重點 |
|------|------|----------|
| **Core NATS Pub/Sub** | `internal/queue.go:20-45` | 基礎發布訂閱模式 |
| **JetStream 持久化** | `internal/queue.go:50-80` | At-least-once 語義 |
| **Queue Groups** | `internal/queue.go:85-110` | 負載均衡、消費者組 |
| **手動 ACK** | `internal/queue.go:115-140` | 消息確認、重試機制 |
| **Stream 管理** | `internal/queue.go:145-180` | 創建 Stream、配置持久化 |
| **消息順序** | `internal/queue.go:185-210` | 分區順序保證 |
| **HTTP API** | `internal/handler.go:15-100` | 發送消息、查詢狀態 |

### 教學簡化

| 功能 | 原因 | 生產環境建議 |
|------|------|-------------|
| **JetStream 叢集** | 聚焦單機概念 | 使用 3 節點 Raft 叢集，配置副本數 `Replicas: 3` |
| **Exactly-once** | 複雜度高 | 使用 JetStream 的冪等發送 + Consumer 去重表 |
| **消息路由** | 簡化為 Subject 匹配 | 可實現複雜路由規則（如 RabbitMQ 的 Topic Exchange） |
| **監控告警** | 非核心教學 | Prometheus + Grafana 監控 NATS 指標 |
| **死信隊列** | 篇幅限制 | 配置 `MaxDeliver` + 死信 Stream |
| **消息優先級** | NATS 不支持 | 若需要可考慮 RabbitMQ 或用多個 Stream 模擬 |

### 生產環境額外需要

1. **高可用性**
   - JetStream 叢集（3+ 節點）
   - 自動容錯、Leader 選舉
   - 跨可用區部署

2. **安全性**
   - TLS 加密（客戶端 ↔ Server）
   - 認證：JWT、NKey
   - 授權：Subject 層級權限控制

3. **監控與告警**
   - 指標：消息堆積、吞吐量、延遲
   - 工具：Prometheus Exporter
   - 告警：消息堆積 > 10K、延遲 > 100ms

4. **備份與恢復**
   - Stream 定期快照
   - 異地備份（S3）
   - 災難恢復演練

5. **性能優化**
   - 批量發送（降低網絡開銷）
   - 壓縮（LZ4）
   - 分層存儲（熱數據記憶體、冷數據 S3）

---

## 關鍵設計原則

### 1. 漸進式複雜度
- **起步簡單**：Core NATS（Pub/Sub）
- **按需增強**：需持久化 → JetStream
- **生產級**：需高可用 → 叢集

### 2. 至少一次送達
- **發送端**：等待 PubAck
- **接收端**：手動 ACK
- **重試**：自動重新投遞
- **代價**：Consumer 需冪等性

### 3. 水平擴展
- **Queue Groups**：自動負載均衡
- **分區**：相同鍵的消息保證順序
- **無狀態**：Consumer 可隨時增減

### 4. 關注點分離
- **JetStream**：負責可靠性、持久化
- **Application**：負責業務邏輯、冪等性
- **邊界清晰**：各司其職

---

## 延伸閱讀

### NATS 相關
- [NATS 官方文檔](https://docs.nats.io/)
- [JetStream 架構](https://docs.nats.io/nats-concepts/jetstream)
- [NATS vs Kafka 對比](https://nats.io/blog/nats-vs-kafka/)

### 系統設計問題
- **Phase 8: Task Scheduler** - 基於 MQ 的延遲任務
- **Phase 9: Event-Driven Architecture** - Event Sourcing + CQRS
- **Phase 21: Chat System** - 實時消息推送

### 設計模式
- **生產者-消費者模式**：解耦、異步處理
- **發布-訂閱模式**：一對多通訊
- **重試模式**：指數退避、熔斷

### 工業實現
- **Synadia Cloud**：NATS 官方雲服務
- **Confluent Cloud**：Kafka 雲服務（對比）
- **CloudAMQP**：RabbitMQ 雲服務（對比）

---

## 總結

### 核心思想
使用 **NATS JetStream** 構建輕量級、高性能的消息隊列系統，強調**漸進式複雜度**和**至少一次送達**保證。

### 適用場景
- ✅ 微服務異步通訊
- ✅ 任務隊列（郵件、報表）
- ✅ 事件驅動架構
- ✅ 削峰填谷

### 不適用場景
- ❌ 大數據日誌聚合（推薦 Kafka）
- ❌ 複雜路由需求（推薦 RabbitMQ）
- ❌ 消息優先級（NATS 不支持）

### 與其他 MQ 對比

| 特性 | NATS | Kafka | RabbitMQ | Redis |
|------|------|-------|----------|-------|
| **吞吐量** | 100K+ msg/s | 1M+ msg/s | 20K msg/s | 100K msg/s |
| **延遲** | <5ms | ~50ms | ~10ms | <1ms |
| **持久化** | ✅ JetStream | ✅ 磁盤日誌 | ✅ 可選 | △ AOF |
| **消息順序** | ✅ 分區 | ✅ Partition | △ 單 Queue | ❌ |
| **複雜路由** | △ Subject | ❌ | ✅ Exchange | ❌ |
| **學習曲線** | 平緩 | 陡峭 | 中等 | 平緩 |
| **運維複雜度** | 低 | 高 | 中 | 低 |
| **適用場景** | 微服務 MQ | 大數據管道 | 企業集成 | 簡單隊列 |

**結論：** NATS 在**輕量級、高性能、易用性**之間取得最佳平衡，是微服務消息隊列的最優解。
