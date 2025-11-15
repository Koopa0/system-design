package internal

import (
	"time"
)

// Config 任務調度器配置
type Config struct {
	// HTTP 服務配置
	HTTPPort string

	// NATS 連接配置
	NATSUrl string

	// 時間輪配置
	WheelConfig WheelConfig

	// 執行器配置
	ExecutorConfig ExecutorConfig
}

// WheelConfig 時間輪配置
type WheelConfig struct {
	// 槽位數量（3600 = 1小時精度，每秒一個槽位）
	SlotCount int

	// 指針轉動間隔
	TickDuration time.Duration
}

// ExecutorConfig 任務執行器配置
type ExecutorConfig struct {
	// HTTP 回調超時時間
	CallbackTimeout time.Duration

	// 最大重試次數
	MaxRetries int

	// 重試間隔基數（指數退避）
	RetryBaseDelay time.Duration
}

// DefaultConfig 返回預設配置
func DefaultConfig() *Config {
	return &Config{
		HTTPPort: "8081",
		NATSUrl:  "nats://localhost:4222",
		WheelConfig: WheelConfig{
			SlotCount:    3600,             // 3600 槽位 = 1 小時
			TickDuration: 1 * time.Second,  // 每秒轉動一次
		},
		ExecutorConfig: ExecutorConfig{
			CallbackTimeout: 30 * time.Second, // HTTP 回調 30 秒超時
			MaxRetries:      3,                // 最多重試 3 次
			RetryBaseDelay:  1 * time.Second,  // 1s → 2s → 4s（指數退避）
		},
	}
}
