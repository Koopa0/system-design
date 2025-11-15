// Package internal 實現基於 NATS JetStream 的消息隊列系統
//
// 系統設計問題：
//
//	如何實現輕量級、高性能的分布式消息隊列？
//
// 核心挑戰：
//  1. 如何保證消息不丟失？（At-least-once 語義）
//  2. 如何支持水平擴展？（Queue Groups）
//  3. 如何處理消費失敗？（重試機制）
//  4. 如何平衡性能與可靠性？（持久化策略）
//
// 設計方案：
//
//	✅ NATS JetStream：輕量級、高性能（100K+ msg/s）
//	✅ 磁盤持久化：重啟不丟失，7 天保留期
//	✅ 手動 ACK：消費成功才確認，失敗自動重試
//	✅ Queue Groups：自動負載均衡，線性擴展
//
// 為何選擇 NATS 而非 Kafka/RabbitMQ？
//
//  1. 輕量級：單一二進制檔案，Docker 一行啟動
//  2. 高性能：微秒級延遲 vs Kafka 的毫秒級
//  3. 簡單易用：API 直觀，學習曲線平緩
//  4. Go 原生：與專案語言一致，易於整合
//
// Trade-offs：
//
//  - 優勢：輕量、快速、易用
//  - 代價：生態較小、可能重複消費（需 Consumer 冪等性）
package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// MessageQueue 消息隊列核心實現
//
// 架構設計：
//
//	Publisher → NATS JetStream → Consumer
//	             ↓
//	        Persistent Storage
//	           (Disk)
//
// 系統設計考量：
//
//  1. 為什麼使用 JetStream 而非 Core NATS？
//     Core NATS：Fire-and-forget，無持久化
//     JetStream：持久化 + ACK + 重試，適合業務消息
//
//  2. 為什麼選擇 File Storage？
//     Memory：極致性能，但重啟丟失
//     File：持久化可靠，性能仍優秀（SSD 順序寫入）
//
//  3. 如何保證 At-least-once？
//     Publisher：同步發送，等待 PubAck
//     JetStream：磁盤持久化
//     Consumer：手動 ACK，未 ACK 自動重試
//
//  4. 消息保留策略？
//     MaxAge：7 天（平衡存儲成本與回溯需求）
//     MaxBytes：10 GB（防止磁盤爆滿）
//     超過限制：自動刪除最舊消息
type MessageQueue struct {
	conn *nats.Conn // NATS 連接
	js   nats.JetStreamContext // JetStream 上下文
	cfg  *Config // 配置
}

// Message 消息結構
type Message struct {
	Subject   string                 `json:"subject"`    // 消息主題（如 "order.created"）
	Data      map[string]interface{} `json:"data"`       // 消息內容
	Timestamp time.Time              `json:"timestamp"`  // 發送時間
}

// NewMessageQueue 創建消息隊列實例
//
// 系統設計重點：
//
//  1. 連接管理：
//     - 自動重連：斷線後自動重新連接
//     - 健康檢查：定期 ping 檢測連接狀態
//
//  2. Stream 初始化：
//     - 冪等性：重複調用不會創建多個 Stream
//     - 配置更新：Stream 已存在時更新配置
func NewMessageQueue(cfg *Config) (*MessageQueue, error) {
	// 1. 連接 NATS Server
	//    選項說明：
	//    - MaxReconnects(-1)：無限重連
	//    - ReconnectWait(1s)：重連間隔
	//    - PingInterval(20s)：心跳檢測
	conn, err := nats.Connect(
		cfg.NATSUrl,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
		nats.PingInterval(20*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("連接 NATS 失敗: %w", err)
	}

	// 2. 創建 JetStream 上下文
	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("創建 JetStream 上下文失敗: %w", err)
	}

	mq := &MessageQueue{
		conn: conn,
		js:   js,
		cfg:  cfg,
	}

	// 3. 初始化 Stream
	if err := mq.initStream(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("初始化 Stream 失敗: %w", err)
	}

	return mq, nil
}

// initStream 初始化 JetStream Stream
//
// 系統設計重點：
//
//  1. Stream 配置：
//     - Subjects：定義接收哪些主題的消息（支持萬用字符）
//     - Storage：FileStorage（持久化）或 MemoryStorage（高性能）
//     - MaxAge：消息保留時間（超過自動刪除）
//     - MaxBytes：存儲上限（防止磁盤爆滿）
//     - Replicas：副本數（叢集模式，教學簡化為 1）
//
//  2. 冪等性：
//     - StreamInfo()：檢查 Stream 是否已存在
//     - AddStream()：不存在則創建
//     - UpdateStream()：已存在則更新配置
func (mq *MessageQueue) initStream() error {
	streamCfg := mq.cfg.StreamConfig

	// 確定存儲類型
	var storage nats.StorageType
	if streamCfg.StorageType == "memory" {
		storage = nats.MemoryStorage
	} else {
		storage = nats.FileStorage
	}

	// Stream 配置
	cfg := &nats.StreamConfig{
		Name:     streamCfg.Name,
		Subjects: streamCfg.Subjects,
		Storage:  storage,
		MaxAge:   streamCfg.MaxAge,
		MaxBytes: streamCfg.MaxBytes,

		// 教學簡化：單副本
		// 生產環境建議：Replicas: 3（高可用）
		Replicas: 1,

		// 消息去重（基於 Message ID）
		// 教學簡化：未啟用
		// 生產環境可啟用：Duplicates: 2 * time.Minute
	}

	// 檢查 Stream 是否已存在
	_, err := mq.js.StreamInfo(streamCfg.Name)
	if err == nats.ErrStreamNotFound {
		// 不存在，創建新 Stream
		_, err = mq.js.AddStream(cfg)
		if err != nil {
			return fmt.Errorf("創建 Stream 失敗: %w", err)
		}
		return nil
	} else if err != nil {
		return fmt.Errorf("查詢 Stream 失敗: %w", err)
	}

	// 已存在，更新配置
	_, err = mq.js.UpdateStream(cfg)
	if err != nil {
		return fmt.Errorf("更新 Stream 失敗: %w", err)
	}

	return nil
}

// Publish 發送消息（同步，At-least-once 語義）
//
// 系統設計重點：
//
//  1. 同步發送 vs 異步發送：
//     - 同步：等待 PubAck，確保消息已持久化
//     - 異步：立即返回，更高吞吐但可能丟失
//     選擇：同步（可靠性優先）
//
//  2. 發送流程：
//     T1: 序列化消息
//     T2: js.Publish() 發送
//     T3: JetStream 寫入磁盤
//     T4: 返回 PubAck（包含 Sequence 序號）
//     T5: Publisher 確認成功
//
//  3. 錯誤處理：
//     - 連接斷開：NATS 自動重連後重試
//     - 超時：返回錯誤，由調用方決定重試策略
//
//  4. 性能優化（教學簡化）：
//     - 當前：單條發送（簡單易懂）
//     - 生產環境可優化：批量發送（PublishAsync + 批量 flush）
func (mq *MessageQueue) Publish(ctx context.Context, msg *Message) (*nats.PubAck, error) {
	// 1. 添加時間戳
	msg.Timestamp = time.Now()

	// 2. 序列化消息
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return nil, fmt.Errorf("序列化消息失敗: %w", err)
	}

	// 3. 同步發送，等待 ACK
	//    選項說明：
	//    - Context：支持超時控制
	//    - Subject：消息路由鍵
	//    - Data：消息內容（JSON 格式）
	pubAck, err := mq.js.Publish(msg.Subject, data, nats.Context(ctx))
	if err != nil {
		return nil, fmt.Errorf("發送消息失敗: %w", err)
	}

	// 4. 返回發送確認
	//    PubAck 包含：
	//    - Stream：消息所在 Stream 名稱
	//    - Sequence：消息序號（全局唯一，遞增）
	//    - Duplicate：是否為重複消息
	return pubAck, nil
}

// Subscribe 訂閱消息（At-least-once 語義，手動 ACK）
//
// 系統設計重點：
//
//  1. 消費者模型：
//     - Push：JetStream 主動推送消息
//     - Pull：Consumer 主動拉取（更可控）
//     選擇：Push（簡單易用）
//
//  2. ACK 模式：
//     - AckExplicit：手動 ACK（最安全）
//     - AckAll：確認到當前消息為止的所有消息
//     - AckNone：不需要 ACK（Fire-and-forget）
//     選擇：AckExplicit（保證 At-least-once）
//
//  3. 重試機制：
//     - MaxDeliver：最大投遞次數（-1 為無限）
//     - AckWait：等待 ACK 的超時時間
//     - 超時未 ACK：自動重新投遞
//
//  4. 訂閱方式：
//     - Subscribe：獨立訂閱（每個 Consumer 收到所有消息）
//     - QueueSubscribe：Queue Group（負載均衡）
//     本函數：獨立訂閱（廣播模式）
func (mq *MessageQueue) Subscribe(subject, consumerName string, handler nats.MsgHandler) (*nats.Subscription, error) {
	// Consumer 配置
	//
	// 系統設計考量：
	//  - Durable：持久化 Consumer（重啟後繼續消費）
	//  - AckExplicit：手動 ACK
	//  - AckWait：30 秒超時（可根據業務調整）
	//  - MaxDeliver：-1 無限重試（教學簡化）
	//
	// 生產環境建議：
	//  - MaxDeliver：5-10 次（避免無限重試）
	//  - DeliverPolicy：DeliverAll（從頭消費）或 DeliverNew（只消費新消息）
	sub, err := mq.js.Subscribe(
		subject,
		handler,
		nats.Durable(consumerName),
		nats.ManualAck(), // 手動 ACK
		nats.AckWait(30*time.Second), // 30 秒超時
		nats.MaxDeliver(-1), // 無限重試
	)
	if err != nil {
		return nil, fmt.Errorf("訂閱失敗: %w", err)
	}

	return sub, nil
}

// QueueSubscribe 訂閱消息（Queue Group 模式，負載均衡）
//
// 系統設計重點：
//
//  1. Queue Groups 原理：
//     - 多個 Consumer 加入同一個 Queue Group
//     - JetStream 自動負載均衡（Round-Robin）
//     - 每條消息只被一個 Consumer 處理
//
//  2. 負載均衡策略：
//     - Round-Robin：輪流分配（簡單公平）
//     - Random：隨機分配
//     - 自定義：可實現權重、一致性哈希等
//
//  3. 容錯機制：
//     - Consumer 崩潰：未 ACK 的消息自動分配給其他 Consumer
//     - Consumer 加入：自動參與負載均衡
//     - Consumer 離開：消息重新分配
//
//  4. 擴展性：
//     - 1 Consumer → 10K msg/s
//     - 5 Consumers → 50K msg/s
//     - 線性擴展 ✅
//
// 範例：
//
//	Queue Group "order-processor"
//	├─ Consumer 1 ──> 處理消息 1, 4, 7...
//	├─ Consumer 2 ──> 處理消息 2, 5, 8...
//	└─ Consumer 3 ──> 處理消息 3, 6, 9...
func (mq *MessageQueue) QueueSubscribe(subject, queueGroup, consumerName string, handler nats.MsgHandler) (*nats.Subscription, error) {
	// Queue Subscribe 配置
	//
	// 與普通 Subscribe 的差異：
	//  - 多了 queueGroup 參數
	//  - JetStream 自動在同組 Consumer 間負載均衡
	sub, err := mq.js.QueueSubscribe(
		subject,
		queueGroup,
		handler,
		nats.Durable(consumerName),
		nats.ManualAck(),
		nats.AckWait(30*time.Second),
		nats.MaxDeliver(-1),
	)
	if err != nil {
		return nil, fmt.Errorf("Queue Subscribe 失敗: %w", err)
	}

	return sub, nil
}

// GetStreamInfo 獲取 Stream 狀態信息
//
// 返回信息：
//  - Messages：消息總數
//  - Bytes：存儲大小
//  - FirstSeq/LastSeq：消息序號範圍
//  - Consumers：消費者數量
func (mq *MessageQueue) GetStreamInfo() (*nats.StreamInfo, error) {
	info, err := mq.js.StreamInfo(mq.cfg.StreamConfig.Name)
	if err != nil {
		return nil, fmt.Errorf("獲取 Stream 信息失敗: %w", err)
	}
	return info, nil
}

// GetConsumerInfo 獲取 Consumer 狀態信息
//
// 返回信息：
//  - NumPending：待處理消息數
//  - NumAckPending：已發送但未 ACK 的消息數
//  - NumRedelivered：重新投遞的消息數
//  - Delivered：已投遞的消息序號
func (mq *MessageQueue) GetConsumerInfo(consumerName string) (*nats.ConsumerInfo, error) {
	info, err := mq.js.ConsumerInfo(mq.cfg.StreamConfig.Name, consumerName)
	if err != nil {
		return nil, fmt.Errorf("獲取 Consumer 信息失敗: %w", err)
	}
	return info, nil
}

// Close 關閉連接
func (mq *MessageQueue) Close() {
	if mq.conn != nil {
		mq.conn.Close()
	}
}
