package internal

// Config Event-Driven Architecture 配置
type Config struct {
	// HTTP 服務配置
	HTTPPort string

	// NATS 配置
	NATSUrl string

	// Event Store 配置
	EventStoreConfig EventStoreConfig

	// Projection 配置（CQRS Read Side）
	ProjectionConfig ProjectionConfig
}

// EventStoreConfig Event Store 配置
type EventStoreConfig struct {
	// Stream 名稱
	StreamName string

	// Subject 前綴（事件路由）
	SubjectPrefix string

	// 最大事件數（0 = 無限制）
	MaxEvents int64

	// 事件保留時間（0 = 永久保存）
	MaxAge int64
}

// ProjectionConfig Projection 配置
type ProjectionConfig struct {
	// Consumer 名稱
	ConsumerName string

	// 批次大小
	BatchSize int

	// 處理超時
	ProcessTimeout int
}

// DefaultConfig 返回默認配置
func DefaultConfig() *Config {
	return &Config{
		HTTPPort: "8082",
		NATSUrl:  "nats://localhost:4222",

		EventStoreConfig: EventStoreConfig{
			StreamName:    "ORDERS_EVENTS",
			SubjectPrefix: "orders",
			MaxEvents:     0,     // 無限制
			MaxAge:        0,     // 永久保存
		},

		ProjectionConfig: ProjectionConfig{
			ConsumerName:   "order-projection",
			BatchSize:      10,
			ProcessTimeout: 30, // 秒
		},
	}
}
