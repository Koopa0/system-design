package internal

import (
	"context"
	"fmt"
	"time"
)

// Saga 分布式事務協調（Distributed Transaction Coordination）
//
// 系統設計問題：微服務如何處理跨服務事務？
//
// 場景：下訂單流程
//   1. 創建訂單（Order Service）
//   2. 扣庫存（Inventory Service）
//   3. 扣款（Payment Service）
//
// 問題：如何保證三個操作的一致性？
//   - 部分成功怎麼辦？（訂單創建了，但扣款失敗）
//   - 如何回滾？（不能用資料庫事務，跨服務了）
//
// 方案 A：兩階段提交（2PC）
//   流程：
//     1. Prepare：所有服務準備（鎖定資源）
//     2. Commit：所有服務提交
//   問題：
//     - 阻塞：Coordinator 崩潰導致資源鎖定
//     - 性能差：多次網絡往返
//     - 單點：Coordinator 是瓶頸
//
// 選擇方案 B：Saga 模式（事件驅動）
//   核心思想：
//     - 將長事務拆分為多個本地事務
//     - 每個本地事務發布事件
//     - 失敗時執行補償事務（Compensating Transaction）
//
//   實現方式：
//     1. Choreography（編排）：事件驅動，去中心化 ✅
//     2. Orchestration（協調）：中央協調器，集中控制
//
// Saga Choreography 流程：
//
//   成功路徑：
//     Order Service: CreateOrder → OrderCreated event
//       ↓
//     Inventory Service: Subscribe → ReserveInventory → InventoryReserved event
//       ↓
//     Payment Service: Subscribe → ChargePayment → PaymentCompleted event
//       ↓
//     Order Service: Subscribe → CompleteOrder
//
//   失敗路徑（補償）：
//     Payment Service: ChargePayment failed → PaymentFailed event
//       ↓
//     Inventory Service: Subscribe → ReleaseInventory → InventoryReleased event
//       ↓
//     Order Service: Subscribe → CancelOrder
//
// 優勢：
//   - 非阻塞：異步執行，無鎖
//   - 高可用：無單點，服務獨立
//   - 可擴展：服務鬆耦合
//
// 權衡：
//   - 最終一致性：非原子（可能中間狀態）
//   - 複雜度：需設計補償邏輯
//   - 無隔離性：其他事務可能看到中間狀態

// OrderSaga 訂單 Saga（教學簡化版）
//
// 系統設計職責：
//   1. 訂閱相關事件（OrderCreated, InventoryReserved, PaymentFailed 等）
//   2. 根據事件執行下一步操作
//   3. 失敗時執行補償邏輯
//
// 教學簡化：
//   - 模擬 Inventory Service 和 Payment Service（實際應為獨立服務）
//   - 內存存儲 Saga 狀態（生產環境應持久化）
//   - 簡化補償邏輯（生產環境需更複雜的補償策略）
//
// 生產環境考量：
//   - Saga 狀態持久化：記錄當前步驟，重啟後可恢復
//   - 冪等性：同一個事件多次處理結果相同
//   - 補償事務設計：考慮所有失敗場景
//   - 超時處理：步驟超時自動補償
type OrderSaga struct {
	eventStore *EventStore
	repository *OrderRepository

	// 模擬的外部服務（教學用）
	inventoryService *MockInventoryService
	paymentService   *MockPaymentService
}

// NewOrderSaga 創建訂單 Saga
func NewOrderSaga(eventStore *EventStore, repository *OrderRepository) *OrderSaga {
	return &OrderSaga{
		eventStore:       eventStore,
		repository:       repository,
		inventoryService: NewMockInventoryService(),
		paymentService:   NewMockPaymentService(),
	}
}

// Start 啟動 Saga（訂閱事件）
//
// 系統設計模式：Event-Driven Choreography
//
// 訂閱的事件：
//   - OrderCreated：觸發庫存預留
//   - InventoryReserved：觸發支付
//   - PaymentCompleted：觸發訂單完成
//   - PaymentFailed：觸發補償（釋放庫存、取消訂單）
func (s *OrderSaga) Start() error {
	// 訂閱所有訂單事件
	go s.eventStore.Subscribe(context.Background(), s.HandleEvent)
	return nil
}

// HandleEvent Saga 事件處理器
//
// 系統設計流程：
//   1. 接收事件
//   2. 根據事件類型執行對應操作
//   3. 產生新事件（觸發下一步）
//
// 為什麼用事件驅動：
//   - 去中心化：無需中央協調器
//   - 解耦：服務間通過事件通信
//   - 可擴展：新增步驟只需訂閱事件
func (s *OrderSaga) HandleEvent(event *Event) error {
	fmt.Printf("🎯 Saga 收到事件: %s (Order: %s)\n", event.Type, event.AggregateID)

	switch event.Type {
	case "OrderCreated":
		// 步驟 1：預留庫存
		return s.handleOrderCreated(event)

	case "InventoryReserved":
		// 步驟 2：執行支付
		return s.handleInventoryReserved(event)

	case "InventoryFailed":
		// 補償：取消訂單（庫存預留失敗）
		return s.handleInventoryFailed(event)

	case "PaymentCompleted":
		// 步驟 3：完成訂單
		return s.handlePaymentCompleted(event)

	case "PaymentFailed":
		// 補償：釋放庫存、取消訂單
		return s.handlePaymentFailed(event)
	}

	return nil
}

// handleOrderCreated 處理 OrderCreated 事件（步驟 1：預留庫存）
func (s *OrderSaga) handleOrderCreated(event *Event) error {
	orderID := event.AggregateID

	// 模擬庫存服務：預留庫存
	if err := s.inventoryService.Reserve(orderID); err != nil {
		// 庫存預留失敗，發布 InventoryFailed 事件
		failedEvent := &Event{
			AggregateID: orderID,
			Type:        "InventoryFailed",
			Data: map[string]interface{}{
				"reason": err.Error(),
			},
			Timestamp: time.Now(),
		}
		s.eventStore.Append(context.Background(), failedEvent)
		return nil
	}

	// 庫存預留成功，發布 InventoryReserved 事件
	reservedEvent := &Event{
		AggregateID: orderID,
		Type:        "InventoryReserved",
		Data:        map[string]interface{}{},
		Timestamp:   time.Now(),
	}
	s.eventStore.Append(context.Background(), reservedEvent)

	fmt.Printf("   ✅ 庫存已預留: Order %s\n", orderID)
	return nil
}

// handleInventoryReserved 處理 InventoryReserved 事件（步驟 2：執行支付）
func (s *OrderSaga) handleInventoryReserved(event *Event) error {
	orderID := event.AggregateID

	// 模擬支付服務：扣款
	if err := s.paymentService.Charge(orderID); err != nil {
		// 支付失敗，發布 PaymentFailed 事件（觸發補償）
		failedEvent := &Event{
			AggregateID: orderID,
			Type:        "PaymentFailed",
			Data: map[string]interface{}{
				"reason": err.Error(),
			},
			Timestamp: time.Now(),
		}
		s.eventStore.Append(context.Background(), failedEvent)
		return nil
	}

	// 支付成功，發布 PaymentCompleted 事件
	completedEvent := &Event{
		AggregateID: orderID,
		Type:        "PaymentCompleted",
		Data:        map[string]interface{}{},
		Timestamp:   time.Now(),
	}
	s.eventStore.Append(context.Background(), completedEvent)

	fmt.Printf("   ✅ 支付成功: Order %s\n", orderID)
	return nil
}

// handlePaymentCompleted 處理 PaymentCompleted 事件（步驟 3：完成訂單）
func (s *OrderSaga) handlePaymentCompleted(event *Event) error {
	orderID := event.AggregateID

	// 加載訂單 Aggregate
	order, err := s.repository.Load(orderID)
	if err != nil {
		return err
	}

	// 執行命令：完成訂單
	if err := order.CompleteOrder(); err != nil {
		return err
	}

	// 保存事件
	s.repository.Save(order)

	fmt.Printf("   ✅ 訂單已完成: Order %s\n", orderID)
	return nil
}

// handleInventoryFailed 處理 InventoryFailed 事件（補償：取消訂單）
func (s *OrderSaga) handleInventoryFailed(event *Event) error {
	orderID := event.AggregateID
	reason := event.Data["reason"].(string)

	fmt.Printf("   ❌ 庫存預留失敗: %s, 原因: %s\n", orderID, reason)

	// 取消訂單
	cancelEvent := &Event{
		AggregateID: orderID,
		Type:        "OrderCancelled",
		Data: map[string]interface{}{
			"reason": "庫存不足",
		},
		Timestamp: time.Now(),
	}
	s.eventStore.Append(context.Background(), cancelEvent)

	return nil
}

// handlePaymentFailed 處理 PaymentFailed 事件（補償：釋放庫存、取消訂單）
//
// 系統設計重點：Compensating Transaction（補償事務）
//
// 為什麼需要補償：
//   - Saga 非原子：已執行的步驟無法回滾
//   - 保證最終一致性：通過補償恢復一致狀態
//
// 補償流程：
//   1. 釋放已預留的庫存
//   2. 取消訂單
//   3. 可選：通知用戶、退款等
func (s *OrderSaga) handlePaymentFailed(event *Event) error {
	orderID := event.AggregateID
	reason := event.Data["reason"].(string)

	fmt.Printf("   ❌ 支付失敗: %s, 原因: %s\n", orderID, reason)
	fmt.Printf("   🔄 開始補償流程...\n")

	// 補償步驟 1：釋放庫存
	s.inventoryService.Release(orderID)
	releaseEvent := &Event{
		AggregateID: orderID,
		Type:        "InventoryReleased",
		Data:        map[string]interface{}{},
		Timestamp:   time.Now(),
	}
	s.eventStore.Append(context.Background(), releaseEvent)
	fmt.Printf("   ✅ 庫存已釋放: Order %s\n", orderID)

	// 補償步驟 2：取消訂單
	cancelEvent := &Event{
		AggregateID: orderID,
		Type:        "OrderCancelled",
		Data: map[string]interface{}{
			"reason": "支付失敗",
		},
		Timestamp: time.Now(),
	}
	s.eventStore.Append(context.Background(), cancelEvent)
	fmt.Printf("   ✅ 訂單已取消: Order %s\n", orderID)

	return nil
}

// MockInventoryService 模擬庫存服務（教學用）
//
// 教學簡化：
//   - 實際應為獨立的微服務
//   - 有自己的資料庫、API
//   - 通過事件或 HTTP API 通信
type MockInventoryService struct {
	reserved map[string]bool
}

func NewMockInventoryService() *MockInventoryService {
	return &MockInventoryService{
		reserved: make(map[string]bool),
	}
}

// Reserve 預留庫存
func (s *MockInventoryService) Reserve(orderID string) error {
	// 模擬：10% 機率庫存不足
	if time.Now().UnixNano()%10 == 0 {
		return fmt.Errorf("庫存不足")
	}

	s.reserved[orderID] = true
	return nil
}

// Release 釋放庫存（補償操作）
func (s *MockInventoryService) Release(orderID string) {
	delete(s.reserved, orderID)
}

// MockPaymentService 模擬支付服務（教學用）
type MockPaymentService struct {
	charged map[string]bool
}

func NewMockPaymentService() *MockPaymentService {
	return &MockPaymentService{
		charged: make(map[string]bool),
	}
}

// Charge 扣款
func (s *MockPaymentService) Charge(orderID string) error {
	// 模擬：15% 機率支付失敗
	if time.Now().UnixNano()%7 == 0 {
		return fmt.Errorf("餘額不足")
	}

	s.charged[orderID] = true
	return nil
}

// Refund 退款（補償操作，教學簡化未使用）
func (s *MockPaymentService) Refund(orderID string) {
	delete(s.charged, orderID)
}
