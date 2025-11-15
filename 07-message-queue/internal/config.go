package internal

import (
	"time"
)

// Config 服務配置
type Config struct {
	// HTTP 服務配置
	HTTPPort string

	// NATS 連接配置
	NATSUrl string // NATS Server 地址（如 nats://localhost:4222）

	// JetStream 配置
	StreamConfig StreamConfig
}

// StreamConfig JetStream Stream 配置
type StreamConfig struct {
	// Stream 名稱
	Name string

	// Subject 列表（支持萬用字符，如 "order.*"）
	Subjects []string

	// 消息保留時間
	MaxAge time.Duration

	// 最大存儲大小（bytes）
	MaxBytes int64

	// 存儲類型（FileStorage 或 MemoryStorage）
	StorageType string
}

// DefaultConfig 返回預設配置
func DefaultConfig() *Config {
	return &Config{
		HTTPPort: "8080",
		NATSUrl:  "nats://localhost:4222",
		StreamConfig: StreamConfig{
			Name:        "ORDERS",
			Subjects:    []string{"order.*", "payment.*"},
			MaxAge:      7 * 24 * time.Hour, // 7 天
			MaxBytes:    10 * 1024 * 1024 * 1024, // 10 GB
			StorageType: "file", // file 或 memory
		},
	}
}
