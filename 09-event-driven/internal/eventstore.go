// Package internal Event-Driven Architecture 核心實現
//
// Event Store（事件存儲）系統設計問題：
//
// 問題 1：為什麼需要 Event Store？
//   傳統方式：UPDATE orders SET status = 'completed'
//   問題：
//     - 只有最終狀態，無法知道如何到達
//     - 無法審計：誰在什麼時候改了什麼？
//     - 無法重現：出問題時難以 Debug
//
//   Event Sourcing 方式：存儲所有事件
//     events = [OrderCreated, OrderPaid, OrderShipped, OrderCompleted]
//   優勢：
//     - 完整歷史：所有變更可追溯
//     - 自然審計：事件即審計日誌
//     - 時間旅行：可查詢任意時間點的狀態
//     - Debug 友好：重放事件重現問題
//
// 問題 2：Event Store 如何選型？
//   方案 A：關聯式資料庫（PostgreSQL）
//     CREATE TABLE events (id, aggregate_id, type, data, timestamp)
//     問題：
//       - 寫入性能：每個事件一次 INSERT
//       - 查詢效率：重播需全表掃描
//       - 分區困難：難以水平擴展
//
//   方案 B：Kafka
//     優勢：高吞吐（百萬級 events/s）、持久化、重播
//     問題：重量級（需 ZooKeeper）、複雜度高
//
//   選擇：NATS JetStream（教學最佳選擇）
//     優勢：
//       - 輕量級：單一二進制，無外部依賴
//       - 完整功能：持久化、重播、分區
//       - 簡單易用：適合教學演示
//     適用：中小規模事件流（10K events/s）
//
// 問題 3：如何保證事件順序？
//   NATS Subject 命名：orders.{aggregate_id}.{event_type}
//   範例：orders.order-123.OrderCreated
//   保證：同一個 aggregate_id 的事件順序一致
//
package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// Event 事件結構
//
// 系統設計考量：
//   - AggregateID：聚合根 ID，保證事件順序（同一個 ID 的事件順序一致）
//   - Type：事件類型（OrderCreated, OrderPaid 等）
//   - Data：事件數據（JSON）
//   - Timestamp：事件時間戳（用於審計、重放）
//   - Version：事件版本（用於樂觀鎖、並發控制）
type Event struct {
	AggregateID string                 `json:"aggregate_id"` // 聚合根 ID（如 order-123）
	Type        string                 `json:"type"`         // 事件類型
	Data        map[string]interface{} `json:"data"`         // 事件數據
	Timestamp   time.Time              `json:"timestamp"`    // 事件時間
	Version     int                    `json:"version"`      // 事件版本
}

// EventStore 事件存儲
//
// 系統設計職責：
//   1. 追加事件（Append）：寫入新事件到 NATS Stream
//   2. 讀取事件（Load）：讀取指定 aggregate 的所有事件
//   3. 訂閱事件（Subscribe）：實時接收新事件（CQRS Read Side 使用）
//
// 為什麼使用 NATS JetStream：
//   - Stream：持久化事件流（類似 Kafka Topic）
//   - Subject：事件路由（orders.order-123.OrderCreated）
//   - Consumer：支持從頭重播（實現事件重放）
type EventStore struct {
	conn   *nats.Conn
	js     nats.JetStreamContext
	config EventStoreConfig
}

// NewEventStore 創建 Event Store
//
// 系統設計步驟：
//   1. 連接 NATS
//   2. 創建 JetStream Context
//   3. 創建或更新 Stream（持久化事件）
func NewEventStore(natsURL string, config EventStoreConfig) (*EventStore, error) {
	// 1. 連接 NATS
	conn, err := nats.Connect(natsURL)
	if err != nil {
		return nil, fmt.Errorf("連接 NATS 失敗: %w", err)
	}

	// 2. 創建 JetStream Context
	js, err := conn.JetStream()
	if err != nil {
		return nil, fmt.Errorf("創建 JetStream 失敗: %w", err)
	}

	es := &EventStore{
		conn:   conn,
		js:     js,
		config: config,
	}

	// 3. 初始化 Stream
	if err := es.initStream(); err != nil {
		return nil, err
	}

	return es, nil
}

// initStream 初始化 NATS Stream
//
// 系統設計考量：
//   - Subjects：orders.* （接收所有訂單事件）
//   - Storage：FileStorage（持久化到磁盤，重啟不丟失）
//   - Retention：LimitsPolicy（根據 MaxEvents/MaxAge 淘汰舊事件）
//   - Discard：DiscardOld（滿了淘汰舊事件，保證新事件寫入）
func (es *EventStore) initStream() error {
	streamConfig := &nats.StreamConfig{
		Name:      es.config.StreamName,                     // ORDERS_EVENTS
		Subjects:  []string{es.config.SubjectPrefix + ".*"}, // orders.*
		Storage:   nats.FileStorage,                         // 持久化
		Retention: nats.LimitsPolicy,
		MaxMsgs:   es.config.MaxEvents, // 0 = 無限制
		MaxAge:    time.Duration(es.config.MaxAge) * time.Second,
		Discard:   nats.DiscardOld,
	}

	// AddStream：如果 Stream 不存在則創建，存在則更新
	_, err := es.js.AddStream(streamConfig)
	if err != nil {
		return fmt.Errorf("創建 Stream 失敗: %w", err)
	}

	return nil
}

// Append 追加事件到 Event Store
//
// 系統設計流程：
//   1. 序列化事件為 JSON
//   2. 構建 Subject：orders.{aggregate_id}（保證順序）
//   3. 發布到 NATS JetStream（持久化）
//   4. 等待 ACK（確保寫入成功）
//
// 為什麼這樣設計：
//   - Subject 包含 aggregate_id：保證同一個訂單的事件順序
//   - 使用 PublishAsync：異步發布，提升性能
//   - 等待 ACK：確保事件已持久化（不丟失）
func (es *EventStore) Append(ctx context.Context, event *Event) error {
	// 1. 序列化事件
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("序列化事件失敗: %w", err)
	}

	// 2. 構建 Subject（保證同一個 aggregate 的事件順序）
	//    範例：orders.order-123
	subject := fmt.Sprintf("%s.%s", es.config.SubjectPrefix, event.AggregateID)

	// 3. 發布事件到 JetStream
	_, err = es.js.Publish(subject, data)
	if err != nil {
		return fmt.Errorf("發布事件失敗: %w", err)
	}

	return nil
}

// Load 讀取指定 aggregate 的所有事件
//
// 系統設計用途：
//   - Event Sourcing：從事件重建 Aggregate 狀態
//   - 事件重放：Debug、審計、狀態回溯
//
// 實現方式：
//   1. 訂閱 Subject：orders.{aggregate_id}
//   2. DeliverAll：從頭開始讀取（重放所有事件）
//   3. 收集所有消息直到沒有新消息
func (es *EventStore) Load(ctx context.Context, aggregateID string) ([]*Event, error) {
	subject := fmt.Sprintf("%s.%s", es.config.SubjectPrefix, aggregateID)

	// 創建臨時訂閱（從頭讀取）
	sub, err := es.js.SubscribeSync(subject, nats.DeliverAll())
	if err != nil {
		return nil, fmt.Errorf("訂閱失敗: %w", err)
	}
	defer sub.Unsubscribe()

	events := []*Event{}

	// 讀取所有事件（使用超時防止永久阻塞）
	for {
		msg, err := sub.NextMsgWithContext(ctx)
		if err == nats.ErrTimeout || err == context.DeadlineExceeded {
			break // 沒有更多消息
		}
		if err != nil {
			return nil, fmt.Errorf("讀取事件失敗: %w", err)
		}

		// 反序列化事件
		var event Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			return nil, fmt.Errorf("反序列化事件失敗: %w", err)
		}

		events = append(events, &event)
		msg.Ack()
	}

	return events, nil
}

// Subscribe 訂閱所有新事件（CQRS Read Side 使用）
//
// 系統設計用途：
//   - CQRS Read Side：訂閱事件更新 Read Model
//   - Saga 協調：訂閱事件觸發下一步流程
//
// 訂閱配置：
//   - DeliverAll：從頭讀取（重建 Read Model）
//   - Durable：持久化訂閱（重啟後繼續）
//   - ManualAck：手動確認（處理成功後才 ACK）
func (es *EventStore) Subscribe(ctx context.Context, handler func(*Event) error) error {
	subject := es.config.SubjectPrefix + ".*" // 訂閱所有訂單事件

	// 創建持久化訂閱
	_, err := es.js.Subscribe(subject, func(msg *nats.Msg) {
		// 反序列化事件
		var event Event
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			fmt.Printf("❌ 反序列化事件失敗: %v\n", err)
			return
		}

		// 調用處理器
		if err := handler(&event); err != nil {
			fmt.Printf("❌ 處理事件失敗: %v\n", err)
			// 不 ACK，NATS 會重試
			return
		}

		// 處理成功，確認消息
		msg.Ack()
	}, nats.Durable("order-subscriber"), nats.DeliverAll(), nats.ManualAck())

	if err != nil {
		return fmt.Errorf("訂閱失敗: %w", err)
	}

	// 等待上下文取消
	<-ctx.Done()
	return nil
}

// Close 關閉 Event Store
func (es *EventStore) Close() {
	if es.conn != nil {
		es.conn.Close()
	}
}
