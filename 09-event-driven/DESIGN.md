# Event-Driven Architecture 系統設計文檔

## 場景：你負責電商訂單系統

### 凌晨的緊急電話

凌晨 2 點，客服主管打電話給你：

> **客服：** "有客戶投訴訂單 #12345 被惡意取消！客戶說他沒有取消，但訂單狀態顯示已取消。我們需要調查是誰操作的！"

你打開資料庫查詢：

```sql
SELECT * FROM orders WHERE id = 12345;
```

結果：
```
id    | user_id | amount | status    | updated_at
12345 | 789     | 99.99  | cancelled | 2025-01-15 03:24:15
```

你陷入困境：
- 訂單現在是「已取消」狀態
- 但你不知道：
  * **誰取消的？**（客戶？管理員？系統？）
  * **什麼時候取消的？**（updated_at 只有最後更新時間）
  * **取消前是什麼狀態？**（已支付？已發貨？）
  * **為什麼取消？**（客戶申請？系統超時？）

**資料庫只有「現在」，沒有「歷史」。**

### 你會問自己：

1. **為什麼無法調查？**
   - 傳統資料庫只保存最終狀態
   - UPDATE 操作覆蓋了舊數據
   - 無法追溯變更歷史

2. **如何解決？**
   - 需要記錄所有變更
   - 不只存儲「狀態」，還要存儲「事件」

---

## 第一次嘗試：audit_log 表

### 設計思路

你加了審計表來記錄變更：

```sql
CREATE TABLE audit_log (
    id SERIAL PRIMARY KEY,
    table_name VARCHAR(50),
    record_id INT,
    operation VARCHAR(10),
    old_value JSONB,
    new_value JSONB,
    changed_by VARCHAR(50),
    changed_at TIMESTAMP
);
```

業務代碼：

```go
func CancelOrder(orderID int, userID string) error {
    // 1. 讀取舊數據
    oldOrder := db.GetOrder(orderID)

    // 2. 更新訂單
    db.Exec("UPDATE orders SET status = 'cancelled' WHERE id = ?", orderID)

    // 3. 記錄審計日誌
    db.Insert("audit_log", AuditLog{
        TableName: "orders",
        RecordID:  orderID,
        Operation: "UPDATE",
        OldValue:  oldOrder,
        NewValue:  {Status: "cancelled"},
        ChangedBy: userID,
        ChangedAt: time.Now(),
    })

    return nil
}
```

看起來問題解決了！

### 問題再現

一個月後，你發現問題：

```
調查訂單 #67890：
1. 查看 audit_log，找到 10 條變更記錄
2. 但第 5 條記錄缺失！（開發者忘記記錄）
3. 第 8 條記錄的 old_value 不對！（代碼bug）
4. 無法確定訂單真實的變更歷史
```

**問題根源：**
- 審計日誌是「手動」記錄的
- 開發者容易忘記
- 審計邏輯與業務邏輯分離
- 無法保證完整性

### 你意識到：

> "有沒有更「原生」的方式記錄歷史？讓歷史記錄成為系統的核心，而不是附加功能？"

---

## 靈感：銀行對帳單

你看著銀行 App 的對帳單，突然頓悟：

```
銀行對帳單：
2025-01-01  存款      +1,000  餘額: 1,000
2025-01-05  轉帳      -200    餘額: 800
2025-01-10  消費      -50     餘額: 750
2025-01-15  利息      +5      餘額: 755
```

**關鍵洞察：**
- 銀行不會直接改「餘額」
- 而是記錄每一筆「交易」（事件）
- **餘額 = 初始值 + 所有交易的累加**

這就是 **Event Sourcing**（事件溯源）！

---

## Event Sourcing：把思想用到訂單系統

### 對比

**傳統方式（State-based）：**
```sql
-- 只存儲最終狀態
UPDATE orders SET status = 'completed' WHERE id = 123;
```

無法知道：
- 訂單如何變成 completed 的？
- 中間經歷了哪些狀態？
- 每個狀態變更是誰觸發的？

**Event Sourcing（Event-based）：**
```javascript
// 存儲所有事件（不可變）
events = [
    {
        type: 'OrderCreated',
        data: {id: 123, user_id: 789, amount: 99.99},
        timestamp: '2025-01-15T10:00:00Z',
        actor: 'user:789'
    },
    {
        type: 'OrderPaid',
        data: {id: 123, payment_id: 'pay_456'},
        timestamp: '2025-01-15T10:05:00Z',
        actor: 'payment-gateway'
    },
    {
        type: 'OrderShipped',
        data: {id: 123, tracking: 'TRACK123'},
        timestamp: '2025-01-15T11:00:00Z',
        actor: 'admin:inventory-system'
    },
    {
        type: 'OrderCompleted',
        data: {id: 123},
        timestamp: '2025-01-15T12:00:00Z',
        actor: 'system:auto-complete'
    }
]

// 當前狀態 = 重放所有事件
currentState = events.reduce((state, event) => {
    return applyEvent(state, event);
}, initialState);
```

### 優勢

現在調查 #12345 訂單：

```javascript
// 查詢所有事件
events = eventStore.load('order-12345');

結果：
[
    {type: 'OrderCreated', actor: 'user:789', timestamp: T0},
    {type: 'OrderPaid', actor: 'payment-gateway', timestamp: T1},
    {type: 'OrderCancelled', actor: 'admin:support-001', timestamp: T2}
]

發現：
✅ 是管理員 support-001 取消的
✅ 取消時訂單已經支付
✅ 完整的時間線清晰可見
```

**5 分鐘內找到問題根源，而不是猜測！**

### Event Sourcing 核心思想

```
傳統方式：
- 存儲：最終狀態（snapshot）
- 更新：覆蓋舊數據（destructive）
- 歷史：需要額外維護（error-prone）

Event Sourcing：
- 存儲：所有事件（immutable）
- 更新：追加新事件（append-only）
- 歷史：天然保留（built-in）

類比：
- 傳統：只有最後一張照片
- Event Sourcing：完整的錄影帶
```

---

## 新問題：查詢性能

### Event Sourcing 的困境

產品經理抱怨：

> **PM：** "查詢訂單變慢了！原本 10ms，現在 500ms！"

你分析原因：

```
查詢訂單狀態：
1. 從 Event Store 讀取所有事件（100 個事件）
2. 依序重放事件重建狀態
3. 返回結果

時間消耗：
- 讀取事件：10ms
- 重放事件：100 × 4ms = 400ms
- 序列化返回：10ms
- 總計：420ms ❌
```

**問題：**
- 事件多時，重放很慢
- 每次查詢都要重放
- 無法做複雜查詢（如 JOIN、聚合）

### 你會問：

> "有沒有辦法既保留 Event Sourcing 的優勢（完整歷史），又解決查詢性能問題？"

---

## CQRS：讀寫分離

### 靈感

你意識到：

> "寫入時優化寫入（append-only events），查詢時優化查詢（indexed database），為什麼要用同一個模型？"

這就是 **CQRS**（Command Query Responsibility Segregation）！

### 架構

```
┌────────────────────────────┐
│    Write Side（命令端）      │
│                            │
│  Command                   │
│     ↓                      │
│  Aggregate (業務邏輯)       │
│     ↓                      │
│  Event                     │
│     ↓                      │
│  Event Store (NATS)        │
│  (優化：append-only)        │
└────────────┬───────────────┘
             │
             │ Subscribe (異步)
             ▼
┌────────────────────────────┐
│    Read Side（查詢端）       │
│                            │
│  Event Handler             │
│     ↓                      │
│  Read Model (PostgreSQL)   │
│  (優化：索引、JOIN、聚合)    │
│     ↓                      │
│  Query API                 │
└────────────────────────────┘
```

### 流程範例

**寫入（Command Side）：**
```go
// 1. 接收命令
cmd := CreateOrderCommand{
    UserID: 789,
    Amount: 99.99,
}

// 2. 執行業務邏輯（Aggregate）
order := NewOrderAggregate(123)
order.Create(cmd.UserID, cmd.Amount)  // 產生 OrderCreated 事件

// 3. 保存事件到 Event Store
eventStore.Append(OrderCreatedEvent{
    OrderID: 123,
    UserID:  789,
    Amount:  99.99,
})

// 寫入完成！（極快，< 5ms）
```

**讀取（Query Side）：**
```go
// 1. Event Handler 訂閱事件（異步）
eventStore.Subscribe("orders.*", func(event Event) {
    switch event.Type {
    case "OrderCreated":
        // 寫入 Read Model（PostgreSQL）
        db.Insert("orders", Order{
            ID:     event.OrderID,
            UserID: event.UserID,
            Amount: event.Amount,
            Status: "created",
        })
    case "OrderPaid":
        // 更新 Read Model
        db.Update("orders", event.OrderID, {Status: "paid"})
    }
})

// 2. 查詢從 Read Model 讀取
func GetOrder(orderID int) Order {
    return db.Query("SELECT * FROM orders WHERE id = ?", orderID)
    // 查詢極快！（< 10ms，有索引）
}
```

### 對比

| 特性 | Event Sourcing (純) | CQRS (讀寫分離) |
|------|-------------------|----------------|
| **寫入性能** | ✅ 極快（append-only） | ✅ 極快（同樣 append-only） |
| **查詢性能** | ❌ 慢（需重放事件） | ✅ 極快（優化的 Read Model） |
| **複雜查詢** | ❌ 困難（無 JOIN） | ✅ 簡單（PostgreSQL） |
| **歷史追溯** | ✅ 完整事件歷史 | ✅ 保留（Event Store） |
| **一致性** | ✅ 強一致 | ⚠️ 最終一致（10-50ms 延遲） |
| **複雜度** | ⚠️ 中等 | ❌ 高（維護兩個模型） |

### 權衡：最終一致性

```
時間線：
T0: Write Side 寫入 OrderPaid 事件
T0+10ms: Event Handler 收到事件
T0+15ms: Read Model 更新完成

問題：
- T0 到 T0+15ms 之間，查詢看到舊狀態（"created"）
- 實際已經支付，但 Read Model 還沒更新

是否可接受：
✅ 大部分業務場景（15ms 延遲可忽略）
❌ 金融交易（需要強一致性，應用 2PC）
```

---

## 新挑戰：分布式事務

### 場景

現在你有微服務架構：

```
下訂單流程：
1. Order Service：創建訂單
2. Inventory Service：扣庫存
3. Payment Service：扣款

問題：如何保證三個操作的一致性？
```

### 第一次嘗試：兩階段提交（2PC）

```
流程：
1. Prepare Phase：
   - Order Service: 準備創建訂單（鎖定資源）
   - Inventory Service: 準備扣庫存（鎖定資源）
   - Payment Service: 準備扣款（鎖定資源）

2. Commit Phase：
   - 所有服務都準備好 → 提交
   - 任一服務失敗 → 回滾

問題：
❌ 阻塞：Coordinator 崩潰導致資源鎖定
❌ 性能差：多次網絡往返
❌ 單點：Coordinator 是瓶頸
```

### Saga 模式：事件驅動協調

你意識到：

> "為什麼不用事件來協調？每個服務完成自己的工作，發布事件，下一個服務訂閱事件繼續！"

這就是 **Saga Choreography**（編排式 Saga）！

### 成功流程

```
事件鏈：
1. Order Service:
   - 執行：CreateOrder
   - 發布：OrderCreated event

2. Inventory Service（訂閱 OrderCreated）:
   - 執行：ReserveInventory
   - 發布：InventoryReserved event（成功）

3. Payment Service（訂閱 InventoryReserved）:
   - 執行：ChargePayment
   - 發布：PaymentCompleted event（成功）

4. Order Service（訂閱 PaymentCompleted）:
   - 執行：CompleteOrder
   - 發布：OrderCompleted event

結果：✅ 訂單成功完成
```

### 失敗流程（補償）

```
失敗場景：支付失敗

事件鏈：
1. Order Service:
   - 發布：OrderCreated event

2. Inventory Service:
   - 發布：InventoryReserved event ✅

3. Payment Service:
   - 執行：ChargePayment
   - 失敗：❌ 餘額不足
   - 發布：PaymentFailed event

4. Inventory Service（訂閱 PaymentFailed）:
   - 執行：ReleaseInventory（補償操作）
   - 發布：InventoryReleased event

5. Order Service（訂閱 PaymentFailed）:
   - 執行：CancelOrder（補償操作）
   - 發布：OrderCancelled event

結果：✅ 訂單已取消，庫存已釋放（最終一致）
```

### Saga vs 2PC

| 特性 | Saga（事件驅動） | 2PC（兩階段提交） |
|------|----------------|-----------------|
| **阻塞** | ✅ 非阻塞（異步） | ❌ 阻塞（等待所有服務） |
| **單點** | ✅ 無單點 | ❌ Coordinator 是單點 |
| **性能** | ✅ 高（異步） | ❌ 低（同步往返） |
| **一致性** | ⚠️ 最終一致 | ✅ 強一致 |
| **複雜度** | ❌ 高（需設計補償） | ⚠️ 中等 |
| **適用場景** | 微服務、高可用 | 單體、強一致性 |

---

## Event Store 選型

### 為什麼需要專門的 Event Store？

你嘗試用 PostgreSQL：

```sql
CREATE TABLE events (
    id SERIAL PRIMARY KEY,
    aggregate_id VARCHAR(50),
    type VARCHAR(50),
    data JSONB,
    timestamp TIMESTAMP
);

-- 查詢訂單的所有事件
SELECT * FROM events
WHERE aggregate_id = 'order-123'
ORDER BY timestamp;
```

**問題：**
- 寫入性能：每個事件一次 INSERT（~5ms）
- 查詢效率：重播需全表掃描（無法優化）
- 分區困難：難以水平擴展
- 事件順序：依賴 timestamp（可能不準確）

### 方案對比

| 方案 | 優勢 | 劣勢 | 適用場景 |
|------|------|------|---------|
| **PostgreSQL** | • 熟悉<br>• 事務支持 | • 性能差（~5K events/s）<br>• 難以擴展 | 小規模、教學 |
| **Kafka** | • 超高吞吐（100萬 events/s）<br>• 持久化<br>• 重播 | • 重量級（需 ZooKeeper）<br>• 運維複雜 | 大規模生產環境 |
| **NATS JetStream** | • 輕量級（單一二進制）<br>• 持久化<br>• 重播<br>• 高性能（10K events/s） | • 不如 Kafka 強大 | ✅ 中小規模、教學 |

**選擇：NATS JetStream**
- 承接 07/08 章節（已使用 NATS）
- 輕量級，適合教學
- 功能完整（持久化、重播、分區）

---

## 實現範圍標註

### 已實現（核心教學內容）

| 功能 | 檔案 | 教學重點 |
|------|------|----------|
| **Event Store** | `internal/eventstore.go` | NATS JetStream 事件存儲、訂閱、重播 |
| **Aggregate** | `internal/aggregate.go` | 聚合根、事件應用、業務邏輯 |
| **CQRS Projection** | `internal/projection.go` | 事件投影到 Read Model、查詢優化 |
| **Saga** | `internal/saga.go` | 事件驅動協調、補償事務 |
| **HTTP API** | `cmd/server/main.go` | 命令端（寫）、查詢端（讀） |

### 教學簡化（未實現）

| 功能 | 原因 | 生產環境建議 |
|------|------|-------------|
| **快照（Snapshot）** | 聚焦核心概念 | 每 100 個事件生成快照，加速重放 |
| **事件版本管理** | 避免複雜性 | 實現 Event Upcasting（舊事件轉換） |
| **Saga Orchestrator** | 簡化為 Choreography | 複雜流程用中央協調器（如 Temporal） |
| **冪等性保證** | 教學簡化 | 事件處理器需保證冪等（防止重複處理） |

---

## 你學到了什麼？

### 1. 從真實痛點出發

```
問題演進：
1. 無法調查訂單變更 → 需要審計日誌
2. 手動審計不可靠 → 需要 Event Sourcing
3. 查詢性能差 → 需要 CQRS
4. 分布式事務困難 → 需要 Saga

教訓：從實際問題出發，逐步改進
```

### 2. 借鑑其他領域

```
靈感來源：
- 銀行對帳單 → Event Sourcing
- 版本控制（Git）→ 不可變事件
- 讀寫分離（DB）→ CQRS

教訓：跨領域學習，尋找相似模式
```

### 3. 權衡無處不在

```
CQRS 權衡：
✅ 查詢性能提升
❌ 最終一致性（10-50ms 延遲）
❌ 複雜度增加（維護兩個模型）

Saga 權衡：
✅ 非阻塞、高可用
❌ 最終一致性
❌ 需設計補償邏輯

教訓：沒有完美方案，根據場景選擇
```

### 4. 工業界實踐

| 公司 | 使用場景 | 技術選型 |
|------|---------|---------|
| **Uber** | 訂單系統 | Event Sourcing + CQRS |
| **Netflix** | 支付系統 | Saga（Choreography） |
| **Amazon** | 庫存管理 | Event Sourcing（DynamoDB Streams） |
| **LinkedIn** | 活動流 | Kafka Event Streaming |

---

## 總結

Event-Driven Architecture 展示了**從狀態驅動到事件驅動的演進**：

1. **Event Sourcing**：存儲事件而非狀態，完整歷史可追溯
2. **CQRS**：讀寫分離，優化性能
3. **Saga**：事件驅動的分布式事務協調

**核心思想：** 用事件記錄「發生了什麼」，而不是「現在是什麼」。

**適用場景：**
- ✅ 需要完整審計歷史（金融、醫療）
- ✅ 微服務事件驅動
- ✅ 複雜業務流程
- ✅ 分布式事務協調

**不適用：**
- ❌ 簡單 CRUD 應用
- ❌ 強一致性要求（推薦 2PC）
- ❌ 極高吞吐量（推薦 Kafka）

**關鍵權衡：**
- 完整歷史 vs 存儲增長
- 查詢性能 vs 最終一致性
- 靈活性 vs 複雜度
