// Package testutils 提供測試用的共用工具和輔助函數
//
// 本套件實作了測試容器（testcontainers）的管理，包括：
//   - Redis 測試容器
//   - PostgreSQL 測試容器
//   - 資料庫遷移工具
//   - 測試資料清理
//
// 所有測試容器都會在測試結束時自動清理。
package testutils

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	tc "github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestEnvironment 封裝測試環境
type TestEnvironment struct {
	RedisClient    *redis.Client
	PostgresPool   *pgxpool.Pool
	RedisContainer tc.Container
	PgContainer    tc.Container
	RedisAddr      string
	PostgresDSN    string
	Logger         *slog.Logger
	ctx            context.Context
	t              testing.TB
}

// SetupTestEnvironment 設置完整的測試環境
//
// 這個函數會：
//  1. 啟動 Redis 容器
//  2. 啟動 PostgreSQL 容器
//  3. 執行資料庫遷移
//  4. 註冊清理函數
//
// 使用範例：
//
//	func TestSomething(t *testing.T) {
//	    env := testutils.SetupTestEnvironment(t)
//	    // 使用 env.RedisClient 和 env.PostgresPool
//	}
func SetupTestEnvironment(t testing.TB) *TestEnvironment {
	t.Helper()

	ctx := context.Background()
	env := &TestEnvironment{
		ctx: ctx,
		t:   t,
		Logger: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelWarn, // 測試時減少日誌噪音
		})),
	}

	// 設置 Redis
	env.setupRedis(t)

	// 設置 PostgreSQL
	env.setupPostgreSQL(t)

	// 註冊清理
	t.Cleanup(func() {
		env.Cleanup()
	})

	return env
}

// setupRedis 啟動 Redis 測試容器
func (env *TestEnvironment) setupRedis(t testing.TB) {
	t.Helper()

	ctx := env.ctx

	// 啟動 Redis 容器
	redisContainer, err := tcredis.Run(ctx,
		"redis:7-alpine",
		tcredis.WithSnapshotting(10, 1), // 啟用快照以測試持久化
		tcredis.WithLogLevel(tcredis.LogLevelVerbose),
	)
	if err != nil {
		t.Fatalf("failed to start redis container: %v", err)
	}

	env.RedisContainer = redisContainer

	// 獲取連接地址
	endpoint, err := redisContainer.Endpoint(ctx, "")
	if err != nil {
		t.Fatalf("failed to get redis endpoint: %v", err)
	}
	env.RedisAddr = endpoint

	// 建立 Redis 客戶端
	env.RedisClient = redis.NewClient(&redis.Options{
		Addr:         endpoint,
		DB:           0,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 5,
	})

	// 驗證連接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := env.RedisClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("failed to ping redis: %v", err)
	}
}

// setupPostgreSQL 啟動 PostgreSQL 測試容器並執行遷移
func (env *TestEnvironment) setupPostgreSQL(t testing.TB) {
	t.Helper()

	ctx := env.ctx

	// 啟動 PostgreSQL 容器
	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("testdb"),
		tcpostgres.WithUsername("testuser"),
		tcpostgres.WithPassword("testpass"),
		tcpostgres.WithSQLDriver("pgx"),
		tc.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	env.PgContainer = pgContainer

	// 獲取連接字串
	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get postgres connection string: %v", err)
	}
	env.PostgresDSN = dsn

	// 建立連接池
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("failed to parse postgres config: %v", err)
	}

	// 設定連接池參數
	config.MaxConns = 10
	config.MinConns = 2
	config.MaxConnLifetime = 30 * time.Minute
	config.MaxConnIdleTime = 5 * time.Minute

	env.PostgresPool, err = pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("failed to create postgres pool: %v", err)
	}

	// 驗證連接
	if err := env.PostgresPool.Ping(ctx); err != nil {
		t.Fatalf("failed to ping postgres: %v", err)
	}

	// 執行資料庫遷移
	env.runMigrations(t)
}

// runMigrations 執行資料庫遷移
func (env *TestEnvironment) runMigrations(t testing.TB) {
	t.Helper()

	// 使用 database/sql 來執行遷移
	db, err := sql.Open("postgres", env.PostgresDSN)
	if err != nil {
		t.Fatalf("failed to open sql connection for migration: %v", err)
	}
	defer db.Close()

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		t.Fatalf("failed to create migration driver: %v", err)
	}

	// 尋找 migrations 目錄
	migrationPaths := []string{
		"file://../../sql/migrations",
		"file://../sql/migrations",
		"file://sql/migrations",
		"file://./sql/migrations",
	}

	var m *migrate.Migrate
	var migrationErr error

	for _, path := range migrationPaths {
		m, migrationErr = migrate.NewWithDatabaseInstance(
			path,
			"postgres", driver,
		)
		if migrationErr == nil {
			break
		}
	}

	if migrationErr != nil {
		// 如果找不到遷移檔案，創建基本的測試表
		env.createTestTables(t)
		return
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("failed to run migrations: %v", err)
	}
}

// createTestTables 創建測試用的基本表結構
func (env *TestEnvironment) createTestTables(t testing.TB) {
	t.Helper()

	ctx := env.ctx

	// 創建計數器表
	createCountersTable := `
	CREATE TABLE IF NOT EXISTS counters (
		id SERIAL PRIMARY KEY,
		name VARCHAR(255) UNIQUE NOT NULL,
		current_value BIGINT DEFAULT 0,
		type VARCHAR(50) DEFAULT 'normal',
		metadata JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_counters_name ON counters(name);
	CREATE INDEX IF NOT EXISTS idx_counters_type ON counters(type);
	`

	// 創建寫入佇列表
	createWriteQueueTable := `
	CREATE TABLE IF NOT EXISTS write_queue (
		id SERIAL PRIMARY KEY,
		counter_name VARCHAR(255) NOT NULL,
		operation VARCHAR(50) NOT NULL,
		value BIGINT NOT NULL,
		user_id VARCHAR(255),
		metadata JSONB,
		processed BOOLEAN DEFAULT FALSE,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE INDEX IF NOT EXISTS idx_write_queue_processed ON write_queue(processed);
	CREATE INDEX IF NOT EXISTS idx_write_queue_created ON write_queue(created_at);
	`

	// 創建歷史記錄表
	createHistoryTable := `
	CREATE TABLE IF NOT EXISTS counter_history (
		id SERIAL PRIMARY KEY,
		counter_name VARCHAR(255) NOT NULL,
		date DATE NOT NULL,
		final_value BIGINT NOT NULL,
		unique_users JSONB,
		metadata JSONB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(counter_name, date)
	);

	CREATE INDEX IF NOT EXISTS idx_history_date ON counter_history(date);
	CREATE INDEX IF NOT EXISTS idx_history_counter ON counter_history(counter_name);
	`

	tables := []string{
		createCountersTable,
		createWriteQueueTable,
		createHistoryTable,
	}

	for _, ddl := range tables {
		if _, err := env.PostgresPool.Exec(ctx, ddl); err != nil {
			t.Fatalf("failed to create test table: %v", err)
		}
	}
}

// Cleanup 清理測試環境
func (env *TestEnvironment) Cleanup() {
	ctx := context.Background()

	if env.RedisClient != nil {
		_ = env.RedisClient.Close()
	}

	if env.PostgresPool != nil {
		env.PostgresPool.Close()
	}

	if env.RedisContainer != nil {
		_ = env.RedisContainer.Terminate(ctx)
	}

	if env.PgContainer != nil {
		_ = env.PgContainer.Terminate(ctx)
	}
}

// FlushRedis 清空 Redis 資料（用於測試之間的清理）
func (env *TestEnvironment) FlushRedis(t testing.TB) {
	t.Helper()

	ctx := context.Background()
	if err := env.RedisClient.FlushDB(ctx).Err(); err != nil {
		t.Fatalf("failed to flush redis: %v", err)
	}
}

// TruncatePostgresTables 清空 PostgreSQL 表（用於測試之間的清理）
func (env *TestEnvironment) TruncatePostgresTables(t testing.TB) {
	t.Helper()

	ctx := context.Background()
	tables := []string{"counters", "write_queue", "counter_history"}

	for _, table := range tables {
		query := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table)
		if _, err := env.PostgresPool.Exec(ctx, query); err != nil {
			// 忽略表不存在的錯誤
			if !isTableNotExistError(err) {
				t.Fatalf("failed to truncate table %s: %v", table, err)
			}
		}
	}
}

// ResetTestData 重置所有測試資料
func (env *TestEnvironment) ResetTestData(t testing.TB) {
	t.Helper()

	env.FlushRedis(t)
	env.TruncatePostgresTables(t)
}

// isTableNotExistError 檢查是否為表不存在錯誤
func isTableNotExistError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return errStr == "relation does not exist" || errStr == "table does not exist"
}

// WaitForRedis 等待 Redis 就緒
func (env *TestEnvironment) WaitForRedis(t testing.TB, timeout time.Duration) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatal("timeout waiting for redis")
		case <-ticker.C:
			if err := env.RedisClient.Ping(ctx).Err(); err == nil {
				return
			}
		}
	}
}

// WaitForPostgres 等待 PostgreSQL 就緒
func (env *TestEnvironment) WaitForPostgres(t testing.TB, timeout time.Duration) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatal("timeout waiting for postgres")
		case <-ticker.C:
			if err := env.PostgresPool.Ping(ctx); err == nil {
				return
			}
		}
	}
}
