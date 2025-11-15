package internal

import (
	"context"
	"fmt"
	"time"
)

// Aggregate 聚合根（Command Side - 寫端）
//
// 系統設計問題：為什麼需要 Aggregate？
//
// 問題：如何保證業務邏輯一致性？
//   傳統方式：直接操作資料庫
//     UPDATE orders SET status = 'paid' WHERE id = 123
//   問題：
//     - 業務邏輯分散：驗證、狀態轉換散落各處
//     - 一致性難保證：多個地方修改同一個訂單
//     - 無法追蹤歷史：不知道訂單如何到達當前狀態
//
// Aggregate 解決方案：
//   - 所有狀態變更通過事件：Order.Pay() → OrderPaid event
//   - 業務邏輯集中：所有驗證、狀態轉換在 Aggregate 內
//   - 事件溯源：從事件重建狀態
//
// DDD 術語：
//   - Aggregate Root：聚合根（Order）
//   - Entity：實體（訂單本身）
//   - Value Object：值對象（OrderItem）
//   - Domain Event：領域事件（OrderCreated, OrderPaid）
//
// 核心方法：
//   - ExecuteCommand：執行命令（如 CreateOrder）
//   - ApplyEvent：應用事件（重建狀態）
//   - GetUncommittedEvents：獲取未提交事件（待保存）

// OrderAggregate 訂單聚合根
//
// 系統設計職責：
//   1. 接收命令（Command）：CreateOrder, PayOrder, ShipOrder
//   2. 執行業務邏輯：驗證、狀態轉換
//   3. 產生事件（Event）：OrderCreated, OrderPaid, OrderShipped
//   4. 應用事件（Apply）：更新內部狀態
//
// 為什麼這樣設計：
//   - Command/Event 分離：命令可能失敗，事件已發生（不可逆）
//   - 事件驅動：所有狀態變更通過事件
//   - 可重放：從事件重建 Aggregate 狀態
type OrderAggregate struct {
	// 聚合根 ID
	ID string

	// 當前狀態（從事件重建）
	UserID    int
	Amount    float64
	Status    string // created, paid, shipped, completed, cancelled
	Items     []OrderItem
	CreatedAt time.Time
	UpdatedAt time.Time

	// 版本號（樂觀鎖，防止並發衝突）
	Version int

	// 未提交的事件（待保存到 Event Store）
	uncommittedEvents []*Event
}

// OrderItem 訂單項目（Value Object）
type OrderItem struct {
	ProductID int `json:"product_id"`
	Quantity  int `json:"quantity"`
}

// NewOrderAggregate 創建新的訂單聚合根
func NewOrderAggregate(id string) *OrderAggregate {
	return &OrderAggregate{
		ID:                id,
		Status:            "created",
		Version:           0,
		uncommittedEvents: []*Event{},
	}
}

// CreateOrder 創建訂單（Command Handler）
//
// 系統設計流程：
//   1. 驗證業務規則（金額 > 0）
//   2. 產生 OrderCreated 事件
//   3. 應用事件更新狀態
//   4. 將事件加入未提交列表（等待保存）
//
// 為什麼不直接修改狀態：
//   - Event Sourcing：所有狀態變更必須通過事件
//   - 可追溯：事件即歷史記錄
//   - 可重放：從事件重建狀態
func (o *OrderAggregate) CreateOrder(userID int, amount float64, items []OrderItem) error {
	// 1. 業務驗證
	if amount <= 0 {
		return fmt.Errorf("訂單金額必須大於 0")
	}
	if len(items) == 0 {
		return fmt.Errorf("訂單必須包含至少一個商品")
	}

	// 2. 產生事件
	event := &Event{
		AggregateID: o.ID,
		Type:        "OrderCreated",
		Data: map[string]interface{}{
			"user_id": userID,
			"amount":  amount,
			"items":   items,
		},
		Timestamp: time.Now(),
		Version:   o.Version + 1,
	}

	// 3. 應用事件（更新內部狀態）
	o.ApplyEvent(event)

	// 4. 加入未提交事件列表
	o.uncommittedEvents = append(o.uncommittedEvents, event)

	return nil
}

// PayOrder 支付訂單（Command Handler）
//
// 業務規則：
//   - 只有 created 狀態的訂單可以支付
//   - 支付成功產生 OrderPaid 事件
func (o *OrderAggregate) PayOrder() error {
	// 驗證狀態
	if o.Status != "created" {
		return fmt.Errorf("只有待支付訂單可以支付，當前狀態: %s", o.Status)
	}

	event := &Event{
		AggregateID: o.ID,
		Type:        "OrderPaid",
		Data: map[string]interface{}{
			"paid_at": time.Now(),
		},
		Timestamp: time.Now(),
		Version:   o.Version + 1,
	}

	o.ApplyEvent(event)
	o.uncommittedEvents = append(o.uncommittedEvents, event)

	return nil
}

// ShipOrder 發貨（Command Handler）
func (o *OrderAggregate) ShipOrder(trackingNumber string) error {
	if o.Status != "paid" {
		return fmt.Errorf("只有已支付訂單可以發貨，當前狀態: %s", o.Status)
	}

	event := &Event{
		AggregateID: o.ID,
		Type:        "OrderShipped",
		Data: map[string]interface{}{
			"tracking_number": trackingNumber,
		},
		Timestamp: time.Now(),
		Version:   o.Version + 1,
	}

	o.ApplyEvent(event)
	o.uncommittedEvents = append(o.uncommittedEvents, event)

	return nil
}

// CompleteOrder 完成訂單
func (o *OrderAggregate) CompleteOrder() error {
	if o.Status != "shipped" {
		return fmt.Errorf("只有已發貨訂單可以完成，當前狀態: %s", o.Status)
	}

	event := &Event{
		AggregateID: o.ID,
		Type:        "OrderCompleted",
		Data:        map[string]interface{}{},
		Timestamp:   time.Now(),
		Version:     o.Version + 1,
	}

	o.ApplyEvent(event)
	o.uncommittedEvents = append(o.uncommittedEvents, event)

	return nil
}

// ApplyEvent 應用事件（更新狀態）
//
// 系統設計核心：Event Sourcing 的狀態重建
//
// 為什麼需要 Apply：
//   - 從事件流重建 Aggregate 狀態
//   - 保證狀態轉換的一致性
//   - 支持事件重放（Debug、審計）
//
// 設計模式：Event Dispatcher
//   - 根據事件類型分發到不同的處理方法
//   - 每個事件類型對應一個狀態轉換
func (o *OrderAggregate) ApplyEvent(event *Event) {
	switch event.Type {
	case "OrderCreated":
		o.UserID = int(event.Data["user_id"].(float64))
		o.Amount = event.Data["amount"].(float64)
		o.Status = "created"
		o.CreatedAt = event.Timestamp

		// 處理 items（JSON 反序列化）
		if items, ok := event.Data["items"].([]interface{}); ok {
			o.Items = []OrderItem{}
			for _, item := range items {
				itemMap := item.(map[string]interface{})
				o.Items = append(o.Items, OrderItem{
					ProductID: int(itemMap["product_id"].(float64)),
					Quantity:  int(itemMap["quantity"].(float64)),
				})
			}
		}

	case "OrderPaid":
		o.Status = "paid"

	case "OrderShipped":
		o.Status = "shipped"

	case "OrderCompleted":
		o.Status = "completed"

	case "OrderCancelled":
		o.Status = "cancelled"
	}

	// 更新版本號
	o.Version = event.Version
	o.UpdatedAt = event.Timestamp
}

// GetUncommittedEvents 獲取未提交的事件
//
// 系統設計用途：
//   - 保存到 Event Store：將產生的事件持久化
//   - 發布到 Message Bus：通知其他服務（CQRS Read Side、Saga）
func (o *OrderAggregate) GetUncommittedEvents() []*Event {
	return o.uncommittedEvents
}

// ClearUncommittedEvents 清空未提交事件（保存後調用）
func (o *OrderAggregate) ClearUncommittedEvents() {
	o.uncommittedEvents = []*Event{}
}

// LoadFromEvents 從事件流重建 Aggregate
//
// 系統設計用途：
//   - Event Sourcing：從 Event Store 讀取事件重建狀態
//   - 事件重放：Debug、審計、時間旅行
//
// 實現：
//   - 依序應用每個事件
//   - 最終狀態 = 初始狀態 + 所有事件
func (o *OrderAggregate) LoadFromEvents(events []*Event) {
	for _, event := range events {
		o.ApplyEvent(event)
	}
}

// OrderRepository 訂單倉儲（連接 Aggregate 與 Event Store）
//
// 系統設計職責：
//   1. Save：保存 Aggregate 產生的事件到 Event Store
//   2. Load：從 Event Store 讀取事件重建 Aggregate
//
// 為什麼需要 Repository：
//   - 分離關注點：Aggregate 不關心持久化細節
//   - 統一接口：屏蔽 Event Store 實現細節
type OrderRepository struct {
	eventStore *EventStore
}

// NewOrderRepository 創建訂單倉儲
func NewOrderRepository(eventStore *EventStore) *OrderRepository {
	return &OrderRepository{
		eventStore: eventStore,
	}
}

// Save 保存訂單（將未提交事件保存到 Event Store）
func (r *OrderRepository) Save(aggregate *OrderAggregate) error {
	events := aggregate.GetUncommittedEvents()
	if len(events) == 0 {
		return nil // 沒有變更
	}

	// 保存所有事件
	for _, event := range events {
		if err := r.eventStore.Append(context.Background(), event); err != nil {
			return fmt.Errorf("保存事件失敗: %w", err)
		}
	}

	// 清空未提交事件
	aggregate.ClearUncommittedEvents()

	return nil
}

// Load 加載訂單（從 Event Store 讀取事件重建）
func (r *OrderRepository) Load(aggregateID string) (*OrderAggregate, error) {
	// 從 Event Store 讀取事件
	events, err := r.eventStore.Load(context.Background(), aggregateID)
	if err != nil {
		return nil, fmt.Errorf("讀取事件失敗: %w", err)
	}

	if len(events) == 0 {
		return nil, fmt.Errorf("訂單不存在: %s", aggregateID)
	}

	// 重建 Aggregate
	aggregate := NewOrderAggregate(aggregateID)
	aggregate.LoadFromEvents(events)

	return aggregate, nil
}
