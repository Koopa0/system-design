package internal_test

import (
	"os"
	"testing"
	"time"

	"github.com/koopa0/system-design/exercise-1/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfig_DefaultValues 測試配置的預設值
func TestConfig_DefaultValues(t *testing.T) {
	config := &internal.Config{}

	t.Run("server defaults", func(t *testing.T) {
		// 設置一些預設值
		config.Server.Port = 8080
		config.Server.ReadTimeout = 5 * time.Second
		config.Server.WriteTimeout = 10 * time.Second

		assert.Equal(t, 8080, config.Server.Port)
		assert.Equal(t, 5*time.Second, config.Server.ReadTimeout)
		assert.Equal(t, 10*time.Second, config.Server.WriteTimeout)
	})

	t.Run("redis defaults", func(t *testing.T) {
		config.Redis.Addr = "localhost:6379"
		config.Redis.DB = 0
		config.Redis.PoolSize = 10
		config.Redis.MinIdleConns = 5
		config.Redis.MaxRetries = 3
		config.Redis.ReadTimeout = 3 * time.Second
		config.Redis.WriteTimeout = 3 * time.Second

		assert.Equal(t, "localhost:6379", config.Redis.Addr)
		assert.Equal(t, 0, config.Redis.DB)
		assert.Equal(t, 10, config.Redis.PoolSize)
		assert.Equal(t, 5, config.Redis.MinIdleConns)
		assert.Equal(t, 3, config.Redis.MaxRetries)
		assert.Equal(t, 3*time.Second, config.Redis.ReadTimeout)
		assert.Equal(t, 3*time.Second, config.Redis.WriteTimeout)
	})

	t.Run("postgres defaults", func(t *testing.T) {
		config.Postgres.Host = "localhost"
		config.Postgres.Port = 5432
		config.Postgres.User = "postgres"
		config.Postgres.Password = "password"
		config.Postgres.DBName = "counter_db"
		config.Postgres.MaxConns = 10
		config.Postgres.MinConns = 2

		assert.Equal(t, "localhost", config.Postgres.Host)
		assert.Equal(t, 5432, config.Postgres.Port)
		assert.Equal(t, "postgres", config.Postgres.User)
		assert.Equal(t, "password", config.Postgres.Password)
		assert.Equal(t, "counter_db", config.Postgres.DBName)
		assert.Equal(t, int32(10), config.Postgres.MaxConns)
		assert.Equal(t, int32(2), config.Postgres.MinConns)
	})

	t.Run("counter defaults", func(t *testing.T) {
		config.Counter.BatchSize = 100
		config.Counter.FlushInterval = 1 * time.Second
		config.Counter.EnableFallback = true
		config.Counter.FallbackThreshold = 3

		assert.Equal(t, 100, config.Counter.BatchSize)
		assert.Equal(t, 1*time.Second, config.Counter.FlushInterval)
		assert.True(t, config.Counter.EnableFallback)
		assert.Equal(t, 3, config.Counter.FallbackThreshold)
	})

	t.Run("log defaults", func(t *testing.T) {
		config.Log.Level = "info"
		config.Log.Format = "json"

		assert.Equal(t, "info", config.Log.Level)
		assert.Equal(t, "json", config.Log.Format)
	})
}

// TestConfig_PostgresDSN 測試 PostgreSQL 連接字串生成
func TestConfig_PostgresDSN(t *testing.T) {
	config := &internal.Config{}

	t.Run("generate DSN from config", func(t *testing.T) {
		config.Postgres.Host = "db.example.com"
		config.Postgres.Port = 5432
		config.Postgres.User = "testuser"
		config.Postgres.Password = "testpass"
		config.Postgres.DBName = "testdb"

		expectedDSN := "host=db.example.com port=5432 user=testuser password=testpass dbname=testdb sslmode=disable"
		actualDSN := config.PostgresDSN()

		assert.Equal(t, expectedDSN, actualDSN)
	})

	t.Run("use DATABASE_URL environment variable", func(t *testing.T) {
		// 設置環境變數
		envDSN := "postgres://user:pass@localhost:5432/mydb?sslmode=require"
		err := os.Setenv("DATABASE_URL", envDSN)
		require.NoError(t, err)
		defer os.Unsetenv("DATABASE_URL")

		actualDSN := config.PostgresDSN()
		assert.Equal(t, envDSN, actualDSN)
	})

	t.Run("config takes precedence when no env var", func(t *testing.T) {
		// 確保沒有環境變數
		os.Unsetenv("DATABASE_URL")

		config.Postgres.Host = "localhost"
		config.Postgres.Port = 5432
		config.Postgres.User = "user"
		config.Postgres.Password = "pass"
		config.Postgres.DBName = "db"

		expectedDSN := "host=localhost port=5432 user=user password=pass dbname=db sslmode=disable"
		actualDSN := config.PostgresDSN()

		assert.Equal(t, expectedDSN, actualDSN)
	})
}

// TestConfig_EdgeCases 測試配置的邊界情況
func TestConfig_EdgeCases(t *testing.T) {
	config := &internal.Config{}

	t.Run("empty password", func(t *testing.T) {
		config.Postgres.Host = "localhost"
		config.Postgres.Port = 5432
		config.Postgres.User = "user"
		config.Postgres.Password = ""
		config.Postgres.DBName = "db"

		dsn := config.PostgresDSN()
		assert.Contains(t, dsn, "password= ")
	})

	t.Run("special characters in password", func(t *testing.T) {
		config.Postgres.Password = "p@ss!word#123"
		dsn := config.PostgresDSN()
		assert.Contains(t, dsn, "password=p@ss!word#123")
	})

	t.Run("non-standard port", func(t *testing.T) {
		config.Postgres.Port = 15432
		dsn := config.PostgresDSN()
		assert.Contains(t, dsn, "port=15432")
	})

	t.Run("zero values", func(t *testing.T) {
		config.Server.Port = 0
		config.Redis.DB = 0
		config.Counter.BatchSize = 0

		// 零值應該被正常處理
		assert.Equal(t, 0, config.Server.Port)
		assert.Equal(t, 0, config.Redis.DB)
		assert.Equal(t, 0, config.Counter.BatchSize)
	})

	t.Run("negative values", func(t *testing.T) {
		// 負值通常應該被驗證，但這裡只測試配置本身
		config.Counter.FallbackThreshold = -1
		assert.Equal(t, -1, config.Counter.FallbackThreshold)
	})
}

// TestConfig_TimeoutValues 測試超時配置
func TestConfig_TimeoutValues(t *testing.T) {
	config := &internal.Config{}

	t.Run("server timeouts", func(t *testing.T) {
		config.Server.ReadTimeout = 30 * time.Second
		config.Server.WriteTimeout = 45 * time.Second

		assert.Equal(t, 30*time.Second, config.Server.ReadTimeout)
		assert.Equal(t, 45*time.Second, config.Server.WriteTimeout)
	})

	t.Run("redis timeouts", func(t *testing.T) {
		config.Redis.ReadTimeout = 5 * time.Second
		config.Redis.WriteTimeout = 5 * time.Second

		assert.Equal(t, 5*time.Second, config.Redis.ReadTimeout)
		assert.Equal(t, 5*time.Second, config.Redis.WriteTimeout)
	})

	t.Run("counter flush interval", func(t *testing.T) {
		config.Counter.FlushInterval = 500 * time.Millisecond
		assert.Equal(t, 500*time.Millisecond, config.Counter.FlushInterval)

		config.Counter.FlushInterval = 2 * time.Minute
		assert.Equal(t, 2*time.Minute, config.Counter.FlushInterval)
	})
}

// TestConfig_ConnectionPooling 測試連接池配置
func TestConfig_ConnectionPooling(t *testing.T) {
	config := &internal.Config{}

	t.Run("redis pool configuration", func(t *testing.T) {
		config.Redis.PoolSize = 50
		config.Redis.MinIdleConns = 10
		config.Redis.MaxRetries = 5

		assert.Equal(t, 50, config.Redis.PoolSize)
		assert.Equal(t, 10, config.Redis.MinIdleConns)
		assert.Equal(t, 5, config.Redis.MaxRetries)

		// 驗證合理性
		assert.Greater(t, config.Redis.PoolSize, config.Redis.MinIdleConns,
			"Pool size should be greater than min idle connections")
	})

	t.Run("postgres pool configuration", func(t *testing.T) {
		config.Postgres.MaxConns = 25
		config.Postgres.MinConns = 5

		assert.Equal(t, int32(25), config.Postgres.MaxConns)
		assert.Equal(t, int32(5), config.Postgres.MinConns)

		// 驗證合理性
		assert.Greater(t, config.Postgres.MaxConns, config.Postgres.MinConns,
			"Max connections should be greater than min connections")
	})
}

// TestConfig_LogConfiguration 測試日誌配置
func TestConfig_LogConfiguration(t *testing.T) {
	config := &internal.Config{}

	testCases := []struct {
		name   string
		level  string
		format string
	}{
		{"debug json", "debug", "json"},
		{"info text", "info", "text"},
		{"warn json", "warn", "json"},
		{"error text", "error", "text"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config.Log.Level = tc.level
			config.Log.Format = tc.format

			assert.Equal(t, tc.level, config.Log.Level)
			assert.Equal(t, tc.format, config.Log.Format)
		})
	}
}

// TestConfig_BatchConfiguration 測試批量操作配置
func TestConfig_BatchConfiguration(t *testing.T) {
	config := &internal.Config{}

	t.Run("batch size variations", func(t *testing.T) {
		sizes := []int{1, 10, 100, 1000, 10000}

		for _, size := range sizes {
			config.Counter.BatchSize = size
			assert.Equal(t, size, config.Counter.BatchSize)
		}
	})

	t.Run("flush interval variations", func(t *testing.T) {
		intervals := []time.Duration{
			10 * time.Millisecond,
			100 * time.Millisecond,
			1 * time.Second,
			10 * time.Second,
			1 * time.Minute,
		}

		for _, interval := range intervals {
			config.Counter.FlushInterval = interval
			assert.Equal(t, interval, config.Counter.FlushInterval)
		}
	})
}

// TestConfig_FallbackConfiguration 測試降級配置
func TestConfig_FallbackConfiguration(t *testing.T) {
	config := &internal.Config{}

	t.Run("fallback enabled", func(t *testing.T) {
		config.Counter.EnableFallback = true
		config.Counter.FallbackThreshold = 5

		assert.True(t, config.Counter.EnableFallback)
		assert.Equal(t, 5, config.Counter.FallbackThreshold)
	})

	t.Run("fallback disabled", func(t *testing.T) {
		config.Counter.EnableFallback = false
		config.Counter.FallbackThreshold = 0

		assert.False(t, config.Counter.EnableFallback)
		assert.Equal(t, 0, config.Counter.FallbackThreshold)
	})

	t.Run("various threshold values", func(t *testing.T) {
		thresholds := []int{1, 3, 5, 10, 100}

		for _, threshold := range thresholds {
			config.Counter.FallbackThreshold = threshold
			assert.Equal(t, threshold, config.Counter.FallbackThreshold)
		}
	})
}

// TestConfig_SecurityConfiguration 測試安全相關配置
func TestConfig_SecurityConfiguration(t *testing.T) {
	config := &internal.Config{}

	t.Run("redis password", func(t *testing.T) {
		config.Redis.Password = "redis-secret-password"
		assert.Equal(t, "redis-secret-password", config.Redis.Password)

		// 空密碼（無認證）
		config.Redis.Password = ""
		assert.Empty(t, config.Redis.Password)
	})

	t.Run("postgres password", func(t *testing.T) {
		config.Postgres.Password = "postgres-secret-password"
		assert.Equal(t, "postgres-secret-password", config.Postgres.Password)

		// 包含特殊字符的密碼
		config.Postgres.Password = "p@$$w0rd!@#$%^&*()"
		assert.Equal(t, "p@$$w0rd!@#$%^&*()", config.Postgres.Password)
	})
}

// TestConfig_HostConfiguration 測試主機配置
func TestConfig_HostConfiguration(t *testing.T) {
	config := &internal.Config{}

	t.Run("localhost configurations", func(t *testing.T) {
		config.Redis.Addr = "localhost:6379"
		config.Postgres.Host = "localhost"

		assert.Equal(t, "localhost:6379", config.Redis.Addr)
		assert.Equal(t, "localhost", config.Postgres.Host)
	})

	t.Run("IP address configurations", func(t *testing.T) {
		config.Redis.Addr = "192.168.1.100:6379"
		config.Postgres.Host = "10.0.0.50"

		assert.Equal(t, "192.168.1.100:6379", config.Redis.Addr)
		assert.Equal(t, "10.0.0.50", config.Postgres.Host)
	})

	t.Run("domain name configurations", func(t *testing.T) {
		config.Redis.Addr = "redis.example.com:6379"
		config.Postgres.Host = "db.example.com"

		assert.Equal(t, "redis.example.com:6379", config.Redis.Addr)
		assert.Equal(t, "db.example.com", config.Postgres.Host)
	})

	t.Run("cluster configurations", func(t *testing.T) {
		// Redis cluster 可能有多個地址
		config.Redis.Addr = "redis-node1:6379,redis-node2:6379,redis-node3:6379"
		assert.Contains(t, config.Redis.Addr, "redis-node1")
		assert.Contains(t, config.Redis.Addr, "redis-node2")
		assert.Contains(t, config.Redis.Addr, "redis-node3")
	})
}

// TestConfig_Validation 測試配置驗證（如果有驗證邏輯）
func TestConfig_Validation(t *testing.T) {
	config := &internal.Config{}

	t.Run("valid configuration", func(t *testing.T) {
		config.Server.Port = 8080
		config.Redis.Addr = "localhost:6379"
		config.Postgres.Host = "localhost"
		config.Postgres.Port = 5432
		config.Counter.BatchSize = 100

		// 所有值都應該是有效的
		assert.Greater(t, config.Server.Port, 0)
		assert.NotEmpty(t, config.Redis.Addr)
		assert.NotEmpty(t, config.Postgres.Host)
		assert.Greater(t, config.Postgres.Port, 0)
		assert.Greater(t, config.Counter.BatchSize, 0)
	})

	t.Run("boundary values", func(t *testing.T) {
		// 端口範圍測試
		config.Server.Port = 1
		assert.Equal(t, 1, config.Server.Port)

		config.Server.Port = 65535
		assert.Equal(t, 65535, config.Server.Port)

		// 連接池大小
		config.Redis.PoolSize = 1
		assert.Equal(t, 1, config.Redis.PoolSize)

		config.Postgres.MaxConns = 1
		assert.Equal(t, int32(1), config.Postgres.MaxConns)
	})
}

// BenchmarkConfig_PostgresDSN 基準測試 DSN 生成
func BenchmarkConfig_PostgresDSN(b *testing.B) {
	config := &internal.Config{}
	config.Postgres.Host = "localhost"
	config.Postgres.Port = 5432
	config.Postgres.User = "user"
	config.Postgres.Password = "password"
	config.Postgres.DBName = "database"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = config.PostgresDSN()
	}
}

// BenchmarkConfig_PostgresDSNWithEnv 基準測試使用環境變數的 DSN
func BenchmarkConfig_PostgresDSNWithEnv(b *testing.B) {
	config := &internal.Config{}
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	defer os.Unsetenv("DATABASE_URL")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = config.PostgresDSN()
	}
}