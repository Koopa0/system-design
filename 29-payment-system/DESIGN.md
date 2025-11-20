# Chapter 29: Payment System（支付系統）

> **難度**：★★★★☆
> **預估時間**：5-6 週
> **核心概念**：冪等性、雙寫一致性、對帳系統、分散式交易

---

## Act 1: 重複支付的噩夢

週一早晨，Emma 收到了一封來自客戶的緊急郵件。

**Emma**：「各位早安！我們有個嚴重問題——有用戶反應他們的信用卡被扣款兩次，但訂單只有一筆。」

**David**（皺眉）：「這聽起來像是重複支付的問題。用戶點擊『付款』按鈕時發生了什麼？」

**Sarah**：「讓我看看程式碼...」

```go
// ❌ 錯誤示範：沒有冪等性保護
func (s *PaymentService) CreatePayment(ctx context.Context, req *CreatePaymentRequest) (*Payment, error) {
    // 1. 建立支付記錄
    payment := &Payment{
        OrderID: req.OrderID,
        Amount:  req.Amount,
        Status:  "pending",
    }
    if err := s.repo.Create(ctx, payment); err != nil {
        return nil, err
    }

    // 2. 呼叫第三方支付
    result, err := s.stripeClient.Charge(req.Amount, req.CardToken)
    if err != nil {
        return nil, err
    }

    // 3. 更新狀態
    payment.Status = "success"
    s.repo.Update(ctx, payment)

    return payment, nil
}
```

**Michael**：「我看到問題了！如果用戶因為網路延遲多次點擊按鈕，這個函式會被呼叫多次。每次呼叫都會建立新的支付記錄並扣款。」

**Emma**：「沒錯。而且更糟的是，如果第 2 步成功但第 3 步失敗，我們的資料庫會顯示 `pending`，但 Stripe 已經扣款成功了。」

**David**：「這就是為什麼我們需要 **冪等性設計（Idempotency）**。相同的請求無論執行多少次，結果都應該一樣。」

### 冪等性設計

**Sarah**：「我們可以使用 **Idempotency Key** 來確保冪等性：」

```go
// ✅ 正確示範：使用 Idempotency Key
func (s *PaymentService) CreatePayment(ctx context.Context, req *CreatePaymentRequest) (*Payment, error) {
    // 1. 生成或使用客戶端提供的 Idempotency Key
    idempotencyKey := req.IdempotencyKey
    if idempotencyKey == "" {
        idempotencyKey = generateIdempotencyKey(req.OrderID, req.UserID)
    }

    // 2. 檢查是否已經處理過這個請求
    existing, err := s.repo.FindByIdempotencyKey(ctx, idempotencyKey)
    if err == nil && existing != nil {
        // 已經處理過，直接返回之前的結果
        return existing, nil
    }

    // 3. 使用分散式鎖確保只有一個請求在處理
    lock := s.redisClient.Lock(ctx, "payment:lock:"+idempotencyKey, 10*time.Second)
    if !lock.Acquired() {
        return nil, errors.New("重複請求，請稍後再試")
    }
    defer lock.Release()

    // 4. 再次檢查（Double-Check）
    existing, err = s.repo.FindByIdempotencyKey(ctx, idempotencyKey)
    if err == nil && existing != nil {
        return existing, nil
    }

    // 5. 建立支付記錄（包含 Idempotency Key）
    payment := &Payment{
        IdempotencyKey: idempotencyKey,
        OrderID:        req.OrderID,
        UserID:         req.UserID,
        Amount:         req.Amount,
        Status:         "pending",
        CreatedAt:      time.Now(),
    }

    if err := s.repo.Create(ctx, payment); err != nil {
        return nil, err
    }

    // 6. 呼叫第三方支付（使用相同的 Idempotency Key）
    result, err := s.stripeClient.ChargeWithIdempotency(
        idempotencyKey,
        req.Amount,
        req.CardToken,
    )
    if err != nil {
        payment.Status = "failed"
        payment.ErrorMessage = err.Error()
        s.repo.Update(ctx, payment)
        return nil, err
    }

    // 7. 更新狀態
    payment.Status = "success"
    payment.TransactionID = result.TransactionID
    payment.PaidAt = time.Now()
    s.repo.Update(ctx, payment)

    return payment, nil
}

// generateIdempotencyKey 生成冪等性鍵
func generateIdempotencyKey(orderID, userID string) string {
    return fmt.Sprintf("%s:%s:%d", orderID, userID, time.Now().Unix())
}
```

**Michael**：「這個設計有幾個關鍵點：」
1. **檢查已存在的記錄**：避免重複處理
2. **分散式鎖**：確保同一時間只有一個請求在處理
3. **Double-Check**：取得鎖後再次檢查
4. **第三方支付也使用 Idempotency Key**：Stripe 等支付服務也支援冪等性

**Emma**：「這樣就能保證無論用戶點擊多少次，只會扣款一次！」

---

## Act 2: 資料一致性的挑戰

**David**：「冪等性解決了重複支付的問題。但我們還有另一個挑戰：**雙寫一致性**。」

**Sarah**：「什麼是雙寫一致性？」

**David**：「當支付成功後，我們需要更新多個地方的資料：」
1. **支付記錄表**：記錄這筆支付
2. **訂單表**：更新訂單狀態為『已支付』
3. **用戶帳戶表**：扣除餘額或增加點數
4. **商家帳戶表**：增加收入

**Michael**：「如果這些操作有任何一個失敗，資料就會不一致。我們不能使用傳統的資料庫交易（Transaction），因為涉及多個服務和資料庫。」

**Emma**：「那該怎麼辦？」

### 本地訊息表（Transactional Outbox）

**Sarah**：「我們可以使用 **本地訊息表模式（Transactional Outbox Pattern）**：」

```go
// Payment 資料模型
type Payment struct {
    ID              int64     `db:"id"`
    IdempotencyKey  string    `db:"idempotency_key"`
    OrderID         string    `db:"order_id"`
    UserID          string    `db:"user_id"`
    Amount          int64     `db:"amount"`           // 單位：分
    Status          string    `db:"status"`           // pending, success, failed
    TransactionID   string    `db:"transaction_id"`   // 第三方交易ID
    PaidAt          time.Time `db:"paid_at"`
    CreatedAt       time.Time `db:"created_at"`
}

// PaymentEvent 本地訊息表
type PaymentEvent struct {
    ID        int64     `db:"id"`
    PaymentID int64     `db:"payment_id"`
    EventType string    `db:"event_type"` // payment_success, payment_failed
    Payload   string    `db:"payload"`    // JSON
    Status    string    `db:"status"`     // pending, published, failed
    CreatedAt time.Time `db:"created_at"`
    UpdatedAt time.Time `db:"updated_at"`
}
```

```go
// ✅ 使用本地訊息表確保一致性
func (s *PaymentService) ProcessPaymentSuccess(ctx context.Context, payment *Payment, result *stripe.ChargeResult) error {
    // 開始資料庫交易
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // 1. 更新支付狀態
    payment.Status = "success"
    payment.TransactionID = result.TransactionID
    payment.PaidAt = time.Now()

    if err := s.repo.UpdateWithTx(ctx, tx, payment); err != nil {
        return err
    }

    // 2. 寫入本地訊息表（在同一個交易中）
    event := &PaymentEvent{
        PaymentID: payment.ID,
        EventType: "payment_success",
        Payload: toJSON(map[string]interface{}{
            "payment_id":     payment.ID,
            "order_id":       payment.OrderID,
            "user_id":        payment.UserID,
            "amount":         payment.Amount,
            "transaction_id": payment.TransactionID,
        }),
        Status:    "pending",
        CreatedAt: time.Now(),
    }

    if err := s.eventRepo.CreateWithTx(ctx, tx, event); err != nil {
        return err
    }

    // 3. 提交交易
    if err := tx.Commit(); err != nil {
        return err
    }

    // 成功！支付記錄和事件記錄已經原子性地寫入
    return nil
}
```

**David**：「這樣做的好處是：支付記錄和事件記錄在同一個資料庫交易中，要麼全部成功，要麼全部失敗。」

### 事件發佈器（Event Publisher）

**Michael**：「接下來，我們需要一個背景服務來發佈這些事件：」

```go
// EventPublisher 事件發佈器
type EventPublisher struct {
    eventRepo   EventRepository
    kafkaWriter *kafka.Writer
}

// Run 持續掃描並發佈事件
func (p *EventPublisher) Run(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            p.publishPendingEvents(ctx)
        }
    }
}

// publishPendingEvents 發佈待處理的事件
func (p *EventPublisher) publishPendingEvents(ctx context.Context) {
    // 1. 查詢待發佈的事件（限制數量避免一次處理太多）
    events, err := p.eventRepo.FindPendingEvents(ctx, 100)
    if err != nil {
        log.Error("查詢待發佈事件失敗", err)
        return
    }

    for _, event := range events {
        // 2. 發佈到 Kafka
        err := p.kafkaWriter.WriteMessages(ctx, kafka.Message{
            Key:   []byte(event.PaymentID),
            Value: []byte(event.Payload),
            Headers: []kafka.Header{
                {Key: "event_type", Value: []byte(event.EventType)},
            },
        })

        if err != nil {
            log.Error("發佈事件失敗", "event_id", event.ID, "error", err)

            // 標記為失敗
            event.Status = "failed"
            event.UpdatedAt = time.Now()
            p.eventRepo.Update(ctx, event)
            continue
        }

        // 3. 標記為已發佈
        event.Status = "published"
        event.UpdatedAt = time.Now()
        p.eventRepo.Update(ctx, event)

        log.Info("事件發佈成功", "event_id", event.ID, "event_type", event.EventType)
    }
}
```

**Sarah**：「所以流程是：」
1. 支付成功後，更新支付記錄 + 寫入事件表（同一個交易）
2. 背景服務掃描事件表，發佈到 Kafka
3. 其他服務（訂單服務、帳戶服務）訂閱 Kafka，更新各自的資料

**Emma**：「這樣即使 Kafka 暫時掛掉，事件也不會丟失，因為已經持久化在事件表中！」

---

## Act 3: 對帳系統

**David**：「即使我們有了冪等性和一致性保證,還是需要 **對帳系統（Reconciliation System）** 來發現和修復資料不一致。」

**Michael**：「為什麼會有不一致？」

**David**：「有很多可能的原因：」
1. **網路故障**：我們以為第三方支付失敗了,但實際上成功了
2. **回調遺失**：第三方支付的回調通知沒有送達
3. **時序問題**：事件處理的順序錯誤
4. **Bug**：程式碼有 Bug 導致狀態更新失敗

**Sarah**：「所以我們需要定期比對我們的資料和第三方支付的資料？」

### T+1 對帳

**David**：「沒錯！標準做法是 **T+1 對帳**（Transaction + 1 Day）：」

```go
// ReconciliationService 對帳服務
type ReconciliationService struct {
    paymentRepo PaymentRepository
    stripeClient *stripe.Client
    discrepancyRepo DiscrepancyRepository
}

// ReconcileDate 對帳指定日期
func (s *ReconciliationService) ReconcileDate(ctx context.Context, date time.Time) (*ReconciliationReport, error) {
    report := &ReconciliationReport{
        Date:      date,
        StartTime: time.Now(),
    }

    // 1. 獲取我們系統中該日期的所有支付記錄
    ourPayments, err := s.paymentRepo.FindByDate(ctx, date)
    if err != nil {
        return nil, err
    }

    report.OurPaymentCount = len(ourPayments)
    report.OurTotalAmount = sumAmount(ourPayments)

    // 2. 獲取 Stripe 的對帳檔案（Balance Transaction）
    stripePayments, err := s.stripeClient.ListBalanceTransactions(ctx, date)
    if err != nil {
        return nil, err
    }

    report.StripePaymentCount = len(stripePayments)
    report.StripeTotalAmount = sumAmount(stripePayments)

    // 3. 比對差異
    discrepancies := s.findDiscrepancies(ourPayments, stripePayments)
    report.DiscrepancyCount = len(discrepancies)

    // 4. 記錄差異
    for _, d := range discrepancies {
        s.discrepancyRepo.Create(ctx, d)
    }

    report.EndTime = time.Now()
    report.Duration = report.EndTime.Sub(report.StartTime)

    return report, nil
}

// findDiscrepancies 找出差異
func (s *ReconciliationService) findDiscrepancies(ours []*Payment, theirs []*stripe.BalanceTransaction) []*Discrepancy {
    var discrepancies []*Discrepancy

    // 建立 Map 方便查找
    ourMap := make(map[string]*Payment)
    for _, p := range ours {
        ourMap[p.TransactionID] = p
    }

    theirMap := make(map[string]*stripe.BalanceTransaction)
    for _, t := range theirs {
        theirMap[t.ID] = t
    }

    // 檢查我們有但 Stripe 沒有的
    for txID, ourPayment := range ourMap {
        if _, exists := theirMap[txID]; !exists {
            discrepancies = append(discrepancies, &Discrepancy{
                Type:          "missing_in_stripe",
                PaymentID:     ourPayment.ID,
                TransactionID: txID,
                OurAmount:     ourPayment.Amount,
                OurStatus:     ourPayment.Status,
                CreatedAt:     time.Now(),
            })
        }
    }

    // 檢查 Stripe 有但我們沒有的
    for txID, stripeTx := range theirMap {
        if _, exists := ourMap[txID]; !exists {
            discrepancies = append(discrepancies, &Discrepancy{
                Type:           "missing_in_our_system",
                TransactionID:  txID,
                StripeAmount:   stripeTx.Amount,
                StripeStatus:   stripeTx.Status,
                CreatedAt:      time.Now(),
            })
        }
    }

    // 檢查金額不一致的
    for txID, ourPayment := range ourMap {
        if stripeTx, exists := theirMap[txID]; exists {
            if ourPayment.Amount != stripeTx.Amount {
                discrepancies = append(discrepancies, &Discrepancy{
                    Type:          "amount_mismatch",
                    PaymentID:     ourPayment.ID,
                    TransactionID: txID,
                    OurAmount:     ourPayment.Amount,
                    StripeAmount:  stripeTx.Amount,
                    CreatedAt:     time.Now(),
                })
            }

            if ourPayment.Status != stripeTx.Status {
                discrepancies = append(discrepancies, &Discrepancy{
                    Type:          "status_mismatch",
                    PaymentID:     ourPayment.ID,
                    TransactionID: txID,
                    OurStatus:     ourPayment.Status,
                    StripeStatus:  stripeTx.Status,
                    CreatedAt:     time.Now(),
                })
            }
        }
    }

    return discrepancies
}

// ReconciliationReport 對帳報告
type ReconciliationReport struct {
    Date               time.Time
    OurPaymentCount    int
    OurTotalAmount     int64
    StripePaymentCount int
    StripeTotalAmount  int64
    DiscrepancyCount   int
    StartTime          time.Time
    EndTime            time.Time
    Duration           time.Duration
}

// Discrepancy 差異記錄
type Discrepancy struct {
    ID             int64
    Type           string  // missing_in_stripe, missing_in_our_system, amount_mismatch, status_mismatch
    PaymentID      int64
    TransactionID  string
    OurAmount      int64
    OurStatus      string
    StripeAmount   int64
    StripeStatus   string
    Resolved       bool
    ResolvedAt     time.Time
    ResolvedBy     string
    Resolution     string
    CreatedAt      time.Time
}
```

**Emma**：「對帳發現差異後要怎麼處理？」

**Michael**：「這需要人工介入。我們可以建立一個管理後台，讓財務團隊查看差異並決定如何處理：」

```go
// ResolveDiscrepancy 解決差異
func (s *ReconciliationService) ResolveDiscrepancy(ctx context.Context, discrepancyID int64, resolution string, operator string) error {
    discrepancy, err := s.discrepancyRepo.FindByID(ctx, discrepancyID)
    if err != nil {
        return err
    }

    switch discrepancy.Type {
    case "missing_in_stripe":
        // 我們記錄了支付成功，但 Stripe 沒有
        // 可能的處理：
        // 1. 重新查詢 Stripe 確認（可能是對帳檔案延遲）
        // 2. 退款給用戶
        // 3. 標記為詐騙訂單

    case "missing_in_our_system":
        // Stripe 有扣款，但我們沒記錄
        // 可能的處理：
        // 1. 補建支付記錄
        // 2. 關聯到正確的訂單

    case "amount_mismatch":
        // 金額不一致
        // 需要調查原因並修正

    case "status_mismatch":
        // 狀態不一致
        // 以 Stripe 的狀態為準，更新我們的記錄
    }

    // 標記為已解決
    discrepancy.Resolved = true
    discrepancy.ResolvedAt = time.Now()
    discrepancy.ResolvedBy = operator
    discrepancy.Resolution = resolution

    return s.discrepancyRepo.Update(ctx, discrepancy)
}
```

**Sarah**：「所以對帳系統的核心是：**定期比對 + 人工審核 + 修復差異**。」

---

## Act 4: 分散式交易與 Saga 模式

**David**：「我們已經解決了支付本身的問題。但還記得嗎？支付成功後需要更新訂單、帳戶等多個服務。」

**Emma**：「對，我們使用了本地訊息表 + Kafka 事件。但如果訂單服務更新失敗了怎麼辦？」

**Michael**：「這就需要 **Saga 模式（Saga Pattern）** 來協調分散式交易。」

### Saga 模式簡介

**Sarah**：「Saga 是什麼？」

**David**：「Saga 是一系列本地交易的組合。如果某個步驟失敗，會執行補償交易（Compensation）來回滾之前的操作。」

**Michael**：「支付流程的 Saga 可以這樣設計：」

```go
// PaymentSaga 支付 Saga
type PaymentSaga struct {
    paymentService *PaymentService
    orderService   *OrderService
    accountService *AccountService
    kafkaReader    *kafka.Reader
}

// HandlePaymentSuccessEvent 處理支付成功事件
func (s *PaymentSaga) HandlePaymentSuccessEvent(ctx context.Context, event *PaymentSuccessEvent) error {
    // Saga 步驟
    steps := []SagaStep{
        {
            Name:        "更新訂單狀態",
            Execute:     s.updateOrderStatus,
            Compensate:  s.revertOrderStatus,
        },
        {
            Name:        "扣除用戶餘額",
            Execute:     s.deductUserBalance,
            Compensate:  s.refundUserBalance,
        },
        {
            Name:        "增加商家收入",
            Execute:     s.creditMerchantAccount,
            Compensate:  s.debitMerchantAccount,
        },
    }

    // 執行 Saga
    executor := NewSagaExecutor(steps)
    return executor.Execute(ctx, event)
}

// SagaStep Saga 步驟
type SagaStep struct {
    Name       string
    Execute    func(context.Context, *PaymentSuccessEvent) error
    Compensate func(context.Context, *PaymentSuccessEvent) error
}

// SagaExecutor Saga 執行器
type SagaExecutor struct {
    steps          []SagaStep
    completedSteps []int // 記錄已完成的步驟索引
}

// Execute 執行 Saga
func (e *SagaExecutor) Execute(ctx context.Context, event *PaymentSuccessEvent) error {
    for i, step := range e.steps {
        log.Info("執行 Saga 步驟", "step", step.Name)

        err := step.Execute(ctx, event)
        if err != nil {
            log.Error("Saga 步驟失敗", "step", step.Name, "error", err)

            // 執行補償（回滾）
            e.compensate(ctx, event)
            return fmt.Errorf("Saga 失敗於步驟 %s: %w", step.Name, err)
        }

        e.completedSteps = append(e.completedSteps, i)
    }

    log.Info("Saga 執行成功")
    return nil
}

// compensate 執行補償交易
func (e *SagaExecutor) compensate(ctx context.Context, event *PaymentSuccessEvent) {
    // 反向執行補償
    for i := len(e.completedSteps) - 1; i >= 0; i-- {
        stepIndex := e.completedSteps[i]
        step := e.steps[stepIndex]

        log.Info("執行 Saga 補償", "step", step.Name)

        err := step.Compensate(ctx, event)
        if err != nil {
            // 補償失敗是嚴重問題，需要告警
            log.Error("Saga 補償失敗", "step", step.Name, "error", err)
            // 發送告警通知運維團隊
            alertOps(fmt.Sprintf("Saga 補償失敗: %s", step.Name))
        }
    }
}
```

**Emma**：「所以如果『扣除用戶餘額』這步失敗了，會自動執行 `revertOrderStatus` 把訂單狀態改回去？」

**Michael**：「完全正確！讓我們看看具體的實作：」

```go
// updateOrderStatus 更新訂單狀態
func (s *PaymentSaga) updateOrderStatus(ctx context.Context, event *PaymentSuccessEvent) error {
    return s.orderService.UpdateStatus(ctx, &UpdateOrderStatusRequest{
        OrderID: event.OrderID,
        Status:  "paid",
        PaidAt:  event.PaidAt,
    })
}

// revertOrderStatus 回滾訂單狀態
func (s *PaymentSaga) revertOrderStatus(ctx context.Context, event *PaymentSuccessEvent) error {
    return s.orderService.UpdateStatus(ctx, &UpdateOrderStatusRequest{
        OrderID: event.OrderID,
        Status:  "pending_payment",
        PaidAt:  time.Time{}, // 清空支付時間
    })
}

// deductUserBalance 扣除用戶餘額
func (s *PaymentSaga) deductUserBalance(ctx context.Context, event *PaymentSuccessEvent) error {
    return s.accountService.DeductBalance(ctx, &DeductBalanceRequest{
        UserID:        event.UserID,
        Amount:        event.Amount,
        TransactionID: event.TransactionID,
        Reason:        fmt.Sprintf("支付訂單 %s", event.OrderID),
    })
}

// refundUserBalance 退款給用戶
func (s *PaymentSaga) refundUserBalance(ctx context.Context, event *PaymentSuccessEvent) error {
    return s.accountService.AddBalance(ctx, &AddBalanceRequest{
        UserID:        event.UserID,
        Amount:        event.Amount,
        TransactionID: event.TransactionID + "-refund",
        Reason:        fmt.Sprintf("訂單 %s 支付失敗退款", event.OrderID),
    })
}
```

**Sarah**：「但這有個問題：如果補償交易也失敗了怎麼辦？」

**David**：「這就是 Saga 的侷限。補償交易必須設計成 **儘可能不會失敗**，並且要有 **重試機制** 和 **人工介入流程**。」

### Saga 狀態持久化

**Michael**：「為了確保可靠性，我們需要持久化 Saga 的執行狀態：」

```go
// SagaExecution Saga 執行記錄
type SagaExecution struct {
    ID            string    `db:"id"`
    SagaType      string    `db:"saga_type"`      // payment_success, refund, etc.
    PaymentID     int64     `db:"payment_id"`
    EventPayload  string    `db:"event_payload"`  // JSON
    Status        string    `db:"status"`         // running, completed, failed, compensating, compensated
    CurrentStep   int       `db:"current_step"`
    CompletedSteps string   `db:"completed_steps"` // JSON array
    ErrorMessage  string    `db:"error_message"`
    CreatedAt     time.Time `db:"created_at"`
    UpdatedAt     time.Time `db:"updated_at"`
}

// PersistentSagaExecutor 持久化 Saga 執行器
type PersistentSagaExecutor struct {
    steps    []SagaStep
    repo     SagaExecutionRepository
    execution *SagaExecution
}

// Execute 執行 Saga（帶持久化）
func (e *PersistentSagaExecutor) Execute(ctx context.Context, event *PaymentSuccessEvent) error {
    // 1. 建立執行記錄
    e.execution = &SagaExecution{
        ID:           uuid.New().String(),
        SagaType:     "payment_success",
        PaymentID:    event.PaymentID,
        EventPayload: toJSON(event),
        Status:       "running",
        CurrentStep:  0,
        CreatedAt:    time.Now(),
    }

    if err := e.repo.Create(ctx, e.execution); err != nil {
        return err
    }

    // 2. 執行每個步驟
    var completedSteps []int

    for i, step := range e.steps {
        log.Info("執行 Saga 步驟", "execution_id", e.execution.ID, "step", step.Name)

        // 更新當前步驟
        e.execution.CurrentStep = i
        e.execution.UpdatedAt = time.Now()
        e.repo.Update(ctx, e.execution)

        err := step.Execute(ctx, event)
        if err != nil {
            log.Error("Saga 步驟失敗", "step", step.Name, "error", err)

            // 標記為補償中
            e.execution.Status = "compensating"
            e.execution.ErrorMessage = err.Error()
            e.repo.Update(ctx, e.execution)

            // 執行補償
            e.compensateWithPersistence(ctx, event, completedSteps)
            return fmt.Errorf("Saga 失敗於步驟 %s: %w", step.Name, err)
        }

        completedSteps = append(completedSteps, i)
        e.execution.CompletedSteps = toJSON(completedSteps)
        e.repo.Update(ctx, e.execution)
    }

    // 3. 標記為完成
    e.execution.Status = "completed"
    e.execution.UpdatedAt = time.Now()
    e.repo.Update(ctx, e.execution)

    log.Info("Saga 執行成功", "execution_id", e.execution.ID)
    return nil
}

// compensateWithPersistence 執行補償（帶持久化）
func (e *PersistentSagaExecutor) compensateWithPersistence(ctx context.Context, event *PaymentSuccessEvent, completedSteps []int) {
    for i := len(completedSteps) - 1; i >= 0; i-- {
        stepIndex := completedSteps[i]
        step := e.steps[stepIndex]

        log.Info("執行 Saga 補償", "execution_id", e.execution.ID, "step", step.Name)

        err := step.Compensate(ctx, event)
        if err != nil {
            log.Error("Saga 補償失敗", "step", step.Name, "error", err)

            // 標記為補償失敗（需要人工介入）
            e.execution.Status = "compensation_failed"
            e.execution.ErrorMessage += fmt.Sprintf("; 補償失敗於步驟 %s: %v", step.Name, err)
            e.repo.Update(ctx, e.execution)

            alertOps(fmt.Sprintf("Saga 補償失敗 [%s]: %s", e.execution.ID, step.Name))
            return
        }
    }

    // 補償完成
    e.execution.Status = "compensated"
    e.execution.UpdatedAt = time.Now()
    e.repo.Update(ctx, e.execution)
}
```

**Emma**：「這樣即使服務重啟，我們也能知道 Saga 執行到哪一步了！」

**David**：「沒錯。而且我們可以建立一個監控面板，查看失敗的 Saga 並手動重試。」

---

## Act 5: 退款處理

**Sarah**：「我們討論了支付成功的流程。那退款呢？」

**Michael**：「退款也是一個 Saga，但流程相反：」

```go
// RefundSaga 退款 Saga
type RefundSaga struct {
    paymentService *PaymentService
    orderService   *OrderService
    accountService *AccountService
    stripeClient   *stripe.Client
}

// ProcessRefund 處理退款
func (s *RefundSaga) ProcessRefund(ctx context.Context, req *RefundRequest) error {
    // 1. 驗證是否可退款
    payment, err := s.paymentService.GetPayment(ctx, req.PaymentID)
    if err != nil {
        return err
    }

    if payment.Status != "success" {
        return errors.New("只有成功的支付才能退款")
    }

    // 檢查是否已經退款
    if payment.RefundStatus == "refunded" {
        return errors.New("此支付已經退款")
    }

    // 檢查退款期限（例如：7天內）
    if time.Since(payment.PaidAt) > 7*24*time.Hour {
        return errors.New("超過退款期限")
    }

    // 2. 定義 Saga 步驟
    steps := []SagaStep{
        {
            Name:       "呼叫第三方退款",
            Execute:    s.refundViaStripe,
            Compensate: s.cancelStripeRefund, // 註：很多支付服務不支援取消退款
        },
        {
            Name:       "更新支付記錄",
            Execute:    s.updatePaymentRefundStatus,
            Compensate: s.revertPaymentRefundStatus,
        },
        {
            Name:       "更新訂單狀態",
            Execute:    s.updateOrderRefundStatus,
            Compensate: s.revertOrderRefundStatus,
        },
        {
            Name:       "退款到用戶帳戶",
            Execute:    s.refundToUserAccount,
            Compensate: s.deductUserRefund,
        },
    }

    // 3. 執行 Saga
    executor := NewPersistentSagaExecutor(steps, s.sagaRepo)
    return executor.Execute(ctx, &RefundEvent{
        PaymentID:     req.PaymentID,
        RefundAmount:  req.Amount,
        RefundReason:  req.Reason,
        OperatorID:    req.OperatorID,
    })
}

// refundViaStripe 通過 Stripe 退款
func (s *RefundSaga) refundViaStripe(ctx context.Context, event *RefundEvent) error {
    result, err := s.stripeClient.Refund(ctx, &stripe.RefundRequest{
        ChargeID: event.TransactionID,
        Amount:   event.RefundAmount,
        Reason:   event.RefundReason,
    })

    if err != nil {
        return fmt.Errorf("Stripe 退款失敗: %w", err)
    }

    // 儲存退款 ID
    event.RefundTransactionID = result.ID
    return nil
}

// updatePaymentRefundStatus 更新支付記錄
func (s *RefundSaga) updatePaymentRefundStatus(ctx context.Context, event *RefundEvent) error {
    return s.paymentService.UpdateRefundStatus(ctx, &UpdateRefundStatusRequest{
        PaymentID:            event.PaymentID,
        RefundStatus:         "refunded",
        RefundAmount:         event.RefundAmount,
        RefundTransactionID:  event.RefundTransactionID,
        RefundReason:         event.RefundReason,
        RefundAt:             time.Now(),
    })
}
```

**Emma**：「退款比支付複雜嗎？」

**David**：「退款有一些特殊考慮：」

### 退款的特殊情況

```go
// RefundType 退款類型
type RefundType string

const (
    RefundTypeFull    RefundType = "full"    // 全額退款
    RefundTypePartial RefundType = "partial" // 部分退款
)

// RefundPolicy 退款政策
type RefundPolicy struct {
    MaxRefundDays      int     // 最大退款天數
    PartialRefundRatio float64 // 部分退款比例
    HandlingFee        int64   // 手續費（分）
}

// CalculateRefundAmount 計算退款金額
func (p *RefundPolicy) CalculateRefundAmount(payment *Payment, refundType RefundType, daysSincePaid int) (int64, error) {
    // 檢查是否超過退款期限
    if daysSincePaid > p.MaxRefundDays {
        return 0, errors.New("超過退款期限")
    }

    var refundAmount int64

    switch refundType {
    case RefundTypeFull:
        refundAmount = payment.Amount

    case RefundTypePartial:
        refundAmount = int64(float64(payment.Amount) * p.PartialRefundRatio)
    }

    // 扣除手續費
    refundAmount -= p.HandlingFee

    if refundAmount < 0 {
        refundAmount = 0
    }

    return refundAmount, nil
}
```

**Michael**：「還有一個重要問題：**退款衝突**。如果用戶同時發起多個退款請求怎麼辦？」

```go
// ProcessRefundWithLock 使用分散式鎖處理退款
func (s *RefundSaga) ProcessRefundWithLock(ctx context.Context, req *RefundRequest) error {
    // 1. 獲取分散式鎖
    lockKey := fmt.Sprintf("refund:lock:%d", req.PaymentID)
    lock := s.redisClient.Lock(ctx, lockKey, 30*time.Second)

    if !lock.Acquired() {
        return errors.New("該支付正在處理退款，請稍後再試")
    }
    defer lock.Release()

    // 2. 再次檢查退款狀態（Double-Check）
    payment, err := s.paymentService.GetPayment(ctx, req.PaymentID)
    if err != nil {
        return err
    }

    if payment.RefundStatus == "refunding" {
        return errors.New("該支付正在退款中")
    }

    if payment.RefundStatus == "refunded" {
        return errors.New("該支付已經退款")
    }

    // 3. 標記為退款中
    payment.RefundStatus = "refunding"
    if err := s.paymentService.UpdatePayment(ctx, payment); err != nil {
        return err
    }

    // 4. 執行退款 Saga
    return s.ProcessRefund(ctx, req)
}
```

**Sarah**：「所以退款的關鍵是：**驗證 + 鎖 + Saga + 狀態管理**。」

---

## Act 6: 效能優化

**Emma**：「我們的支付系統已經很完善了。但在大促期間，QPS 會暴增。我們該如何優化效能？」

**David**：「讓我們從幾個維度來優化。」

### 1. 資料庫優化

```go
// 支付表分片策略
// 按用戶 ID 分片（假設有 16 個分片）
func (r *PaymentRepository) getShardID(userID string) int {
    hash := crc32.ChecksumIEEE([]byte(userID))
    return int(hash % 16)
}

// 根據分片 ID 選擇資料庫
func (r *PaymentRepository) getDB(userID string) *sql.DB {
    shardID := r.getShardID(userID)
    return r.dbShards[shardID]
}

// Create 建立支付記錄（自動路由到正確的分片）
func (r *PaymentRepository) Create(ctx context.Context, payment *Payment) error {
    db := r.getDB(payment.UserID)

    query := `
        INSERT INTO payments (
            idempotency_key, order_id, user_id, amount, status, created_at
        ) VALUES (?, ?, ?, ?, ?, ?)
    `

    result, err := db.ExecContext(ctx, query,
        payment.IdempotencyKey,
        payment.OrderID,
        payment.UserID,
        payment.Amount,
        payment.Status,
        payment.CreatedAt,
    )

    if err != nil {
        return err
    }

    id, _ := result.LastInsertId()
    payment.ID = id

    return nil
}
```

### 2. Redis 快取

**Michael**：「我們可以快取一些熱點資料：」

```go
// CachedPaymentService 帶快取的支付服務
type CachedPaymentService struct {
    paymentService *PaymentService
    redisClient    *redis.Client
    cacheTTL       time.Duration
}

// GetPayment 獲取支付記錄（優先從快取）
func (s *CachedPaymentService) GetPayment(ctx context.Context, paymentID int64) (*Payment, error) {
    // 1. 先查快取
    cacheKey := fmt.Sprintf("payment:%d", paymentID)

    cached, err := s.redisClient.Get(ctx, cacheKey).Result()
    if err == nil {
        var payment Payment
        if err := json.Unmarshal([]byte(cached), &payment); err == nil {
            return &payment, nil
        }
    }

    // 2. 快取未命中，查資料庫
    payment, err := s.paymentService.GetPayment(ctx, paymentID)
    if err != nil {
        return nil, err
    }

    // 3. 寫入快取
    paymentJSON, _ := json.Marshal(payment)
    s.redisClient.Set(ctx, cacheKey, paymentJSON, s.cacheTTL)

    return payment, nil
}

// InvalidateCache 使快取失效
func (s *CachedPaymentService) InvalidateCache(ctx context.Context, paymentID int64) {
    cacheKey := fmt.Sprintf("payment:%d", paymentID)
    s.redisClient.Del(ctx, cacheKey)
}
```

### 3. 非同步處理

**Sarah**：「支付成功後的一些非關鍵操作可以非同步處理：」

```go
// ProcessPaymentSuccessAsync 非同步處理支付成功
func (s *PaymentService) ProcessPaymentSuccessAsync(ctx context.Context, payment *Payment) error {
    // 1. 關鍵操作：更新支付狀態（同步）
    payment.Status = "success"
    if err := s.repo.Update(ctx, payment); err != nil {
        return err
    }

    // 2. 發送事件到 Kafka（同步，確保可靠性）
    event := &PaymentSuccessEvent{
        PaymentID:     payment.ID,
        OrderID:       payment.OrderID,
        UserID:        payment.UserID,
        Amount:        payment.Amount,
        TransactionID: payment.TransactionID,
        PaidAt:        payment.PaidAt,
    }

    if err := s.kafkaWriter.WriteMessages(ctx, kafka.Message{
        Key:   []byte(payment.OrderID),
        Value: []byte(toJSON(event)),
    }); err != nil {
        return err
    }

    // 3. 非關鍵操作：發送通知、更新統計等（非同步）
    go func() {
        // 使用新的 context 避免受原 context 取消影響
        bgCtx := context.Background()

        // 發送郵件通知
        s.emailService.SendPaymentSuccessEmail(bgCtx, payment)

        // 發送簡訊通知
        s.smsService.SendPaymentSuccessSMS(bgCtx, payment)

        // 更新用戶支付統計
        s.analyticsService.UpdatePaymentStats(bgCtx, payment)

        // 發送 webhook 到商家系統
        s.webhookService.SendPaymentWebhook(bgCtx, payment)
    }()

    return nil
}
```

### 4. 連接池優化

```go
// 資料庫連接池配置
func NewDatabasePool(dsn string) (*sql.DB, error) {
    db, err := sql.Open("mysql", dsn)
    if err != nil {
        return nil, err
    }

    // 最大開啟連接數
    db.SetMaxOpenConns(100)

    // 最大閒置連接數
    db.SetMaxIdleConns(20)

    // 連接最大生命週期
    db.SetConnMaxLifetime(time.Hour)

    // 連接最大閒置時間
    db.SetConnMaxIdleTime(10 * time.Minute)

    return db, nil
}

// HTTP 客戶端連接池配置
func NewHTTPClient() *http.Client {
    return &http.Client{
        Timeout: 10 * time.Second,
        Transport: &http.Transport{
            MaxIdleConns:        100,
            MaxIdleConnsPerHost: 20,
            IdleConnTimeout:     90 * time.Second,
            DisableKeepAlives:   false,
        },
    }
}
```

**Emma**：「這些優化能帶來多少效能提升？」

**David**：「讓我們看看基準測試結果：」

```go
// 效能測試
func BenchmarkPaymentCreation(b *testing.B) {
    // 未優化版本：~500 ops/sec
    // 優化後版本：~5000 ops/sec
    // 提升：10x

    service := NewOptimizedPaymentService()

    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            req := &CreatePaymentRequest{
                OrderID:   uuid.New().String(),
                UserID:    "user123",
                Amount:    10000,
                CardToken: "tok_visa",
            }

            _, err := service.CreatePayment(context.Background(), req)
            if err != nil {
                b.Fatal(err)
            }
        }
    })
}
```

---

## Act 7: 監控與告警

**Michael**：「最後，我們需要完善的監控系統。」

**Sarah**：「支付系統應該監控哪些指標？」

### 核心指標

```go
// PaymentMetrics 支付系統監控指標
type PaymentMetrics struct {
    // 請求指標
    TotalRequests      prometheus.Counter   // 總請求數
    SuccessRequests    prometheus.Counter   // 成功請求數
    FailedRequests     prometheus.Counter   // 失敗請求數

    // 延遲指標
    RequestDuration    prometheus.Histogram // 請求耗時
    StripeAPILatency   prometheus.Histogram // Stripe API 延遲
    DatabaseLatency    prometheus.Histogram // 資料庫延遲

    // 業務指標
    PaymentAmount      prometheus.Counter   // 支付總金額
    RefundAmount       prometheus.Counter   // 退款總金額
    DiscrepancyCount   prometheus.Counter   // 對帳差異數

    // 錯誤指標
    IdempotencyConflicts prometheus.Counter // 冪等性衝突數
    TimeoutErrors        prometheus.Counter // 逾時錯誤數
    StripeErrors         prometheus.Counter // Stripe 錯誤數
}

// RecordPaymentSuccess 記錄支付成功
func (m *PaymentMetrics) RecordPaymentSuccess(duration time.Duration, amount int64) {
    m.TotalRequests.Inc()
    m.SuccessRequests.Inc()
    m.RequestDuration.Observe(duration.Seconds())
    m.PaymentAmount.Add(float64(amount))
}

// RecordPaymentFailure 記錄支付失敗
func (m *PaymentMetrics) RecordPaymentFailure(duration time.Duration, errorType string) {
    m.TotalRequests.Inc()
    m.FailedRequests.Inc()
    m.RequestDuration.Observe(duration.Seconds())

    switch errorType {
    case "timeout":
        m.TimeoutErrors.Inc()
    case "stripe_error":
        m.StripeErrors.Inc()
    }
}
```

### 健康檢查

```go
// HealthChecker 健康檢查
type HealthChecker struct {
    db           *sql.DB
    redis        *redis.Client
    stripeClient *stripe.Client
}

// CheckHealth 執行健康檢查
func (h *HealthChecker) CheckHealth(ctx context.Context) *HealthStatus {
    status := &HealthStatus{
        Timestamp: time.Now(),
        Checks:    make(map[string]CheckResult),
    }

    // 檢查資料庫
    dbCheck := h.checkDatabase(ctx)
    status.Checks["database"] = dbCheck

    // 檢查 Redis
    redisCheck := h.checkRedis(ctx)
    status.Checks["redis"] = redisCheck

    // 檢查 Stripe
    stripeCheck := h.checkStripe(ctx)
    status.Checks["stripe"] = stripeCheck

    // 整體狀態
    status.Overall = "healthy"
    for _, check := range status.Checks {
        if check.Status != "healthy" {
            status.Overall = "unhealthy"
            break
        }
    }

    return status
}

// checkDatabase 檢查資料庫連接
func (h *HealthChecker) checkDatabase(ctx context.Context) CheckResult {
    start := time.Now()

    err := h.db.PingContext(ctx)

    duration := time.Since(start)

    if err != nil {
        return CheckResult{
            Status:   "unhealthy",
            Duration: duration,
            Error:    err.Error(),
        }
    }

    return CheckResult{
        Status:   "healthy",
        Duration: duration,
    }
}

// HealthStatus 健康狀態
type HealthStatus struct {
    Timestamp time.Time
    Overall   string
    Checks    map[string]CheckResult
}

// CheckResult 檢查結果
type CheckResult struct {
    Status   string        // healthy, unhealthy, degraded
    Duration time.Duration
    Error    string
}
```

### 告警規則

**David**：「我們應該設定以下告警規則：」

```yaml
# prometheus-alerts.yaml
groups:
  - name: payment_system
    rules:
      # 錯誤率告警
      - alert: HighPaymentErrorRate
        expr: |
          rate(payment_failed_requests_total[5m])
          /
          rate(payment_total_requests_total[5m])
          > 0.05
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "支付錯誤率過高"
          description: "過去 5 分鐘支付錯誤率 {{ $value }}% 超過 5%"

      # 延遲告警
      - alert: HighPaymentLatency
        expr: |
          histogram_quantile(0.99,
            rate(payment_request_duration_seconds_bucket[5m])
          ) > 3
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "支付延遲過高"
          description: "P99 延遲 {{ $value }}s 超過 3 秒"

      # Stripe API 告警
      - alert: StripeAPIErrors
        expr: rate(payment_stripe_errors_total[5m]) > 10
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Stripe API 錯誤頻繁"
          description: "過去 5 分鐘 Stripe 錯誤數 {{ $value }} 超過 10"

      # 對帳差異告警
      - alert: ReconciliationDiscrepancies
        expr: payment_discrepancy_count_total > 0
        for: 1h
        labels:
          severity: warning
        annotations:
          summary: "發現對帳差異"
          description: "有 {{ $value }} 筆對帳差異需要處理"

      # 資料庫連接告警
      - alert: DatabaseConnectionPoolExhausted
        expr: |
          mysql_global_status_threads_connected
          /
          mysql_global_variables_max_connections
          > 0.8
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "資料庫連接池即將耗盡"
          description: "連接使用率 {{ $value }}% 超過 80%"
```

**Emma**：「這樣我們就能及時發現並處理問題了！」

**Michael**：「沒錯。完善的監控和告警是支付系統穩定運行的關鍵。」

**Sarah**：「讓我總結一下我們學到的：」

### 支付系統設計要點

1. **冪等性設計**：防止重複支付
   - Idempotency Key
   - 分散式鎖
   - Double-Check

2. **資料一致性**：確保多服務資料同步
   - 本地訊息表（Transactional Outbox）
   - 事件驅動架構
   - Saga 模式

3. **對帳系統**：定期核對資料
   - T+1 對帳
   - 差異記錄
   - 人工審核

4. **退款處理**：安全可靠的退款
   - 退款 Saga
   - 分散式鎖
   - 狀態管理

5. **效能優化**：支撐高併發
   - 資料庫分片
   - Redis 快取
   - 連接池優化
   - 非同步處理

6. **監控告警**：及時發現問題
   - 核心指標監控
   - 健康檢查
   - 告警規則

**David**：「支付是電商系統的核心，必須做到 **安全、可靠、高效能**。每一筆錢都要負責！」

**Emma**：「我們的支付系統現在已經達到生產級別了。準備上線吧！」

---

## 總結

本章我們深入學習了 **支付系統（Payment System）** 的設計，涵蓋：

### 核心技術點

1. **冪等性設計**
   - Idempotency Key 機制
   - 分散式鎖（Redis Lock）
   - Double-Check 模式

2. **資料一致性**
   - 本地訊息表（Transactional Outbox）
   - 事件發佈器（Event Publisher）
   - 最終一致性

3. **對帳系統**
   - T+1 對帳流程
   - 差異檢測與處理
   - 人工審核機制

4. **分散式交易**
   - Saga 模式
   - 補償交易
   - 狀態持久化

5. **退款處理**
   - 退款 Saga
   - 退款政策
   - 衝突處理

6. **效能優化**
   - 資料庫分片
   - Redis 快取
   - 非同步處理
   - 連接池優化

7. **監控告警**
   - Prometheus 指標
   - 健康檢查
   - 告警規則

### 架構特點

- **可靠性**：冪等性 + 本地訊息表 + Saga 模式
- **一致性**：事件驅動 + 對帳系統 + 補償交易
- **高效能**：分片 + 快取 + 非同步 + 連接池
- **可觀測性**：完善的監控和告警機制

支付系統是金融級應用，對可靠性和一致性要求極高。通過本章學習，你已經掌握了構建生產級支付系統的核心技術！💰✨
