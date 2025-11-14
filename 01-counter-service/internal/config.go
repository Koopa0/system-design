package internal

import (
	"fmt"
	"os"
	"time"
)

// Config 整個應用的配置
type Config struct {
	Server struct {
		Port         int           `yaml:"port"`
		ReadTimeout  time.Duration `yaml:"read_timeout"`
		WriteTimeout time.Duration `yaml:"write_timeout"`
	} `yaml:"server"`

	Redis struct {
		Addr         string        `yaml:"addr"`
		Password     string        `yaml:"password"`
		DB           int           `yaml:"db"`
		PoolSize     int           `yaml:"pool_size"`
		MinIdleConns int           `yaml:"min_idle_conns"`
		MaxRetries   int           `yaml:"max_retries"`
		ReadTimeout  time.Duration `yaml:"read_timeout"`
		WriteTimeout time.Duration `yaml:"write_timeout"`
	} `yaml:"redis"`

	Postgres struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		User     string `yaml:"user"`
		Password string `yaml:"password"`
		DBName   string `yaml:"dbname"`
		MaxConns int32  `yaml:"max_conns"`
		MinConns int32  `yaml:"min_conns"`
	} `yaml:"postgres"`

	Counter struct {
		BatchSize         int           `yaml:"batch_size"`
		FlushInterval     time.Duration `yaml:"flush_interval"`
		EnableFallback    bool          `yaml:"enable_fallback"`
		FallbackThreshold int           `yaml:"fallback_threshold"`
		
		// DAU 計數模式配置
		DAUCountMode      string        `yaml:"dau_count_mode"`       // "exact" 或 "approximate"
		EnableMemoryCache bool          `yaml:"enable_memory_cache"`   // 降級時啟用記憶體快取
		CacheTTL          time.Duration `yaml:"cache_ttl"`             // 快取過期時間
		CacheSize         int           `yaml:"cache_size"`            // 快取大小（項目數）
	} `yaml:"counter"`

	Log struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
	} `yaml:"log"`
}

// PostgresDSN 生成 PostgreSQL 連線字串
func (c *Config) PostgresDSN() string {
	// 支援環境變數覆蓋（生產環境常用）
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn
	}

	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		c.Postgres.Host,
		c.Postgres.Port,
		c.Postgres.User,
		c.Postgres.Password,
		c.Postgres.DBName,
	)
}
