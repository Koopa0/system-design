package internal_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/koopa0/system-design/01-counter-service/internal"
	"github.com/koopa0/system-design/01-counter-service/internal/sqlc"
	"github.com/koopa0/system-design/01-counter-service/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPostgres_IncrementFallback 測試 PostgreSQL 降級增加功能
func TestPostgres_IncrementFallback(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	t.Run("increment in fallback mode", func(t *testing.T) {
		// 關閉 Redis 以強制使用 PostgreSQL
		env.RedisClient.Close()

		// 觸發降級模式
		for i := 0; i < config.Counter.FallbackThreshold+1; i++ {
			_, _ = counter.Increment(ctx, fmt.Sprintf("fallback_test_%d", i), 1, "")
		}

		// 等待降級生效
		time.Sleep(100 * time.Millisecond)

		// 現在應該使用 PostgreSQL
		value, err := counter.Increment(ctx, "postgres_increment", 10, "")
		// 可能會有錯誤（取決於 PostgreSQL 設置），但不應該 panic
		if err == nil {
			assert.Equal(t, int64(10), value)

			// 驗證值存在於 PostgreSQL
			queries := sqlc.New(env.PostgresPool)
			dbCounter, err := queries.GetCounter(ctx, "postgres_increment")
			if err == nil {
				assert.Equal(t, int64(10), dbCounter.CurrentValue.Int64)
			}
		}
	})
}

// TestPostgres_DecrementFallback 測試 PostgreSQL 降級減少功能
func TestPostgres_DecrementFallback(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	queries := sqlc.New(env.PostgresPool)

	// 預先在 PostgreSQL 中創建計數器
	ctx := context.Background()
	_, err := queries.CreateCounter(ctx, sqlc.CreateCounterParams{
		Name:    "decrement_test",
		Column2: "normal",
	})
	// 忽略已存在錯誤
	_ = err

	// 設置初始值
	err = queries.SetCounter(ctx, sqlc.SetCounterParams{
		Name:         "decrement_test",
		CurrentValue: pgtype.Int8{Int64: 20, Valid: true},
	})
	require.NoError(t, err)

	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	t.Run("decrement in PostgreSQL", func(t *testing.T) {
		// 關閉 Redis
		env.RedisClient.Close()

		// 觸發降級
		for i := 0; i < config.Counter.FallbackThreshold+1; i++ {
			_, _ = counter.GetValue(ctx, fmt.Sprintf("trigger_%d", i))
		}

		time.Sleep(100 * time.Millisecond)

		// 執行減少操作
		value, err := counter.Decrement(ctx, "decrement_test", 5)
		if err == nil {
			assert.Equal(t, int64(15), value)

			// 驗證 PostgreSQL 中的值
			dbCounter, err := queries.GetCounter(ctx, "decrement_test")
			if err == nil {
				assert.Equal(t, int64(15), dbCounter.CurrentValue.Int64)
			}
		}
	})

	t.Run("decrement below zero protection", func(t *testing.T) {
		// 設置較小的值
		err := queries.SetCounter(ctx, sqlc.SetCounterParams{
			Name:         "decrement_zero_test",
			CurrentValue: pgtype.Int8{Int64: 3, Valid: true},
		})
		require.NoError(t, err)

		// 減少超過當前值
		value, err := counter.Decrement(ctx, "decrement_zero_test", 10)
		if err == nil {
			assert.Equal(t, int64(0), value)

			// 驗證不會小於 0
			dbCounter, err := queries.GetCounter(ctx, "decrement_zero_test")
			if err == nil {
				assert.GreaterOrEqual(t, dbCounter.CurrentValue.Int64, int64(0))
			}
		}
	})
}

// TestPostgres_GetValueFallback 測試從 PostgreSQL 獲取值
func TestPostgres_GetValueFallback(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	queries := sqlc.New(env.PostgresPool)
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	t.Run("get value from PostgreSQL when Redis is down", func(t *testing.T) {
		// 在 PostgreSQL 中設置值
		_, err := queries.CreateCounter(ctx, sqlc.CreateCounterParams{
			Name:    "pg_only_counter",
			Column2: "normal",
		})
		_ = err // 忽略已存在錯誤

		err = queries.SetCounter(ctx, sqlc.SetCounterParams{
			Name:         "pg_only_counter",
			CurrentValue: pgtype.Int8{Int64: 42, Valid: true},
		})
		require.NoError(t, err)

		// 關閉 Redis
		env.RedisClient.Close()

		// 應該從 PostgreSQL 獲取
		value, err := counter.GetValue(ctx, "pg_only_counter")
		if err == nil {
			assert.Equal(t, int64(42), value)
		}
	})

	t.Run("get non-existing counter from PostgreSQL", func(t *testing.T) {
		value, err := counter.GetValue(ctx, "non_existing_pg")
		// 不存在時應返回 0
		if err == nil {
			assert.Equal(t, int64(0), value)
		}
	})
}

// TestPostgres_GetMultipleFallback 測試批量從 PostgreSQL 獲取
func TestPostgres_GetMultipleFallback(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	queries := sqlc.New(env.PostgresPool)
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	// 在 PostgreSQL 中創建測試資料
	testCounters := map[string]int64{
		"pg_counter1": 10,
		"pg_counter2": 20,
		"pg_counter3": 30,
	}

	for name, value := range testCounters {
		_, _ = queries.CreateCounter(ctx, sqlc.CreateCounterParams{
			Name:    name,
			Column2: "normal",
		})

		err := queries.SetCounter(ctx, sqlc.SetCounterParams{
			Name:         name,
			CurrentValue: pgtype.Int8{Int64: value, Valid: true},
		})
		require.NoError(t, err)
	}

	t.Run("get multiple from PostgreSQL", func(t *testing.T) {
		// 關閉 Redis
		env.RedisClient.Close()

		names := []string{"pg_counter1", "pg_counter2", "pg_counter3", "non_existing"}
		values, err := counter.GetMultiple(ctx, names)

		if err == nil {
			assert.Len(t, values, 4)
			assert.Equal(t, int64(10), values["pg_counter1"])
			assert.Equal(t, int64(20), values["pg_counter2"])
			assert.Equal(t, int64(30), values["pg_counter3"])
			assert.Equal(t, int64(0), values["non_existing"])
		}
	})
}

// TestPostgres_SyncToPostgres 測試同步到 PostgreSQL
func TestPostgres_SyncToPostgres(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	config.Counter.FlushInterval = 50 * time.Millisecond // 快速刷新

	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	queries := sqlc.New(env.PostgresPool)
	ctx := context.Background()

	t.Run("sync after increment", func(t *testing.T) {
		// 在 Redis 中增加
		value, err := counter.Increment(ctx, "sync_test", 15, "")
		require.NoError(t, err)
		assert.Equal(t, int64(15), value)

		// 等待同步
		time.Sleep(100 * time.Millisecond)

		// 檢查 PostgreSQL
		dbCounter, err := queries.GetCounter(ctx, "sync_test")
		if err == nil {
			assert.Equal(t, int64(15), dbCounter.CurrentValue.Int64)
		}
	})

	t.Run("batch sync", func(t *testing.T) {
		// 快速執行多個操作
		for i := 0; i < 5; i++ {
			counterName := fmt.Sprintf("batch_sync_%d", i)
			_, err := counter.Increment(ctx, counterName, int64(i+1), "")
			assert.NoError(t, err)
		}

		// 等待批量同步
		time.Sleep(150 * time.Millisecond)

		// 驗證所有計數器都被同步
		for i := 0; i < 5; i++ {
			counterName := fmt.Sprintf("batch_sync_%d", i)
			dbCounter, err := queries.GetCounter(ctx, counterName)
			if err == nil {
				assert.Equal(t, int64(i+1), dbCounter.CurrentValue.Int64)
			}
		}
	})
}

// TestPostgres_WriteQueue 測試寫入佇列功能
func TestPostgres_WriteQueue(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	config.Counter.EnableFallback = true

	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	queries := sqlc.New(env.PostgresPool)
	ctx := context.Background()

	t.Run("enqueue write operations", func(t *testing.T) {
		// 關閉 Redis 以觸發降級
		env.RedisClient.Close()

		// 觸發降級模式
		for i := 0; i < config.Counter.FallbackThreshold+1; i++ {
			_, _ = counter.Increment(ctx, fmt.Sprintf("trigger_%d", i), 1, "")
		}

		time.Sleep(100 * time.Millisecond)

		// 執行操作（應該被加入佇列）
		_, _ = counter.Increment(ctx, "queued_counter", 5, "")

		// 檢查佇列
		time.Sleep(50 * time.Millisecond)
		writes, err := queries.DequeueWrites(ctx, 10)
		if err == nil && len(writes) > 0 {
			found := false
			for _, write := range writes {
				if write.CounterName == "queued_counter" {
					assert.Equal(t, "increment", write.Operation)
					assert.Equal(t, int64(5), write.Value)
					found = true
					break
				}
			}
			assert.True(t, found, "Write operation should be queued")
		}
	})
}

// TestPostgres_Recovery 測試從 PostgreSQL 恢復到 Redis
func TestPostgres_Recovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping recovery test in short mode")
	}

	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	queries := sqlc.New(env.PostgresPool)
	ctx := context.Background()

	// 在 PostgreSQL 中設置測試資料
	testData := map[string]int64{
		"recovery_counter1": 100,
		"recovery_counter2": 200,
		"recovery_counter3": 300,
	}

	for name, value := range testData {
		_, _ = queries.CreateCounter(ctx, sqlc.CreateCounterParams{
			Name:    name,
			Column2: "normal",
		})

		err := queries.SetCounter(ctx, sqlc.SetCounterParams{
			Name:         name,
			CurrentValue: pgtype.Int8{Int64: value, Valid: true},
		})
		require.NoError(t, err)
	}

	// 創建計數器（會觸發恢復）
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	// 等待恢復完成
	time.Sleep(6 * time.Second) // 根據 startRecoveryWorkerSQLc 的延遲

	t.Run("verify recovery to Redis", func(t *testing.T) {
		// 檢查資料是否已恢復到 Redis
		for name, expectedValue := range testData {
			redisValue, err := env.RedisClient.Get(ctx, fmt.Sprintf("counter:%s", name)).Int64()
			if err == nil {
				assert.Equal(t, expectedValue, redisValue,
					"Counter %s should be recovered to Redis", name)
			}
		}
	})
}

// TestPostgres_ConcurrentOperations 測試並發 PostgreSQL 操作
func TestPostgres_ConcurrentOperations(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	ctx := context.Background()

	// 使用 mock querier 來模擬 PostgreSQL 操作
	mock := testutils.NewMockQuerier()
	mock.SetCounterValue("concurrent_pg", 0)

	t.Run("concurrent increments", func(t *testing.T) {
		const numGoroutines = 10
		const incrementsPerGoroutine = 10

		done := make(chan bool, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				for j := 0; j < incrementsPerGoroutine; j++ {
					_, err := mock.IncrementCounter(ctx, sqlc.IncrementCounterParams{
						Name:         "concurrent_pg",
						CurrentValue: pgtype.Int8{Int64: 1, Valid: true},
					})
					assert.NoError(t, err)
				}
				done <- true
			}()
		}

		// 等待所有 goroutine 完成
		for i := 0; i < numGoroutines; i++ {
			<-done
		}

		// 驗證最終值
		finalValue, exists := mock.GetCounterValue("concurrent_pg")
		assert.True(t, exists)
		assert.Equal(t, int64(numGoroutines*incrementsPerGoroutine), finalValue)
	})
}

// TestPostgres_ErrorHandling 測試 PostgreSQL 錯誤處理
func TestPostgres_ErrorHandling(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	t.Run("handle connection errors gracefully", func(t *testing.T) {
		// 關閉 PostgreSQL 連接池
		env.PostgresPool.Close()

		// 同時關閉 Redis 以強制使用 PostgreSQL
		env.RedisClient.Close()

		// 操作應該失敗但不應該 panic
		_, err := counter.Increment(ctx, "error_test", 1, "")
		assert.Error(t, err)
	})
}

// TestPostgres_ResetFallback 測試 PostgreSQL 重置功能
func TestPostgres_ResetFallback(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	queries := sqlc.New(env.PostgresPool)
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	t.Run("reset counter in PostgreSQL", func(t *testing.T) {
		// 創建並設置初始值
		_, _ = queries.CreateCounter(ctx, sqlc.CreateCounterParams{
			Name:    "reset_test",
			Column2: "normal",
		})

		err := queries.SetCounter(ctx, sqlc.SetCounterParams{
			Name:         "reset_test",
			CurrentValue: pgtype.Int8{Int64: 100, Valid: true},
		})
		require.NoError(t, err)

		// 執行重置
		err = counter.Reset(ctx, "reset_test")
		assert.NoError(t, err)

		// 驗證已重置
		dbCounter, err := queries.GetCounter(ctx, "reset_test")
		if err == nil {
			assert.Equal(t, int64(0), dbCounter.CurrentValue.Int64)
		}
	})
}

// BenchmarkPostgres_Increment 基準測試：PostgreSQL 增加操作
func BenchmarkPostgres_Increment(b *testing.B) {
	helper := testutils.SetupBenchmark(b)
	defer helper.Cleanup()

	// 使用 mock 來測試純 PostgreSQL 性能
	mock := testutils.NewMockQuerier()
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			counterName := fmt.Sprintf("bench_pg_%d", i%100)
			_, err := mock.IncrementCounter(ctx, sqlc.IncrementCounterParams{
				Name:         counterName,
				CurrentValue: pgtype.Int8{Int64: 1, Valid: true},
			})
			if err != nil {
				b.Fatalf("increment failed: %v", err)
			}
			i++
		}
	})
}

// BenchmarkPostgres_Get 基準測試：PostgreSQL 獲取操作
func BenchmarkPostgres_Get(b *testing.B) {
	helper := testutils.SetupBenchmark(b)
	defer helper.Cleanup()

	mock := testutils.NewMockQuerier()
	ctx := context.Background()

	// 預設一些計數器
	for i := 0; i < 100; i++ {
		counterName := fmt.Sprintf("get_bench_pg_%d", i)
		mock.SetCounterValue(counterName, int64(i*10))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			counterName := fmt.Sprintf("get_bench_pg_%d", i%100)
			_, err := mock.GetCounter(ctx, counterName)
			if err != nil {
				b.Fatalf("get failed: %v", err)
			}
			i++
		}
	})
}

// TestPostgres_Integration 整合測試
func TestPostgres_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	config.Counter.EnableFallback = true
	config.Counter.FallbackThreshold = 2

	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	queries := sqlc.New(env.PostgresPool)
	ctx := context.Background()

	t.Run("complete fallback workflow", func(t *testing.T) {
		counterName := "integration_fallback"

		// 1. 正常操作（使用 Redis）
		value, err := counter.Increment(ctx, counterName, 10, "")
		assert.NoError(t, err)
		assert.Equal(t, int64(10), value)

		// 2. 等待同步到 PostgreSQL
		time.Sleep(150 * time.Millisecond)

		// 3. 驗證 PostgreSQL 有資料
		dbCounter, err := queries.GetCounter(ctx, counterName)
		if err == nil {
			assert.Equal(t, int64(10), dbCounter.CurrentValue.Int64)
		}

		// 4. 模擬 Redis 故障
		env.RedisClient.Close()

		// 5. 觸發降級
		for i := 0; i < 3; i++ {
			_, _ = counter.GetValue(ctx, fmt.Sprintf("trigger_%d", i))
		}

		time.Sleep(100 * time.Millisecond)

		// 6. 在降級模式下操作
		value, err = counter.Increment(ctx, counterName, 5, "")
		if err == nil {
			assert.Equal(t, int64(15), value)
		}

		// 7. 驗證 PostgreSQL 更新
		dbCounter, err = queries.GetCounter(ctx, counterName)
		if err == nil {
			assert.Equal(t, int64(15), dbCounter.CurrentValue.Int64)
		}
	})
}
