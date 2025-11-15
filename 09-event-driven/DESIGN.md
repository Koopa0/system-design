# Event-Driven Architecture 系統設計文檔

## 問題定義

### 業務需求
構建事件驅動架構系統，實現：
- **Event Sourcing**：事件溯源，所有狀態變更記錄為事件
- **CQRS**：讀寫分離，優化查詢性能
- **Saga 模式**：分布式事務協調
- **事件重播**：從事件流重建系統狀態

### 技術指標
| 指標 | 目標值 | 挑戰 |
|------|--------|------|
| **事件吞吐** | 10K events/s | 如何高效寫入？ |
| **查詢延遲** | P99 < 50ms | 如何快速讀取？ |
| **一致性** | 最終一致 | 如何保證？ |
| **可追溯性** | 完整事件歷史 | 如何存儲？ |
| **可擴展** | 水平擴展 | 如何分區？ |

---

## 設計決策樹

### 決策 1：Event Store 如何實現？

```
需求：存儲所有事件，支持追加、重播

方案 A：關聯式資料庫（PostgreSQL）
   機制：events 表，append-only

   問題：
   - 寫入性能：每個事件一次 INSERT
   - 查詢效率：重播需全表掃描
   - 分區困難：難以水平擴展

方案 B：Kafka
   優勢：
   - 高吞吐：百萬級 events/s
   - 持久化：分區日誌
   - 重播：Consumer 可從任意位置讀取

   問題：
   - 重量級：需 ZooKeeper
   - 複雜度高：不適合教學

選擇方案 C：NATS JetStream
   機制：
   - Stream：持久化事件流
   - Consumer：支持從頭重播
   - Subject：事件路由

   優勢：
   - 輕量級：單一二進制
   - 完整功能：持久化、重播、分區
   - 整合簡單：承接 07/08 章節

   適用：
   - 中小規模事件流
   - 微服務事件驅動
   - 教學演示 ✅
```

**選擇：方案 C（NATS JetStream）**

---

### 決策 2：CQRS 如何實現讀寫分離？

```
問題：寫入優化（事件追加）vs 讀取優化（複雜查詢）

方案：CQRS（Command Query Responsibility Segregation）

架構：

Write Side（命令端）：
  Command → Aggregate → Event → Event Store
  ↓
  優化寫入性能

Read Side（查詢端）：
  Event Store → Event Handler → Read Model（如 PostgreSQL）
  ↓
  優化查詢性能

範例（訂單系統）：

Write Side:
  CreateOrderCommand
    → OrderAggregate.create()
    → OrderCreatedEvent
    → NATS JetStream（追加）

Read Side:
  訂閱 OrderCreatedEvent
    → 寫入 orders 表（PostgreSQL）
    → 支持複雜查詢（JOIN、聚合）

優勢：
  - 寫入：append-only，極快
  - 讀取：優化索引、物化視圖

權衡：
  - 最終一致性：讀寫有延遲（毫秒級）
  - 複雜度：維護兩個模型
```

---

### 決策 3：Saga 模式如何協調分布式事務？

```
場景：下訂單 = 創建訂單 + 扣庫存 + 扣款

方案 A：兩階段提交（2PC）
   問題：
   - 阻塞：Coordinator 崩潰導致鎖定
   - 性能差：多次網絡往返

選擇方案 B：Saga 模式（事件驅動）
   機制（Choreography，編排）：

   1. Order Service: OrderCreated event
   2. Inventory Service 訂閱 → ReserveInventory
      - 成功 → InventoryReserved event
      - 失敗 → InventoryFailed event
   3. Payment Service 訂閱 InventoryReserved
      - 成功 → PaymentCompleted event
      - 失敗 → PaymentFailed event → 補償（釋放庫存）

   補償流程（Compensating Transaction）：
   PaymentFailed
     → 發送 ReleaseInventory event
     → Inventory Service 釋放庫存
     → 發送 CancelOrder event
     → Order Service 取消訂單

   優勢：
   - 非阻塞：異步執行
   - 高可用：無單點
   - 可擴展：服務獨立

   權衡：
   - 最終一致性：非原子
   - 複雜度：需設計補償邏輯
```

**選擇：Saga Choreography（事件驅動編排）**

---

## 架構設計

### Event Sourcing 架構

```
┌──────────────────────────────────────────────────────────┐
│                      Command Side（寫）                    │
│                                                            │
│  Client → Command → Aggregate → Events → Event Store      │
│           (CreateOrder)  (業務邏輯)  (OrderCreated)  (NATS)│
└────────────────────────┬───────────────────────────────────┘
                         │
         ┌───────────────┴───────────────┐
         │   NATS JetStream Event Store  │
         │   - Stream: ORDERS_EVENTS     │
         │   - Append-only               │
         │   - 永久保存                   │
         └───────────────┬───────────────┘
                         │ Subscribe
┌────────────────────────┴───────────────────────────────────┐
│                      Query Side（讀）                       │
│                                                            │
│  Event Handler → Read Model → Query API                   │
│  (訂閱事件)      (PostgreSQL)   (REST/GraphQL)             │
│                  (優化查詢)                                 │
└──────────────────────────────────────────────────────────┘
```

### Saga 協調流程

```
Order Service     Inventory Service    Payment Service
     │                   │                   │
     │ 1. CreateOrder    │                   │
     ├──────────────────>│                   │
     │                   │ 2. ReserveInventory│
     │                   ├──────────────────>│
     │                   │                   │ 3. ChargePayment
     │                   │                   ├─────────┐
     │                   │                   │         │
     │                   │    4. PaymentCompleted      │
     │<──────────────────┴───────────────────┘         │
     │ 5. CompleteOrder                                │
     │                                                  │

補償流程（失敗情況）：

     │                   │                   │ 3. ChargePayment
     │                   │                   ├──X (失敗)
     │                   │    4. PaymentFailed
     │<──────────────────┴───────────────────┘
     │ 5. CancelOrder                        │
     ├──────────────────>│ 6. ReleaseInventory
     │                   ├─────────────────────────────>
```

---

## 關鍵概念詳解

### 1. Event Sourcing（事件溯源）

**傳統方式（State-based）：**
```sql
-- 只存儲最終狀態
UPDATE orders SET status = 'completed' WHERE id = 123
```

**Event Sourcing（Event-based）：**
```javascript
// 存儲所有事件
events = [
  {type: 'OrderCreated', data: {id: 123, amount: 99}},
  {type: 'OrderPaid', data: {id: 123}},
  {type: 'OrderShipped', data: {id: 123, tracking: 'ABC'}},
  {type: 'OrderCompleted', data: {id: 123}}
]

// 重建狀態
state = events.reduce((state, event) => apply(state, event), {})
```

**優勢：**
- 完整歷史：所有變更可追溯
- 審計友好：自然的審計日誌
- 時間旅行：可查詢任意時間點的狀態
- Debug 容易：重現問題

**權衡：**
- 存儲增長：事件持續累積
- 查詢複雜：需重放事件

---

### 2. CQRS（讀寫分離）

**單一模型問題：**
```
同一個模型既要優化寫入，又要優化查詢
→ 無法兩全其美
```

**CQRS 解決方案：**
```
Write Model（寫模型）：
- 優化：快速事件追加
- 結構：Aggregate（聚合根）
- 存儲：Event Store（append-only）

Read Model（讀模型）：
- 優化：複雜查詢、JOIN、聚合
- 結構：非規範化表（denormalized）
- 存儲：PostgreSQL + 索引
```

**最終一致性：**
```
T0: Write Model 寫入事件
T1: Event Handler 處理（延遲 10-50ms）
T2: Read Model 更新完成

查詢可能看到舊數據（10-50ms 延遲）
```

---

## 實現範圍標註

### 已實現

| 功能 | 檔案 | 教學重點 |
|------|------|----------|
| **Event Store** | `internal/eventstore.go:20-100` | NATS JetStream 事件存儲 |
| **Aggregate** | `internal/aggregate.go:15-80` | 聚合根、事件應用 |
| **CQRS** | `internal/projection.go:20-100` | 事件投影到讀模型 |
| **Saga** | `internal/saga.go:15-120` | 事件驅動的 Saga 編排 |
| **事件重播** | `internal/replay.go:10-50` | 從頭重建狀態 |

### 教學簡化

| 功能 | 原因 | 生產環境建議 |
|------|------|-------------|
| **快照** | 聚焦核心 | 定期快照減少重放開銷（如每 100 個事件） |
| **事件版本** | 避免複雜 | 實現事件 Upcasting（舊版本事件轉換） |
| **Saga Orchestrator** | 簡化為 Choreography | 複雜流程可用中央協調器 |

---

## 總結

### 核心思想
使用**事件驅動架構**實現系統解耦，通過 **Event Sourcing** 保證可追溯性，通過 **CQRS** 優化讀寫性能，通過 **Saga** 處理分布式事務。

### 適用場景
- ✅ 需要完整審計歷史
- ✅ 微服務事件驅動
- ✅ 分布式事務協調
- ✅ 複雜業務流程

### 不適用場景
- ❌ 簡單 CRUD 應用
- ❌ 強一致性要求（推薦 2PC）
- ❌ 事件量巨大（推薦 Kafka）

### 與其他方案對比

| 特性 | Event Sourcing + CQRS | 傳統 CRUD | Kafka Event Streaming |
|------|----------------------|-----------|----------------------|
| **可追溯性** | ✅ 完整事件歷史 | ❌ 只有最終狀態 | ✅ 完整日誌 |
| **查詢性能** | ✅ CQRS 優化 | ✅ 直接查詢 | △ 需構建視圖 |
| **一致性** | 最終一致 | 強一致 | 最終一致 |
| **複雜度** | 高 | 低 | 高 |
| **適用規模** | 中小規模 | 所有規模 | 大規模 |

**結論：** Event-Driven Architecture 在**可追溯性、解耦性、靈活性**方面具有優勢，適合微服務和複雜業務場景。
