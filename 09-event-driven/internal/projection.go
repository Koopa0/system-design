package internal

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Projection CQRS Read Side（查詢端）
//
// 系統設計問題：為什麼需要 CQRS？
//
// 問題：Event Sourcing 查詢性能問題
//   查詢訂單狀態：
//     1. 從 Event Store 讀取所有事件
//     2. 重放事件重建狀態
//     3. 返回結果
//   問題：
//     - 性能差：每次查詢都要重放事件
//     - 複雜查詢困難：無法 JOIN、聚合
//     - 無法優化：Event Store 是 append-only
//
// CQRS 解決方案：讀寫分離
//
//   Write Side（命令端）：
//     Command → Aggregate → Event → Event Store
//     優化：快速寫入（append-only）
//
//   Read Side（查詢端）：
//     Event Store → Event Handler → Read Model（如 PostgreSQL）
//     優化：快速查詢（索引、JOIN、聚合）
//
// 架構流程：
//   1. Write Side 寫入事件到 Event Store
//   2. Projection 訂閱事件
//   3. Event Handler 更新 Read Model
//   4. 查詢從 Read Model 讀取（不需重放事件）
//
// 權衡：
//   - 最終一致性：Read Model 有延遲（10-50ms）
//   - 複雜度：維護兩個模型（Write Model + Read Model）
//
// 適用場景：
//   - ✅ 讀多寫少（查詢 >> 命令）
//   - ✅ 複雜查詢（JOIN、聚合、報表）
//   - ✅ 可接受最終一致性
//
// 不適用：
//   - ❌ 強一致性要求（讀寫必須同步）
//   - ❌ 簡單 CRUD（不需要讀寫分離）

// OrderReadModel 訂單讀模型（查詢優化）
//
// 系統設計特點：
//   - 非規範化（Denormalized）：包含所有查詢需要的數據
//   - 查詢優化：支持索引、JOIN、聚合
//   - 最終一致性：從事件異步更新
//
// 對比 Write Model（OrderAggregate）：
//   - Write Model：業務邏輯、狀態轉換
//   - Read Model：查詢優化、數據展示
//
// 生產環境：
//   - 存儲：PostgreSQL, MongoDB, Elasticsearch
//   - 索引：order_id, user_id, status, created_at
//   - 物化視圖：複雜聚合查詢
type OrderReadModel struct {
	OrderID   string      `json:"order_id"`
	UserID    int         `json:"user_id"`
	Amount    float64     `json:"amount"`
	Status    string      `json:"status"`
	Items     []OrderItem `json:"items"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`

	// 事件歷史（審計、追溯）
	Events []EventSummary `json:"events"`
}

// EventSummary 事件摘要（用於顯示事件歷史）
type EventSummary struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
}

// OrderProjection 訂單投影（CQRS Read Side）
//
// 系統設計職責：
//   1. 訂閱事件：監聽 Event Store 的新事件
//   2. 更新 Read Model：根據事件類型更新查詢模型
//   3. 提供查詢接口：GetOrder, ListOrders
//
// 為什麼叫 "Projection"：
//   - 將事件 "投影" 到查詢模型
//   - 類似數據庫的物化視圖（Materialized View）
//
// 實現方式：
//   - 教學版：內存存儲（map）
//   - 生產環境：PostgreSQL, MongoDB, Elasticsearch
type OrderProjection struct {
	// 教學簡化：內存存儲
	// 生產環境：PostgreSQL、MongoDB
	orders map[string]*OrderReadModel
	mu     sync.RWMutex

	eventStore *EventStore
}

// NewOrderProjection 創建訂單投影
func NewOrderProjection(eventStore *EventStore) *OrderProjection {
	return &OrderProjection{
		orders:     make(map[string]*OrderReadModel),
		eventStore: eventStore,
	}
}

// Start 啟動投影（訂閱事件）
//
// 系統設計流程：
//   1. 訂閱 Event Store 的所有訂單事件
//   2. 收到事件時調用 HandleEvent
//   3. 更新 Read Model
//
// DeliverAll：
//   - 從頭重放所有事件（重建 Read Model）
//   - 啟動時確保 Read Model 是最新的
//
// Durable Consumer：
//   - 持久化訂閱位置（記住處理到哪裡）
//   - 重啟後繼續處理（不重複處理）
func (p *OrderProjection) Start() error {
	// 訂閱所有訂單事件
	go p.eventStore.Subscribe(context.Background(), p.HandleEvent)
	return nil
}

// HandleEvent 處理事件（更新 Read Model）
//
// 系統設計模式：Event Handler
//
// 流程：
//   1. 根據事件類型分發
//   2. 更新對應的 Read Model
//   3. 記錄事件歷史（審計）
//
// 為什麼每個事件都要處理：
//   - 保持 Read Model 與 Event Store 同步
//   - 記錄完整事件歷史
//   - 支持複雜查詢（如按狀態查詢）
func (p *OrderProjection) HandleEvent(event *Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 獲取或創建 Read Model
	order, exists := p.orders[event.AggregateID]
	if !exists {
		order = &OrderReadModel{
			OrderID: event.AggregateID,
			Events:  []EventSummary{},
		}
		p.orders[event.AggregateID] = order
	}

	// 根據事件類型更新 Read Model
	switch event.Type {
	case "OrderCreated":
		order.UserID = int(event.Data["user_id"].(float64))
		order.Amount = event.Data["amount"].(float64)
		order.Status = "created"
		order.CreatedAt = event.Timestamp

		// 處理訂單項目
		if items, ok := event.Data["items"].([]interface{}); ok {
			order.Items = []OrderItem{}
			for _, item := range items {
				itemMap := item.(map[string]interface{})
				order.Items = append(order.Items, OrderItem{
					ProductID: int(itemMap["product_id"].(float64)),
					Quantity:  int(itemMap["quantity"].(float64)),
				})
			}
		}

	case "OrderPaid":
		order.Status = "paid"

	case "OrderShipped":
		order.Status = "shipped"

	case "OrderCompleted":
		order.Status = "completed"

	case "OrderCancelled":
		order.Status = "cancelled"
	}

	// 記錄事件歷史（審計）
	order.Events = append(order.Events, EventSummary{
		Type:      event.Type,
		Timestamp: event.Timestamp,
	})
	order.UpdatedAt = event.Timestamp

	fmt.Printf("📊 Read Model 已更新: Order %s → %s\n", order.OrderID, order.Status)

	return nil
}

// GetOrder 查詢訂單（從 Read Model）
//
// 系統設計優勢：
//   - O(1) 查詢：直接從 map 讀取（生產環境從資料庫索引讀取）
//   - 無需重放：Read Model 已包含最新狀態
//   - 支持複雜查詢：可加入更多字段、索引
//
// 對比 Event Sourcing 查詢：
//   - Event Sourcing：Load events → Replay → Build state
//   - CQRS：Direct query from Read Model
func (p *OrderProjection) GetOrder(orderID string) (*OrderReadModel, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	order, exists := p.orders[orderID]
	if !exists {
		return nil, fmt.Errorf("訂單不存在: %s", orderID)
	}

	return order, nil
}

// ListOrders 列出所有訂單
//
// 系統設計擴展：
//   - 生產環境可加入：
//     * 分頁：LIMIT/OFFSET
//     * 篩選：WHERE status = 'paid'
//     * 排序：ORDER BY created_at DESC
//     * 聚合：COUNT, SUM, AVG
func (p *OrderProjection) ListOrders() []*OrderReadModel {
	p.mu.RLock()
	defer p.mu.RUnlock()

	orders := make([]*OrderReadModel, 0, len(p.orders))
	for _, order := range p.orders {
		orders = append(orders, order)
	}

	return orders
}

// ListOrdersByStatus 按狀態查詢訂單
//
// 系統設計示範：CQRS 的查詢優勢
//
// Event Sourcing 方式：
//   - 讀取所有訂單的所有事件
//   - 重放每個訂單的事件
//   - 過濾出指定狀態
//   - 性能：O(N * M)，N=訂單數, M=平均事件數
//
// CQRS 方式：
//   - 直接查詢 Read Model
//   - 過濾 status 字段
//   - 性能：O(N)（生產環境可用索引優化到 O(log N)）
func (p *OrderProjection) ListOrdersByStatus(status string) []*OrderReadModel {
	p.mu.RLock()
	defer p.mu.RUnlock()

	orders := []*OrderReadModel{}
	for _, order := range p.orders {
		if order.Status == status {
			orders = append(orders, order)
		}
	}

	return orders
}

// GetStatistics 統計信息（CQRS 的聚合查詢優勢）
//
// 系統設計示範：
//   - CQRS Read Model 可以預計算聚合數據
//   - 生產環境可用物化視圖、定時任務更新
//   - 避免每次查詢都掃描所有數據
type OrderStatistics struct {
	TotalOrders     int     `json:"total_orders"`
	TotalAmount     float64 `json:"total_amount"`
	CompletedOrders int     `json:"completed_orders"`
	PendingOrders   int     `json:"pending_orders"`
}

// GetStatistics 獲取統計信息
func (p *OrderProjection) GetStatistics() *OrderStatistics {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := &OrderStatistics{}

	for _, order := range p.orders {
		stats.TotalOrders++
		stats.TotalAmount += order.Amount

		switch order.Status {
		case "completed":
			stats.CompletedOrders++
		case "created", "paid", "shipped":
			stats.PendingOrders++
		}
	}

	return stats
}
