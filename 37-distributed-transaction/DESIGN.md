# 分佈式事務設計：從 ACID 到最終一致性

> 本文檔採用蘇格拉底式對話法（Socratic Method）呈現系統設計的思考過程

## Act 1: 分佈式事務的挑戰

**場景**：Emma 的電商平台需要處理訂單，涉及多個服務

**Emma**：「用戶下單時，我們需要：扣庫存、扣款、創建訂單。這三個操作如何保證要麼全成功，要麼全失敗？」

**David**：「這就是經典的分佈式事務問題。在單體架構中很簡單：」

```sql
BEGIN TRANSACTION;
  UPDATE inventory SET stock = stock - 1 WHERE product_id = 123;
  INSERT INTO payments (user_id, amount) VALUES (456, 999);
  INSERT INTO orders (user_id, product_id, amount) VALUES (456, 123, 999);
COMMIT;
```

**如果任何一步失敗，ROLLBACK 即可。**

**Sarah**：「但在微服務架構中：」

```
庫存服務（PostgreSQL A）
    ↓
支付服務（PostgreSQL B）
    ↓
訂單服務（PostgreSQL C）

三個獨立的數據庫，無法用單一事務！
```

**Michael**：「這就是分佈式事務的核心挑戰：**跨服務/跨數據庫的 ACID 保證**。」

### 為什麼困難？

**David**：「本地事務（單數據庫）vs 分佈式事務的差異：」

| 特性 | 本地事務 | 分佈式事務 |
|------|----------|------------|
| **原子性** | ✅ 數據庫保證 | ❌ 需要協調機制 |
| **一致性** | ✅ 約束檢查 | ❌ 跨服務難保證 |
| **隔離性** | ✅ 鎖機制 | ❌ 網絡分區問題 |
| **持久性** | ✅ WAL 日誌 | ❌ 部分成功/部分失敗 |
| **延遲** | < 1ms | 10-100ms |
| **失敗率** | 極低 | 網絡、服務故障 |

**Emma**：「那怎麼解決？」

**Sarah**：「有多種方案，各有 Trade-offs：」

1. **2PC（兩階段提交）** - 強一致性，但有阻塞問題
2. **Saga 模式** - 最終一致性，補償機制
3. **TCC（Try-Confirm-Cancel）** - 業務層面的 2PC
4. **Event Sourcing** - 事件驅動，狀態重建
5. **最終一致性** - 放棄強一致性，接受短暫不一致

**Michael**：「讓我們逐一深入探討。」

## Act 2: 兩階段提交（2PC）- 強一致性的代價

**David**：「2PC 是最直觀的方案，模擬數據庫事務。」

### 2PC 工作原理

```
協調者（Coordinator）
    ↓
參與者：庫存服務、支付服務、訂單服務

階段 1：準備階段（Prepare）
─────────────────────────
Coordinator → 參與者1: "準備扣庫存，能提交嗎？"
參與者1: 檢查庫存，鎖定資源 → "OK，準備好了"

Coordinator → 參與者2: "準備扣款，能提交嗎？"
參與者2: 檢查餘額，鎖定資源 → "OK，準備好了"

Coordinator → 參與者3: "準備創建訂單，能提交嗎？"
參與者3: 驗證數據，鎖定資源 → "OK，準備好了"

階段 2：提交階段（Commit）
─────────────────────────
如果所有參與者都 OK：
Coordinator → 所有參與者: "提交！"
參與者1, 2, 3: 執行提交，釋放鎖

如果任何一個參與者失敗：
Coordinator → 所有參與者: "中止！"
參與者1, 2, 3: 回滾，釋放鎖
```

### 實作範例

```go
package twopc

import (
    "context"
    "fmt"
    "time"
)

type Coordinator struct {
    participants []Participant
    timeout      time.Duration
}

type Participant interface {
    Prepare(ctx context.Context, txID string) error
    Commit(ctx context.Context, txID string) error
    Abort(ctx context.Context, txID string) error
}

func (c *Coordinator) ExecuteTransaction(ctx context.Context, txID string) error {
    // 階段 1：準備階段
    prepareCtx, cancel := context.WithTimeout(ctx, c.timeout)
    defer cancel()

    preparedParticipants := []Participant{}

    for _, p := range c.participants {
        err := p.Prepare(prepareCtx, txID)
        if err != nil {
            // 任何一個失敗，中止事務
            c.abortAll(ctx, txID, preparedParticipants)
            return fmt.Errorf("prepare failed: %w", err)
        }
        preparedParticipants = append(preparedParticipants, p)
    }

    // 所有參與者都準備好了
    // 階段 2：提交階段
    commitCtx, cancel := context.WithTimeout(ctx, c.timeout)
    defer cancel()

    for _, p := range c.participants {
        err := p.Commit(commitCtx, txID)
        if err != nil {
            // 理論上不應該失敗，但如果失敗了...
            // 這裡是 2PC 的問題：無法回滾已提交的
            return fmt.Errorf("commit failed: %w", err)
        }
    }

    return nil
}

func (c *Coordinator) abortAll(ctx context.Context, txID string, participants []Participant) {
    for _, p := range participants {
        p.Abort(ctx, txID)
    }
}

// 庫存服務參與者實作
type InventoryParticipant struct {
    db *sql.DB
}

func (p *InventoryParticipant) Prepare(ctx context.Context, txID string) error {
    // 開始本地事務
    tx, err := p.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }

    // 扣減庫存（加鎖）
    _, err = tx.ExecContext(ctx,
        "UPDATE inventory SET stock = stock - 1 WHERE product_id = ? AND stock > 0",
        productID,
    )
    if err != nil {
        tx.Rollback()
        return err
    }

    // 記錄準備狀態（持久化）
    _, err = tx.ExecContext(ctx,
        "INSERT INTO prepared_transactions (tx_id, status) VALUES (?, 'PREPARED')",
        txID,
    )
    if err != nil {
        tx.Rollback()
        return err
    }

    // 不提交，保持鎖定
    // 將 tx 存儲在內存中，等待 Commit/Abort
    storeTx(txID, tx)

    return nil
}

func (p *InventoryParticipant) Commit(ctx context.Context, txID string) error {
    tx := getTx(txID)
    if tx == nil {
        return fmt.Errorf("transaction not found")
    }

    // 提交本地事務
    err := tx.Commit()
    if err != nil {
        return err
    }

    // 更新狀態為已提交
    p.db.ExecContext(ctx,
        "UPDATE prepared_transactions SET status = 'COMMITTED' WHERE tx_id = ?",
        txID,
    )

    return nil
}

func (p *InventoryParticipant) Abort(ctx context.Context, txID string) error {
    tx := getTx(txID)
    if tx == nil {
        return nil
    }

    // 回滾本地事務
    tx.Rollback()

    // 更新狀態為已中止
    p.db.ExecContext(ctx,
        "UPDATE prepared_transactions SET status = 'ABORTED' WHERE tx_id = ?",
        txID,
    )

    return nil
}
```

### 2PC 的致命問題

**Sarah**：「2PC 有幾個嚴重問題：」

**問題 1：阻塞（Blocking）**
```
階段 1 完成後，參與者進入"準備好"狀態
    ↓
資源被鎖定（庫存、餘額）
    ↓
等待協調者發送 Commit 命令
    ↓
如果協調者崩潰？
    ↓
參與者永遠等待！資源永遠被鎖定！❌
```

**Emma**：「那不就死鎖了？」

**Michael**：「沒錯！這就是 2PC 的阻塞問題。」

**問題 2：單點故障**
```
協調者崩潰：
- 參與者不知道該提交還是中止
- 必須等待協調者恢復
- 期間資源被鎖定

參與者崩潰：
- 重啟後不知道事務狀態
- 需要查詢協調者（如果協調者也崩潰了？）
```

**問題 3：性能問題**
```
同步阻塞：
- 準備階段：等待所有參與者響應
- 提交階段：再次等待所有參與者

最慢的參與者決定整體延遲：
- 3 個服務，每個 10ms
- 總延遲：2 × 10ms = 20ms（理想情況）
- 實際：任何一個慢就會拖累所有

鎖定時間長：
- 從 Prepare 到 Commit，資源一直被鎖
- 降低並發度
```

**David**：「這就是為什麼 Google Spanner 花了巨大努力（TrueTime API、原子鐘）來實現分佈式事務。」

**Sarah**：「對於大部分公司，2PC 的代價太高。我們需要更實用的方案。」

## Act 3: Saga 模式 - 最終一致性與補償

**Michael**：「Saga 模式的核心思想：**放棄強一致性，接受最終一致性**。」

### Saga 工作原理

```
不再用兩階段提交，而是：

訂單流程分解為多個本地事務：
T1: 扣庫存
T2: 扣款
T3: 創建訂單

每個事務獨立執行、獨立提交
    ↓
如果後續事務失敗，執行補償事務（Compensation）

正常流程：
T1（扣庫存）→ T2（扣款）→ T3（創建訂單）✅

異常流程（T3 失敗）：
T1（扣庫存）✅ → T2（扣款）✅ → T3（創建訂單）❌
    ↓
C2（退款）← C1（加庫存）← 補償
```

### 兩種實作模式

**David**：「Saga 有兩種協調方式：」

#### 模式 1：編排（Choreography）- 事件驅動

```
無中央協調者，服務之間通過事件通訊

訂單服務 → 發送事件："訂單已創建"
    ↓
庫存服務 監聽事件 → 扣庫存 → 發送事件："庫存已扣減"
    ↓
支付服務 監聽事件 → 扣款 → 發送事件："支付已完成"
    ↓
訂單服務 監聽事件 → 更新訂單狀態為"已支付"

如果支付失敗：
支付服務 → 發送事件："支付失敗"
    ↓
庫存服務 監聽事件 → 恢復庫存 → 發送事件："庫存已恢復"
    ↓
訂單服務 監聽事件 → 更新訂單狀態為"已取消"
```

**實作：**

```go
package saga

import (
    "context"
    "encoding/json"
)

// 事件定義
type OrderCreated struct {
    OrderID   string
    ProductID string
    Quantity  int
    UserID    string
    Amount    float64
}

type InventoryReserved struct {
    OrderID   string
    ProductID string
    Quantity  int
}

type InventoryReservationFailed struct {
    OrderID string
    Reason  string
}

// 庫存服務
type InventoryService struct {
    eventBus EventBus
    db       *sql.DB
}

func (s *InventoryService) Start() {
    // 監聽"訂單已創建"事件
    s.eventBus.Subscribe("order.created", func(ctx context.Context, data []byte) error {
        var event OrderCreated
        json.Unmarshal(data, &event)

        // 嘗試扣庫存
        err := s.reserveInventory(ctx, event.ProductID, event.Quantity)
        if err != nil {
            // 失敗，發送失敗事件
            s.eventBus.Publish("inventory.reservation.failed", InventoryReservationFailed{
                OrderID: event.OrderID,
                Reason:  err.Error(),
            })
            return nil
        }

        // 成功，發送成功事件
        s.eventBus.Publish("inventory.reserved", InventoryReserved{
            OrderID:   event.OrderID,
            ProductID: event.ProductID,
            Quantity:  event.Quantity,
        })

        return nil
    })

    // 監聽"支付失敗"事件（補償）
    s.eventBus.Subscribe("payment.failed", func(ctx context.Context, data []byte) error {
        var event PaymentFailed
        json.Unmarshal(data, &event)

        // 恢復庫存（補償）
        s.releaseInventory(ctx, event.OrderID)

        // 發送庫存已釋放事件
        s.eventBus.Publish("inventory.released", InventoryReleased{
            OrderID: event.OrderID,
        })

        return nil
    })
}

func (s *InventoryService) reserveInventory(ctx context.Context, productID string, quantity int) error {
    result, err := s.db.ExecContext(ctx,
        "UPDATE inventory SET stock = stock - ? WHERE product_id = ? AND stock >= ?",
        quantity, productID, quantity,
    )
    if err != nil {
        return err
    }

    rows, _ := result.RowsAffected()
    if rows == 0 {
        return fmt.Errorf("insufficient stock")
    }

    return nil
}

func (s *InventoryService) releaseInventory(ctx context.Context, orderID string) error {
    // 查詢訂單扣減了多少庫存
    var productID string
    var quantity int
    s.db.QueryRowContext(ctx,
        "SELECT product_id, quantity FROM inventory_reservations WHERE order_id = ?",
        orderID,
    ).Scan(&productID, &quantity)

    // 恢復庫存
    _, err := s.db.ExecContext(ctx,
        "UPDATE inventory SET stock = stock + ? WHERE product_id = ?",
        quantity, productID,
    )

    return err
}
```

**優點：**
- ✅ 無單點故障（無中央協調者）
- ✅ 鬆耦合（服務間通過事件通訊）
- ✅ 易於擴展（新增服務只需訂閱事件）

**缺點：**
- ❌ 難以追蹤（事件在多個服務間流轉）
- ❌ 循環依賴風險
- ❌ 測試困難

#### 模式 2：編排（Orchestration）- 中央協調

```
有一個中央協調者（Saga Orchestrator）

Orchestrator：
Step 1 → 調用庫存服務.扣庫存()
         ✅ 成功 → 繼續
         ❌ 失敗 → 結束

Step 2 → 調用支付服務.扣款()
         ✅ 成功 → 繼續
         ❌ 失敗 → 執行補償：庫存服務.加庫存()

Step 3 → 調用訂單服務.創建訂單()
         ✅ 成功 → 完成
         ❌ 失敗 → 執行補償：支付服務.退款() + 庫存服務.加庫存()
```

**實作：**

```go
package saga

import (
    "context"
    "fmt"
)

// Saga 定義
type SagaDefinition struct {
    Name  string
    Steps []SagaStep
}

type SagaStep struct {
    Name        string
    Action      func(ctx context.Context, data map[string]interface{}) error
    Compensation func(ctx context.Context, data map[string]interface{}) error
}

// Saga 執行引擎
type SagaOrchestrator struct {
    definition SagaDefinition
}

func (o *SagaOrchestrator) Execute(ctx context.Context, initialData map[string]interface{}) error {
    executedSteps := []int{}
    data := initialData

    // 順序執行每個步驟
    for i, step := range o.definition.Steps {
        fmt.Printf("Executing step %d: %s\n", i, step.Name)

        err := step.Action(ctx, data)
        if err != nil {
            fmt.Printf("Step %d failed: %v\n", i, err)

            // 執行補償（逆序）
            o.compensate(ctx, executedSteps, data)

            return fmt.Errorf("saga failed at step %s: %w", step.Name, err)
        }

        executedSteps = append(executedSteps, i)
    }

    fmt.Println("Saga completed successfully")
    return nil
}

func (o *SagaOrchestrator) compensate(ctx context.Context, executedSteps []int, data map[string]interface{}) {
    // 逆序執行補償
    for i := len(executedSteps) - 1; i >= 0; i-- {
        stepIndex := executedSteps[i]
        step := o.definition.Steps[stepIndex]

        if step.Compensation != nil {
            fmt.Printf("Compensating step %d: %s\n", stepIndex, step.Name)
            err := step.Compensation(ctx, data)
            if err != nil {
                // 補償失敗！這是嚴重問題，需要告警
                fmt.Printf("CRITICAL: Compensation failed for step %s: %v\n", step.Name, err)
                // 記錄到數據庫，人工介入
            }
        }
    }
}

// 使用範例
func CreateOrderSaga(inventoryService, paymentService, orderService interface{}) SagaDefinition {
    return SagaDefinition{
        Name: "CreateOrder",
        Steps: []SagaStep{
            {
                Name: "ReserveInventory",
                Action: func(ctx context.Context, data map[string]interface{}) error {
                    productID := data["product_id"].(string)
                    quantity := data["quantity"].(int)

                    return inventoryService.Reserve(ctx, productID, quantity)
                },
                Compensation: func(ctx context.Context, data map[string]interface{}) error {
                    productID := data["product_id"].(string)
                    quantity := data["quantity"].(int)

                    return inventoryService.Release(ctx, productID, quantity)
                },
            },
            {
                Name: "ProcessPayment",
                Action: func(ctx context.Context, data map[string]interface{}) error {
                    userID := data["user_id"].(string)
                    amount := data["amount"].(float64)

                    paymentID, err := paymentService.Charge(ctx, userID, amount)
                    data["payment_id"] = paymentID
                    return err
                },
                Compensation: func(ctx context.Context, data map[string]interface{}) error {
                    paymentID := data["payment_id"].(string)

                    return paymentService.Refund(ctx, paymentID)
                },
            },
            {
                Name: "CreateOrder",
                Action: func(ctx context.Context, data map[string]interface{}) error {
                    orderID, err := orderService.Create(ctx, data)
                    data["order_id"] = orderID
                    return err
                },
                Compensation: func(ctx context.Context, data map[string]interface{}) error {
                    orderID := data["order_id"].(string)

                    return orderService.Cancel(ctx, orderID)
                },
            },
        },
    }
}

// 執行
orchestrator := SagaOrchestrator{
    definition: CreateOrderSaga(inventoryService, paymentService, orderService),
}

err := orchestrator.Execute(ctx, map[string]interface{}{
    "product_id": "prod-123",
    "quantity":   1,
    "user_id":    "user-456",
    "amount":     999.0,
})
```

**優點：**
- ✅ 易於追蹤（所有邏輯在一處）
- ✅ 易於測試
- ✅ 易於可視化工作流

**缺點：**
- ❌ 中央協調者是單點（需要高可用）
- ❌ 耦合性較高

**Emma**：「Saga 看起來更實用！但補償失敗怎麼辦？」

**Michael**：「這是 Saga 的一個難題，需要：」
1. **冪等性**：補償可以重複執行
2. **告警機制**：補償失敗立即告警
3. **人工介入**：最後的保障
4. **最終一致性**：通過定時任務掃描不一致狀態

## Act 4: TCC 模式 - 業務層面的兩階段提交

**David**：「TCC 是 2PC 的業務層變體，將資源鎖定交給業務邏輯。」

### TCC 三個階段

```
Try（嘗試）：
- 預留資源，不直接扣減
- 例如：凍結庫存、凍結餘額

Confirm（確認）：
- 確認使用資源
- 例如：實際扣減庫存、實際扣款

Cancel（取消）：
- 釋放預留的資源
- 例如：解凍庫存、解凍餘額
```

### TCC 實作

```go
package tcc

import (
    "context"
    "time"
)

// TCC 參與者接口
type TCCParticipant interface {
    Try(ctx context.Context, txID string, params map[string]interface{}) error
    Confirm(ctx context.Context, txID string) error
    Cancel(ctx context.Context, txID string) error
}

// 庫存服務 TCC 實作
type InventoryTCC struct {
    db *sql.DB
}

func (t *InventoryTCC) Try(ctx context.Context, txID string, params map[string]interface{}) error {
    productID := params["product_id"].(string)
    quantity := params["quantity"].(int)

    tx, _ := t.db.BeginTx(ctx, nil)
    defer tx.Rollback()

    // 1. 檢查庫存
    var stock int
    err := tx.QueryRowContext(ctx,
        "SELECT stock FROM inventory WHERE product_id = ? FOR UPDATE",
        productID,
    ).Scan(&stock)

    if stock < quantity {
        return fmt.Errorf("insufficient stock")
    }

    // 2. 凍結庫存（不扣減 stock，記錄到凍結表）
    _, err = tx.ExecContext(ctx,
        "INSERT INTO inventory_frozen (tx_id, product_id, quantity, frozen_at) VALUES (?, ?, ?, ?)",
        txID, productID, quantity, time.Now(),
    )
    if err != nil {
        return err
    }

    tx.Commit()
    return nil
}

func (t *InventoryTCC) Confirm(ctx context.Context, txID string) error {
    tx, _ := t.db.BeginTx(ctx, nil)
    defer tx.Rollback()

    // 1. 查詢凍結記錄
    var productID string
    var quantity int
    err := tx.QueryRowContext(ctx,
        "SELECT product_id, quantity FROM inventory_frozen WHERE tx_id = ?",
        txID,
    ).Scan(&productID, &quantity)

    if err != nil {
        return err
    }

    // 2. 實際扣減庫存
    _, err = tx.ExecContext(ctx,
        "UPDATE inventory SET stock = stock - ? WHERE product_id = ?",
        quantity, productID,
    )
    if err != nil {
        return err
    }

    // 3. 刪除凍結記錄
    _, err = tx.ExecContext(ctx,
        "DELETE FROM inventory_frozen WHERE tx_id = ?",
        txID,
    )
    if err != nil {
        return err
    }

    tx.Commit()
    return nil
}

func (t *InventoryTCC) Cancel(ctx context.Context, txID string) error {
    // 直接刪除凍結記錄（釋放資源）
    _, err := t.db.ExecContext(ctx,
        "DELETE FROM inventory_frozen WHERE tx_id = ?",
        txID,
    )

    return err
}

// 支付服務 TCC 實作
type PaymentTCC struct {
    db *sql.DB
}

func (t *PaymentTCC) Try(ctx context.Context, txID string, params map[string]interface{}) error {
    userID := params["user_id"].(string)
    amount := params["amount"].(float64)

    tx, _ := t.db.BeginTx(ctx, nil)
    defer tx.Rollback()

    // 1. 檢查餘額
    var balance float64
    err := tx.QueryRowContext(ctx,
        "SELECT balance FROM accounts WHERE user_id = ? FOR UPDATE",
        userID,
    ).Scan(&balance)

    if balance < amount {
        return fmt.Errorf("insufficient balance")
    }

    // 2. 凍結金額
    _, err = tx.ExecContext(ctx,
        "INSERT INTO balance_frozen (tx_id, user_id, amount, frozen_at) VALUES (?, ?, ?, ?)",
        txID, userID, amount, time.Now(),
    )
    if err != nil {
        return err
    }

    tx.Commit()
    return nil
}

func (t *PaymentTCC) Confirm(ctx context.Context, txID string) error {
    tx, _ := t.db.BeginTx(ctx, nil)
    defer tx.Rollback()

    // 1. 查詢凍結記錄
    var userID string
    var amount float64
    err := tx.QueryRowContext(ctx,
        "SELECT user_id, amount FROM balance_frozen WHERE tx_id = ?",
        txID,
    ).Scan(&userID, &amount)

    if err != nil {
        return err
    }

    // 2. 實際扣款
    _, err = tx.ExecContext(ctx,
        "UPDATE accounts SET balance = balance - ? WHERE user_id = ?",
        amount, userID,
    )
    if err != nil {
        return err
    }

    // 3. 刪除凍結記錄
    _, err = tx.ExecContext(ctx,
        "DELETE FROM balance_frozen WHERE tx_id = ?",
        txID,
    )
    if err != nil {
        return err
    }

    tx.Commit()
    return nil
}

func (t *PaymentTCC) Cancel(ctx context.Context, txID string) error {
    // 刪除凍結記錄
    _, err := t.db.ExecContext(ctx,
        "DELETE FROM balance_frozen WHERE tx_id = ?",
        txID,
    )

    return err
}

// TCC 協調器
type TCCCoordinator struct {
    participants []TCCParticipant
}

func (c *TCCCoordinator) Execute(ctx context.Context, txID string, params map[string]interface{}) error {
    // 階段 1：Try
    triedParticipants := []TCCParticipant{}

    for _, p := range c.participants {
        err := p.Try(ctx, txID, params)
        if err != nil {
            // Try 失敗，Cancel 所有已 Try 的
            c.cancelAll(ctx, txID, triedParticipants)
            return fmt.Errorf("try failed: %w", err)
        }
        triedParticipants = append(triedParticipants, p)
    }

    // 階段 2：Confirm
    for _, p := range c.participants {
        err := p.Confirm(ctx, txID)
        if err != nil {
            // Confirm 失敗！這是嚴重問題
            // 需要重試或告警
            return fmt.Errorf("confirm failed: %w", err)
        }
    }

    return nil
}

func (c *TCCCoordinator) cancelAll(ctx context.Context, txID string, participants []TCCParticipant) {
    for _, p := range participants {
        p.Cancel(ctx, txID)
    }
}
```

### TCC vs 2PC

**Sarah**：「TCC 和 2PC 很像，有什麼區別？」

| 特性 | 2PC | TCC |
|------|-----|-----|
| **鎖定層級** | 數據庫鎖 | 業務邏輯鎖 |
| **性能** | 差（長時間鎖） | 好（業務控制） |
| **實作複雜度** | 簡單 | 複雜（需業務層設計） |
| **適用場景** | 數據庫支援 | 跨系統、跨DB |

**Michael**：「TCC 的優勢在於：**業務層面的資源鎖定更靈活**。」

```
2PC：
鎖定整個行 → 其他事務無法讀寫

TCC：
僅凍結特定數量 → 其他事務仍可操作剩餘部分

例如：
庫存 100 件
事務 A 凍結 10 件 → 剩餘 90 件仍可被其他事務凍結
```

**Emma**：「但 TCC 需要業務層實作 Try/Confirm/Cancel，開發成本高。」

**David**：「沒錯。選擇 TCC 還是 Saga，取決於業務需求：」

```
需要預留資源（庫存、座位、票）→ TCC
不需要預留，可以補償 → Saga
```

## Act 5: Event Sourcing - 事件溯源

**Sarah**：「還有一種完全不同的思路：**不存儲狀態，只存儲事件**。」

### Event Sourcing 核心思想

```
傳統方式（存儲狀態）：
orders 表：
order_id | status    | amount
123      | PAID      | 999
456      | CANCELLED | 500

Event Sourcing（存儲事件）：
events 表：
event_id | aggregate_id | event_type      | data              | timestamp
1        | order-123    | OrderCreated    | {amount: 999}     | 10:00:00
2        | order-123    | PaymentReceived | {payment_id: xx}  | 10:00:05
3        | order-123    | OrderPaid       | {}                | 10:00:06
4        | order-456    | OrderCreated    | {amount: 500}     | 10:01:00
5        | order-456    | OrderCancelled  | {reason: "..."}   | 10:02:00

當前狀態 = 重放所有事件
```

### 實作範例

```go
package eventsourcing

import (
    "context"
    "encoding/json"
    "time"
)

// 事件定義
type Event struct {
    EventID      string
    AggregateID  string
    EventType    string
    Data         json.RawMessage
    Timestamp    time.Time
    Version      int
}

// 訂單聚合根
type Order struct {
    ID          string
    Status      string
    Amount      float64
    ProductID   string
    Quantity    int
    Version     int
    UncommittedEvents []Event
}

// 事件處理（應用事件到聚合根）
func (o *Order) Apply(event Event) {
    switch event.EventType {
    case "OrderCreated":
        var data struct {
            ProductID string
            Quantity  int
            Amount    float64
        }
        json.Unmarshal(event.Data, &data)

        o.ID = event.AggregateID
        o.Status = "CREATED"
        o.ProductID = data.ProductID
        o.Quantity = data.Quantity
        o.Amount = data.Amount

    case "PaymentReceived":
        o.Status = "PAYMENT_RECEIVED"

    case "OrderPaid":
        o.Status = "PAID"

    case "OrderCancelled":
        o.Status = "CANCELLED"
    }

    o.Version = event.Version
}

// 業務邏輯（產生事件）
func (o *Order) CreateOrder(productID string, quantity int, amount float64) {
    data, _ := json.Marshal(map[string]interface{}{
        "product_id": productID,
        "quantity":   quantity,
        "amount":     amount,
    })

    event := Event{
        EventID:     generateUUID(),
        AggregateID: o.ID,
        EventType:   "OrderCreated",
        Data:        data,
        Timestamp:   time.Now(),
        Version:     o.Version + 1,
    }

    o.UncommittedEvents = append(o.UncommittedEvents, event)
    o.Apply(event)
}

func (o *Order) ReceivePayment(paymentID string) error {
    if o.Status != "CREATED" {
        return fmt.Errorf("invalid state: %s", o.Status)
    }

    data, _ := json.Marshal(map[string]interface{}{
        "payment_id": paymentID,
    })

    event := Event{
        EventID:     generateUUID(),
        AggregateID: o.ID,
        EventType:   "PaymentReceived",
        Data:        data,
        Timestamp:   time.Now(),
        Version:     o.Version + 1,
    }

    o.UncommittedEvents = append(o.UncommittedEvents, event)
    o.Apply(event)

    return nil
}

func (o *Order) MarkAsPaid() error {
    if o.Status != "PAYMENT_RECEIVED" {
        return fmt.Errorf("invalid state: %s", o.Status)
    }

    event := Event{
        EventID:     generateUUID(),
        AggregateID: o.ID,
        EventType:   "OrderPaid",
        Data:        []byte("{}"),
        Timestamp:   time.Now(),
        Version:     o.Version + 1,
    }

    o.UncommittedEvents = append(o.UncommittedEvents, event)
    o.Apply(event)

    return nil
}

// Event Store（事件存儲）
type EventStore struct {
    db *sql.DB
}

func (es *EventStore) Save(ctx context.Context, events []Event) error {
    tx, _ := es.db.BeginTx(ctx, nil)
    defer tx.Rollback()

    for _, event := range events {
        _, err := tx.ExecContext(ctx,
            `INSERT INTO events (event_id, aggregate_id, event_type, data, timestamp, version)
             VALUES (?, ?, ?, ?, ?, ?)`,
            event.EventID,
            event.AggregateID,
            event.EventType,
            event.Data,
            event.Timestamp,
            event.Version,
        )
        if err != nil {
            return err
        }
    }

    return tx.Commit()
}

func (es *EventStore) Load(ctx context.Context, aggregateID string) ([]Event, error) {
    rows, err := es.db.QueryContext(ctx,
        "SELECT event_id, aggregate_id, event_type, data, timestamp, version FROM events WHERE aggregate_id = ? ORDER BY version",
        aggregateID,
    )
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    events := []Event{}
    for rows.Next() {
        var event Event
        rows.Scan(
            &event.EventID,
            &event.AggregateID,
            &event.EventType,
            &event.Data,
            &event.Timestamp,
            &event.Version,
        )
        events = append(events, event)
    }

    return events, nil
}

// 重建聚合根（從事件重放）
func (es *EventStore) Reconstruct(ctx context.Context, aggregateID string) (*Order, error) {
    events, err := es.Load(ctx, aggregateID)
    if err != nil {
        return nil, err
    }

    order := &Order{ID: aggregateID}
    for _, event := range events {
        order.Apply(event)
    }

    return order, nil
}

// 使用範例
func ExampleUsage() {
    ctx := context.Background()
    eventStore := &EventStore{db: db}

    // 創建訂單
    order := &Order{ID: "order-123"}
    order.CreateOrder("prod-456", 1, 999.0)

    // 保存事件
    eventStore.Save(ctx, order.UncommittedEvents)

    // 後續操作
    order, _ = eventStore.Reconstruct(ctx, "order-123")
    order.ReceivePayment("payment-789")
    eventStore.Save(ctx, order.UncommittedEvents)

    order, _ = eventStore.Reconstruct(ctx, "order-123")
    order.MarkAsPaid()
    eventStore.Save(ctx, order.UncommittedEvents)

    // 查詢當前狀態
    finalOrder, _ := eventStore.Reconstruct(ctx, "order-123")
    fmt.Printf("Order status: %s\n", finalOrder.Status) // "PAID"
}
```

### Event Sourcing 優勢

**Michael**：「Event Sourcing 的強大之處：」

**1. 完整審計日誌**
```
可以回答：
- 訂單什麼時候變成 CANCELLED 的？
- 誰取消的？
- 為什麼取消？

所有歷史都保存在事件中！
```

**2. 時間旅行**
```go
// 查詢訂單在某個時間點的狀態
func (es *EventStore) ReconstructAt(ctx context.Context, aggregateID string, timestamp time.Time) (*Order, error) {
    events, _ := es.Load(ctx, aggregateID)

    order := &Order{ID: aggregateID}
    for _, event := range events {
        if event.Timestamp.After(timestamp) {
            break
        }
        order.Apply(event)
    }

    return order, nil
}

// 2025-01-15 10:00:00 時訂單狀態是什麼？
order, _ := eventStore.ReconstructAt(ctx, "order-123", time.Parse("2025-01-15 10:00:00"))
```

**3. 事件重放（Debug 利器）**
```
生產環境出 Bug：
1. 導出事件日誌
2. 在測試環境重放
3. 完全重現問題！
```

**4. CQRS（讀寫分離）**
```
寫入：
存儲事件到 Event Store

讀取：
從事件構建不同的讀模型（Read Model）

例如：
- 訂單詳情視圖
- 用戶訂單列表視圖
- 統計報表視圖

每個視圖獨立優化！
```

### Event Sourcing 挑戰

**Emma**：「聽起來很完美，有什麼缺點嗎？」

**Sarah**：「當然有：」

**1. 事件版本管理**
```
V1: OrderCreated 事件
{
  "product_id": "prod-123",
  "amount": 999
}

V2: OrderCreated 事件（新增字段）
{
  "product_id": "prod-123",
  "amount": 999,
  "discount": 50  ← 新字段
}

如何處理舊事件？需要事件升級策略！
```

**2. 性能問題**
```
訂單經過 1000 個事件：
每次查詢都要重放 1000 個事件？

解決方案：快照（Snapshot）
每 100 個事件保存一次快照
查詢時：從最近快照開始重放
```

```go
type Snapshot struct {
    AggregateID string
    Data        json.RawMessage
    Version     int
    Timestamp   time.Time
}

func (es *EventStore) LoadWithSnapshot(ctx context.Context, aggregateID string) (*Order, error) {
    // 1. 載入最近快照
    snapshot, err := es.loadSnapshot(ctx, aggregateID)

    var order *Order
    var startVersion int

    if snapshot != nil {
        json.Unmarshal(snapshot.Data, &order)
        startVersion = snapshot.Version + 1
    } else {
        order = &Order{ID: aggregateID}
        startVersion = 0
    }

    // 2. 只重放快照之後的事件
    events, _ := es.loadEventsAfterVersion(ctx, aggregateID, startVersion)
    for _, event := range events {
        order.Apply(event)
    }

    return order, nil
}
```

**3. 刪除困難**
```
GDPR 要求：用戶有權刪除個人數據

但 Event Sourcing 不刪除事件！

解決方案：
- 加密個人信息，刪除密鑰
- 添加 "PersonalDataDeleted" 事件
- 遺忘事件（Forgotten Event）
```

## Act 6: 最終一致性 - 現實的妥協

**David**：「分佈式系統的現實：**強一致性代價太高，最終一致性是妥協**。」

### BASE vs ACID

```
ACID（強一致性）：
- Atomicity: 原子性
- Consistency: 一致性
- Isolation: 隔離性
- Durability: 持久性

BASE（最終一致性）：
- Basically Available: 基本可用
- Soft state: 軟狀態（允許短暫不一致）
- Eventually consistent: 最終一致
```

### 實踐範例

**場景：訂單創建**

```
強一致性（不現實）：
創建訂單 → 等待庫存扣減 → 等待支付完成 → 返回成功
用戶等待時間：500ms + 300ms + 200ms = 1000ms

最終一致性（現實）：
創建訂單 → 立即返回（訂單狀態: PENDING）→ 200ms
後台異步：
- 扣庫存（300ms 後完成）
- 扣款（500ms 後完成）
- 更新訂單狀態為 PAID

用戶體驗：立即收到"訂單已創建"，幾秒後收到"支付成功"通知
```

### 實作模式

**模式 1：定時任務掃描**

```go
// 掃描未完成的訂單
func ReconcileOrders() {
    // 查詢 30 分鐘前創建但仍是 PENDING 的訂單
    orders := db.Query(`
        SELECT id FROM orders
        WHERE status = 'PENDING'
        AND created_at < NOW() - INTERVAL 30 MINUTE
    `)

    for _, order := range orders {
        // 檢查庫存是否扣減
        inventoryReserved := inventoryService.Check(order.ID)
        if !inventoryReserved {
            // 重試扣庫存
            inventoryService.Reserve(order.ProductID, order.Quantity)
        }

        // 檢查支付是否完成
        paymentCompleted := paymentService.Check(order.ID)
        if !paymentCompleted {
            // 重試支付
            paymentService.Charge(order.UserID, order.Amount)
        }

        // 如果都完成，更新訂單狀態
        if inventoryReserved && paymentCompleted {
            db.Exec("UPDATE orders SET status = 'PAID' WHERE id = ?", order.ID)
        }
    }
}

// 每分鐘執行一次
cron.Schedule("* * * * *", ReconcileOrders)
```

**模式 2：重試機制**

```go
func ProcessOrderAsync(orderID string) {
    maxRetries := 3
    retryDelay := time.Second

    for attempt := 0; attempt < maxRetries; attempt++ {
        err := processOrder(orderID)
        if err == nil {
            return // 成功
        }

        // 失敗，重試
        log.Printf("Attempt %d failed: %v", attempt+1, err)
        time.Sleep(retryDelay)
        retryDelay *= 2 // 指數退避
    }

    // 所有重試都失敗，記錄到死信隊列
    deadLetterQueue.Publish(orderID)
}
```

**模式 3：冪等性保證**

```go
// 所有操作必須冪等（可重複執行）
func ReserveInventory(orderID string, productID string, quantity int) error {
    // 檢查是否已預留
    exists := db.QueryRow(
        "SELECT 1 FROM inventory_reservations WHERE order_id = ?",
        orderID,
    )

    if exists {
        return nil // 已預留，直接返回成功
    }

    // 扣減庫存
    result := db.Exec(
        "UPDATE inventory SET stock = stock - ? WHERE product_id = ? AND stock >= ?",
        quantity, productID, quantity,
    )

    if result.RowsAffected == 0 {
        return fmt.Errorf("insufficient stock")
    }

    // 記錄預留
    db.Exec(
        "INSERT INTO inventory_reservations (order_id, product_id, quantity) VALUES (?, ?, ?)",
        orderID, productID, quantity,
    )

    return nil
}
```

**Sarah**：「最終一致性的關鍵：**可觀測性和補償機制**。」

```
監控指標：
- 待處理訂單數量
- 平均處理時間
- 失敗率

告警：
- 待處理訂單 > 1000 → 告警
- 處理時間 > 5 分鐘 → 告警
- 失敗率 > 1% → 告警

補償：
- 定時掃描
- 手動介入工具
- 死信隊列處理
```

## Act 7: 選擇合適的方案

**Emma**：「這麼多方案，該選哪個？」

**Michael**：「沒有銀彈，根據業務需求選擇：」

### 決策樹

```
需要強一致性？
├─ Yes → 單體架構（本地事務）
│         或
│         2PC（接受性能代價）
│
└─ No → 可接受最終一致性
         ├─ 需要預留資源？
         │  └─ Yes → TCC
         │           例如：酒店預訂、票務
         │
         └─ No → Saga
                 ├─ 複雜工作流？
                 │  └─ Yes → Saga Orchestration
                 │
                 └─ No → Saga Choreography
                         或
                         Event Sourcing（需要審計）
```

### 方案對比

| 方案 | 一致性 | 性能 | 複雜度 | 適用場景 |
|------|--------|------|--------|----------|
| **本地事務** | 強 | ⚡⚡⚡ | 低 | 單體應用 |
| **2PC** | 強 | ⚡ | 中 | 數據庫支援、低並發 |
| **TCC** | 強 | ⚡⚡ | 高 | 需預留資源 |
| **Saga** | 最終 | ⚡⚡⚡ | 中 | 微服務、高並發 |
| **Event Sourcing** | 最終 | ⚡⚡ | 高 | 需審計、複雜領域 |

### 實戰建議

**David**：「我的建議：」

**1. 優先考慮業務拆分**
```
能不能避免分佈式事務？
- 訂單服務自己管理訂單、庫存、支付？（單體）
- 用 Database per Service 但避免跨服務事務？
```

**2. 從簡單開始**
```
第一版：單體 + 本地事務
    ↓
業務增長：拆分服務 + 消息隊列 + 最終一致性
    ↓
特殊需求：引入 Saga/TCC
```

**3. 冪等性是基礎**
```
無論用哪種方案，所有操作都要冪等！
- 使用唯一 ID（訂單號、事務 ID）
- 檢查是否已執行
- 重複執行結果相同
```

**4. 監控和補償**
```
分佈式事務一定會失敗！
- 完善的監控
- 自動重試機制
- 定時對帳掃描
- 人工介入工具
```

**Emma**：「明白了！分佈式事務沒有完美方案，只有最合適的 Trade-off。」

**Sarah**：「沒錯！關鍵是理解業務需求，選擇合適的一致性模型。」

---

## 總結

**Michael**：「分佈式事務的核心挑戰：**在分佈式環境中保證 ACID**。」

| 方案 | 原理 | 優勢 | 劣勢 | 推薦場景 |
|------|------|------|------|----------|
| **2PC** | 兩階段提交 | 強一致性 | 阻塞、單點故障 | 低並發、數據庫支援 |
| **Saga** | 補償機制 | 高性能、鬆耦合 | 最終一致、補償複雜 | 微服務、高並發 |
| **TCC** | Try-Confirm-Cancel | 業務層鎖定 | 開發複雜 | 需預留資源 |
| **Event Sourcing** | 事件溯源 | 完整審計、時間旅行 | 版本管理、性能 | 需審計、複雜領域 |

**透過本章學習，你掌握了：**

1. ✅ **2PC**：兩階段提交的原理與問題
2. ✅ **Saga**：Choreography vs Orchestration
3. ✅ **TCC**：業務層的分佈式事務
4. ✅ **Event Sourcing**：事件驅動的狀態管理
5. ✅ **最終一致性**：冪等性、重試、對帳
6. ✅ **方案選擇**：根據業務需求選擇合適方案

**下一章**：我們將學習 **Consensus Algorithm（Raft/Paxos）**，理解分佈式系統如何達成共識。
