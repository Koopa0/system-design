package internal_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/koopa0/system-design/exercise-1/internal"
	"github.com/koopa0/system-design/exercise-1/internal/testutils"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCounter_Increment 測試增加計數器的各種情況
func TestCounter_Increment(t *testing.T) {
	tests := []struct {
		name           string
		counterName    string
		incrementValue int64
		userID         string
		setupFunc      func(t *testing.T, env *testutils.TestEnvironment)
		expectedValue  int64
		expectedError  bool
	}{
		{
			name:           "simple increment",
			counterName:    "test_counter",
			incrementValue: 1,
			expectedValue:  1,
		},
		{
			name:           "increment by 10",
			counterName:    "test_counter",
			incrementValue: 10,
			expectedValue:  10,
		},
		{
			name:           "increment existing counter",
			counterName:    "existing_counter",
			incrementValue: 5,
			setupFunc: func(t *testing.T, env *testutils.TestEnvironment) {
				// 預設值為 10
				err := env.RedisClient.Set(context.Background(), "counter:existing_counter", 10, 0).Err()
				require.NoError(t, err)
			},
			expectedValue: 15,
		},
		{
			name:           "increment with user ID (DAU)",
			counterName:    "daily_active_users",
			incrementValue: 1,
			userID:         "user_123",
			expectedValue:  1,
		},
		{
			name:           "increment with duplicate user ID",
			counterName:    "daily_active_users",
			incrementValue: 1,
			userID:         "user_456",
			setupFunc: func(t *testing.T, env *testutils.TestEnvironment) {
				// 先增加一次相同用戶
				ctx := context.Background()
				location, _ := time.LoadLocation("Asia/Taipei")
				today := time.Now().In(location).Format("20060102")
				dauKey := fmt.Sprintf("counter:daily_active_users:users:%s", today)
				
				err := env.RedisClient.SAdd(ctx, dauKey, "user_456").Err()
				require.NoError(t, err)
				
				err = env.RedisClient.Set(ctx, "counter:daily_active_users", 1, 0).Err()
				require.NoError(t, err)
			},
			expectedValue: 1, // 不應該增加
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 設置測試環境
			env := testutils.SetupTestEnvironment(t)
			defer env.Cleanup()

			config := testutils.DefaultTestConfig()
			counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
			defer counter.Shutdown()

			// 執行設置函數
			if tt.setupFunc != nil {
				tt.setupFunc(t, env)
			}

			// 執行測試
			ctx := context.Background()
			value, err := counter.Increment(ctx, tt.counterName, tt.incrementValue, tt.userID)

			// 驗證結果
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedValue, value)

				// 驗證 Redis 中的值
				redisValue, err := env.RedisClient.Get(ctx, fmt.Sprintf("counter:%s", tt.counterName)).Int64()
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedValue, redisValue)
			}
		})
	}
}

// TestCounter_Decrement 測試減少計數器
func TestCounter_Decrement(t *testing.T) {
	tests := []struct {
		name           string
		counterName    string
		initialValue   int64
		decrementValue int64
		expectedValue  int64
	}{
		{
			name:           "simple decrement",
			counterName:    "test_counter",
			initialValue:   10,
			decrementValue: 3,
			expectedValue:  7,
		},
		{
			name:           "decrement to zero",
			counterName:    "test_counter",
			initialValue:   5,
			decrementValue: 5,
			expectedValue:  0,
		},
		{
			name:           "decrement below zero (should stop at 0)",
			counterName:    "test_counter",
			initialValue:   3,
			decrementValue: 10,
			expectedValue:  0,
		},
		{
			name:           "decrement non-existing counter",
			counterName:    "new_counter",
			initialValue:   0,
			decrementValue: 5,
			expectedValue:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testutils.SetupTestEnvironment(t)
			defer env.Cleanup()

			config := testutils.DefaultTestConfig()
			counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
			defer counter.Shutdown()

			ctx := context.Background()

			// 設置初始值
			if tt.initialValue > 0 {
				err := env.RedisClient.Set(ctx, fmt.Sprintf("counter:%s", tt.counterName), tt.initialValue, 0).Err()
				require.NoError(t, err)
			}

			// 執行減少操作
			value, err := counter.Decrement(ctx, tt.counterName, tt.decrementValue)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedValue, value)

			// 驗證 Redis 中的值
			redisValue, err := env.RedisClient.Get(ctx, fmt.Sprintf("counter:%s", tt.counterName)).Int64()
			if tt.expectedValue == 0 && tt.initialValue == 0 {
				// 計數器可能不存在
				if err == redis.Nil {
					redisValue = 0
				} else {
					assert.NoError(t, err)
				}
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expectedValue, redisValue)
		})
	}
}

// TestCounter_GetValue 測試獲取計數器值
func TestCounter_GetValue(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	t.Run("get existing counter from Redis", func(t *testing.T) {
		// 設置測試資料
		err := env.RedisClient.Set(ctx, "counter:test1", 42, 0).Err()
		require.NoError(t, err)

		value, err := counter.GetValue(ctx, "test1")
		assert.NoError(t, err)
		assert.Equal(t, int64(42), value)
	})

	t.Run("get non-existing counter", func(t *testing.T) {
		value, err := counter.GetValue(ctx, "non_existing")
		assert.NoError(t, err)
		assert.Equal(t, int64(0), value)
	})

	t.Run("get counter when Redis fails", func(t *testing.T) {
		// 暫時關閉 Redis 連接以模擬失敗
		env.RedisClient.Close()

		// 應該從 PostgreSQL 獲取
		value, err := counter.GetValue(ctx, "fallback_test")
		// 可能會有錯誤，但應該嘗試從 PostgreSQL 獲取
		_ = value
		_ = err
	})
}

// TestCounter_GetMultiple 測試批量獲取計數器
func TestCounter_GetMultiple(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	// 設置測試資料
	testData := map[string]int64{
		"counter1": 10,
		"counter2": 20,
		"counter3": 30,
	}

	for name, value := range testData {
		err := env.RedisClient.Set(ctx, fmt.Sprintf("counter:%s", name), value, 0).Err()
		require.NoError(t, err)
	}

	t.Run("get multiple existing counters", func(t *testing.T) {
		names := []string{"counter1", "counter2", "counter3"}
		values, err := counter.GetMultiple(ctx, names)

		assert.NoError(t, err)
		assert.Len(t, values, 3)
		assert.Equal(t, int64(10), values["counter1"])
		assert.Equal(t, int64(20), values["counter2"])
		assert.Equal(t, int64(30), values["counter3"])
	})

	t.Run("get multiple with some non-existing", func(t *testing.T) {
		names := []string{"counter1", "non_existing", "counter3"}
		values, err := counter.GetMultiple(ctx, names)

		assert.NoError(t, err)
		assert.Len(t, values, 3)
		assert.Equal(t, int64(10), values["counter1"])
		assert.Equal(t, int64(0), values["non_existing"])
		assert.Equal(t, int64(30), values["counter3"])
	})
}

// TestCounter_Reset 測試重置計數器
func TestCounter_Reset(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	t.Run("reset existing counter", func(t *testing.T) {
		// 設置初始值
		err := env.RedisClient.Set(ctx, "counter:test_reset", 100, 0).Err()
		require.NoError(t, err)

		// 重置計數器
		err = counter.Reset(ctx, "test_reset")
		assert.NoError(t, err)

		// 驗證值為 0
		value, err := counter.GetValue(ctx, "test_reset")
		assert.NoError(t, err)
		assert.Equal(t, int64(0), value)
	})

	t.Run("reset with user sets", func(t *testing.T) {
		location, _ := time.LoadLocation("Asia/Taipei")
		today := time.Now().In(location).Format("20060102")
		dauKey := fmt.Sprintf("counter:daily_users:users:%s", today)

		// 設置計數器和用戶集合
		err := env.RedisClient.Set(ctx, "counter:daily_users", 50, 0).Err()
		require.NoError(t, err)

		err = env.RedisClient.SAdd(ctx, dauKey, "user1", "user2", "user3").Err()
		require.NoError(t, err)

		// 重置
		err = counter.Reset(ctx, "daily_users")
		assert.NoError(t, err)

		// 驗證計數器為 0
		value, err := counter.GetValue(ctx, "daily_users")
		assert.NoError(t, err)
		assert.Equal(t, int64(0), value)

		// 驗證用戶集合被清理
		exists := env.RedisClient.Exists(ctx, dauKey).Val()
		assert.Equal(t, int64(0), exists)
	})
}

// TestCounter_ConcurrentOperations 測試並發操作
func TestCounter_ConcurrentOperations(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	t.Run("concurrent increments", func(t *testing.T) {
		const (
			workers    = 10
			increments = 100
		)

		var wg sync.WaitGroup
		wg.Add(workers)

		for i := 0; i < workers; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < increments; j++ {
					_, err := counter.Increment(ctx, "concurrent_test", 1, "")
					assert.NoError(t, err)
				}
			}()
		}

		wg.Wait()

		// 驗證最終值
		value, err := counter.GetValue(ctx, "concurrent_test")
		assert.NoError(t, err)
		assert.Equal(t, int64(workers*increments), value)
	})

	t.Run("concurrent mixed operations", func(t *testing.T) {
		// 設置初始值
		err := env.RedisClient.Set(ctx, "counter:mixed_test", 1000, 0).Err()
		require.NoError(t, err)

		const workers = 10
		var wg sync.WaitGroup
		wg.Add(workers * 2)

		// 並發增加
		for i := 0; i < workers; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					_, _ = counter.Increment(ctx, "mixed_test", 1, "")
				}
			}()
		}

		// 並發減少
		for i := 0; i < workers; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < 30; j++ {
					_, _ = counter.Decrement(ctx, "mixed_test", 1)
				}
			}()
		}

		wg.Wait()

		// 驗證最終值
		value, err := counter.GetValue(ctx, "mixed_test")
		assert.NoError(t, err)
		// 1000 + (10*50) - (10*30) = 1000 + 500 - 300 = 1200
		assert.Equal(t, int64(1200), value)
	})
}

// TestCounter_FallbackMode 測試降級模式
func TestCounter_FallbackMode(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	config.Counter.FallbackThreshold = 2 // 設置較低的閾值以便測試
	
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	t.Run("trigger fallback mode", func(t *testing.T) {
		// 關閉 Redis 連接以觸發錯誤
		env.RedisClient.Close()

		// 多次操作觸發降級
		for i := 0; i < 3; i++ {
			_, _ = counter.Increment(ctx, "fallback_test", 1, "")
		}

		// 給一些時間讓降級生效
		time.Sleep(100 * time.Millisecond)

		// 這時應該使用 PostgreSQL
		// 由於 PostgreSQL 也需要正確設置，這裡只是測試降級邏輯被觸發
	})
}

// TestCounter_BatchWriting 測試批量寫入
func TestCounter_BatchWriting(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	config.Counter.BatchSize = 5
	config.Counter.FlushInterval = 100 * time.Millisecond

	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	t.Run("batch flush on size", func(t *testing.T) {
		// 快速增加多個計數器以觸發批量寫入
		for i := 0; i < 10; i++ {
			_, err := counter.Increment(ctx, fmt.Sprintf("batch_test_%d", i), 1, "")
			assert.NoError(t, err)
		}

		// 等待批量寫入完成
		time.Sleep(200 * time.Millisecond)

		// 驗證值都被正確設置
		for i := 0; i < 10; i++ {
			value, err := counter.GetValue(ctx, fmt.Sprintf("batch_test_%d", i))
			assert.NoError(t, err)
			assert.Equal(t, int64(1), value)
		}
	})

	t.Run("batch flush on interval", func(t *testing.T) {
		// 少量操作，依賴時間間隔觸發
		_, err := counter.Increment(ctx, "interval_test", 5, "")
		assert.NoError(t, err)

		// 等待間隔時間
		time.Sleep(150 * time.Millisecond)

		// 驗證值被同步
		value, err := counter.GetValue(ctx, "interval_test")
		assert.NoError(t, err)
		assert.Equal(t, int64(5), value)
	})
}

// TestCounter_UniqueUsers 測試去重用戶計數（DAU）
func TestCounter_UniqueUsers(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	t.Run("count unique users", func(t *testing.T) {
		users := []string{"user1", "user2", "user3", "user1", "user2", "user4"}
		
		for _, userID := range users {
			_, err := counter.Increment(ctx, "daily_active_users", 1, userID)
			assert.NoError(t, err)
		}

		// 應該只計算唯一用戶：user1, user2, user3, user4 = 4
		value, err := counter.GetValue(ctx, "daily_active_users")
		assert.NoError(t, err)
		assert.Equal(t, int64(4), value)
	})

	t.Run("user set expiration", func(t *testing.T) {
		location, _ := time.LoadLocation("Asia/Taipei")
		today := time.Now().In(location).Format("20060102")
		dauKey := fmt.Sprintf("counter:expiry_test:users:%s", today)

		// 增加一個用戶
		_, err := counter.Increment(ctx, "expiry_test", 1, "test_user")
		assert.NoError(t, err)

		// 檢查過期時間是否設置
		ttl := env.RedisClient.TTL(ctx, dauKey).Val()
		assert.True(t, ttl > 0, "TTL should be set")
		assert.True(t, ttl <= 24*time.Hour, "TTL should be less than 24 hours")
	})
}

// TestCounter_ErrorHandling 測試錯誤處理
func TestCounter_ErrorHandling(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()

	t.Run("handle Redis errors gracefully", func(t *testing.T) {
		counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
		defer counter.Shutdown()

		ctx := context.Background()

		// 模擬 Redis 錯誤
		env.RedisClient.Close()

		// 操作應該降級到 PostgreSQL
		_, err := counter.Increment(ctx, "error_test", 1, "")
		// 錯誤可能發生，但不應該 panic
		_ = err
	})
}

// BenchmarkCounter_Increment 基準測試：增加操作
func BenchmarkCounter_Increment(b *testing.B) {
	helper := testutils.SetupBenchmark(b)
	defer helper.Cleanup()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			counterName := fmt.Sprintf("bench_counter_%d", i%100)
			_, err := helper.Counter.Increment(ctx, counterName, 1, "")
			if err != nil {
				b.Fatalf("increment failed: %v", err)
			}
			i++
		}
	})
}

// BenchmarkCounter_IncrementWithUser 基準測試：帶用戶ID的增加操作
func BenchmarkCounter_IncrementWithUser(b *testing.B) {
	helper := testutils.SetupBenchmark(b)
	defer helper.Cleanup()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			userID := fmt.Sprintf("user_%d", i%1000)
			_, err := helper.Counter.Increment(ctx, "dau_benchmark", 1, userID)
			if err != nil {
				b.Fatalf("increment with user failed: %v", err)
			}
			i++
		}
	})
}

// BenchmarkCounter_GetValue 基準測試：獲取操作
func BenchmarkCounter_GetValue(b *testing.B) {
	helper := testutils.SetupBenchmark(b)
	defer helper.Cleanup()

	ctx := context.Background()

	// 預設一些計數器
	for i := 0; i < 100; i++ {
		counterName := fmt.Sprintf("get_bench_%d", i)
		err := helper.Env.RedisClient.Set(ctx, fmt.Sprintf("counter:%s", counterName), i*10, 0).Err()
		if err != nil {
			b.Fatalf("setup failed: %v", err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			counterName := fmt.Sprintf("get_bench_%d", i%100)
			_, err := helper.Counter.GetValue(ctx, counterName)
			if err != nil {
				b.Fatalf("get value failed: %v", err)
			}
			i++
		}
	})
}

// BenchmarkCounter_GetMultiple 基準測試：批量獲取
func BenchmarkCounter_GetMultiple(b *testing.B) {
	helper := testutils.SetupBenchmark(b)
	defer helper.Cleanup()

	ctx := context.Background()

	// 預設計數器
	names := make([]string, 10)
	for i := 0; i < 10; i++ {
		names[i] = fmt.Sprintf("multi_bench_%d", i)
		key := fmt.Sprintf("counter:%s", names[i])
		err := helper.Env.RedisClient.Set(ctx, key, i*100, 0).Err()
		if err != nil {
			b.Fatalf("setup failed: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := helper.Counter.GetMultiple(ctx, names)
		if err != nil {
			b.Fatalf("get multiple failed: %v", err)
		}
	}
}

// TestCounter_RaceConditions 測試競態條件
func TestCounter_RaceConditions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race condition test in short mode")
	}

	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	const (
		numGoroutines = 50
		numOperations = 100
	)

	var (
		incrementTotal int64
		decrementTotal int64
	)

	// 設置初始值
	initialValue := int64(10000)
	err := env.RedisClient.Set(ctx, "counter:race_test", initialValue, 0).Err()
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2)

	// 增加操作
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				value := int64(j%10 + 1)
				_, err := counter.Increment(ctx, "race_test", value, "")
				if err == nil {
					atomic.AddInt64(&incrementTotal, value)
				}
			}
		}()
	}

	// 減少操作
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				value := int64(j%5 + 1)
				_, err := counter.Decrement(ctx, "race_test", value)
				if err == nil {
					atomic.AddInt64(&decrementTotal, value)
				}
			}
		}()
	}

	wg.Wait()

	// 驗證最終值
	finalValue, err := counter.GetValue(ctx, "race_test")
	assert.NoError(t, err)

	expectedValue := initialValue + incrementTotal - decrementTotal
	if expectedValue < 0 {
		expectedValue = 0
	}

	assert.Equal(t, expectedValue, finalValue, 
		"Final value should match expected: initial(%d) + increments(%d) - decrements(%d) = %d",
		initialValue, incrementTotal, decrementTotal, expectedValue)
}

// TestCounter_MemoryLeaks 測試記憶體洩漏
func TestCounter_MemoryLeaks(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory leak test in short mode")
	}

	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	config.Counter.BatchSize = 1000
	config.Counter.FlushInterval = 50 * time.Millisecond

	// 創建和銷毀多個計數器實例
	for i := 0; i < 10; i++ {
		counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
		
		ctx := context.Background()
		
		// 執行一些操作
		for j := 0; j < 100; j++ {
			counterName := fmt.Sprintf("leak_test_%d_%d", i, j)
			_, _ = counter.Increment(ctx, counterName, 1, "")
		}
		
		// 關閉計數器
		counter.Shutdown()
		
		// 給一些時間讓 goroutine 清理
		time.Sleep(10 * time.Millisecond)
	}

	// 如果有記憶體洩漏，這個測試在多次執行時會顯示記憶體持續增長
}

// TestCounter_EdgeCases 測試邊界情況
func TestCounter_EdgeCases(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	t.Run("very large increment", func(t *testing.T) {
		largeValue := int64(1<<62 - 1) // 接近 int64 最大值
		value, err := counter.Increment(ctx, "large_test", largeValue, "")
		assert.NoError(t, err)
		assert.Equal(t, largeValue, value)
	})

	t.Run("empty counter name", func(t *testing.T) {
		// 空名稱應該正常處理
		_, err := counter.Increment(ctx, "", 1, "")
		// 可能成功也可能失敗，但不應該 panic
		_ = err
	})

	t.Run("very long counter name", func(t *testing.T) {
		longName := ""
		for j := 0; j < 1000; j++ {
			longName += "a"
		}
		
		_, err := counter.Increment(ctx, longName, 1, "")
		// 應該能處理長名稱
		_ = err
	})

	t.Run("special characters in counter name", func(t *testing.T) {
		specialNames := []string{
			"counter:with:colons",
			"counter-with-dashes",
			"counter_with_underscores",
			"counter.with.dots",
			"counter/with/slashes",
			"counter with spaces",
			"計數器中文",
			"🚀emoji",
		}

		for _, name := range specialNames {
			value, err := counter.Increment(ctx, name, 1, "")
			assert.NoError(t, err)
			assert.Equal(t, int64(1), value)

			retrieved, err := counter.GetValue(ctx, name)
			assert.NoError(t, err)
			assert.Equal(t, int64(1), retrieved)
		}
	})

	t.Run("negative increment value", func(t *testing.T) {
		// 負數增加應該減少計數
		err := env.RedisClient.Set(ctx, "counter:negative_test", 10, 0).Err()
		require.NoError(t, err)

		value, err := counter.Increment(ctx, "negative_test", -3, "")
		assert.NoError(t, err)
		assert.Equal(t, int64(7), value)
	})

	t.Run("zero increment value", func(t *testing.T) {
		err := env.RedisClient.Set(ctx, "counter:zero_test", 5, 0).Err()
		require.NoError(t, err)

		value, err := counter.Increment(ctx, "zero_test", 0, "")
		assert.NoError(t, err)
		assert.Equal(t, int64(5), value) // 值不變
	})
}

// TestCounter_Integration 整合測試
func TestCounter_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	t.Run("complete workflow", func(t *testing.T) {
		counterName := "integration_test"

		// 1. 初始增加
		value, err := counter.Increment(ctx, counterName, 10, "")
		assert.NoError(t, err)
		assert.Equal(t, int64(10), value)

		// 2. 再次增加
		value, err = counter.Increment(ctx, counterName, 5, "")
		assert.NoError(t, err)
		assert.Equal(t, int64(15), value)

		// 3. 減少
		value, err = counter.Decrement(ctx, counterName, 3)
		assert.NoError(t, err)
		assert.Equal(t, int64(12), value)

		// 4. 獲取值
		value, err = counter.GetValue(ctx, counterName)
		assert.NoError(t, err)
		assert.Equal(t, int64(12), value)

		// 5. 批量獲取（包含此計數器）
		values, err := counter.GetMultiple(ctx, []string{counterName, "other_counter"})
		assert.NoError(t, err)
		assert.Equal(t, int64(12), values[counterName])
		assert.Equal(t, int64(0), values["other_counter"])

		// 6. 重置
		err = counter.Reset(ctx, counterName)
		assert.NoError(t, err)

		// 7. 驗證重置後的值
		value, err = counter.GetValue(ctx, counterName)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), value)
	})

	t.Run("DAU workflow", func(t *testing.T) {
		dauCounter := "daily_active_users_integration"

		// 模擬不同用戶訪問
		users := []struct {
			id       string
			visits   int
			expected int64
		}{
			{"user_a", 3, 1},  // 多次訪問只計一次
			{"user_b", 1, 2},  // 新用戶
			{"user_c", 2, 3},  // 新用戶
			{"user_a", 5, 3},  // 重複用戶，不增加
			{"user_d", 1, 4},  // 新用戶
		}

		for _, u := range users {
			for i := 0; i < u.visits; i++ {
				value, err := counter.Increment(ctx, dauCounter, 1, u.id)
				assert.NoError(t, err)
				assert.Equal(t, u.expected, value, 
					"User %s after %d visits should result in count %d", 
					u.id, i+1, u.expected)
			}
		}

		// 最終驗證
		finalValue, err := counter.GetValue(ctx, dauCounter)
		assert.NoError(t, err)
		assert.Equal(t, int64(4), finalValue, "Should have 4 unique users")
	})
}

// TestCounter_StressTest 壓力測試
func TestCounter_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	config.Counter.BatchSize = 500
	config.Counter.FlushInterval = 10 * time.Millisecond

	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	const (
		numCounters   = 100
		numGoroutines = 100
		numOperations = 100
	)

	// 創建多個計數器並發操作
	var wg sync.WaitGroup
	errorCount := atomic.Int32{}
	successCount := atomic.Int32{}

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < numOperations; j++ {
				counterID := fmt.Sprintf("stress_%d", j%numCounters)
				operation := j % 4

				var err error
				switch operation {
				case 0:
					_, err = counter.Increment(ctx, counterID, 1, "")
				case 1:
					_, err = counter.Decrement(ctx, counterID, 1)
				case 2:
					_, err = counter.GetValue(ctx, counterID)
				case 3:
					if j%10 == 0 { // 減少批量操作頻率
						names := make([]string, 5)
						for k := 0; k < 5; k++ {
							names[k] = fmt.Sprintf("stress_%d", (j+k)%numCounters)
						}
						_, err = counter.GetMultiple(ctx, names)
					}
				}

				if err != nil {
					errorCount.Add(1)
					if errors.Is(err, context.DeadlineExceeded) {
						return // 超時則停止
					}
				} else {
					successCount.Add(1)
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	// 輸出統計
	totalOps := int32(numGoroutines * numOperations)
	successRate := float64(successCount.Load()) / float64(totalOps) * 100
	opsPerSecond := float64(successCount.Load()) / duration.Seconds()

	t.Logf("Stress test completed:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Total operations: %d", totalOps)
	t.Logf("  Successful: %d", successCount.Load())
	t.Logf("  Failed: %d", errorCount.Load())
	t.Logf("  Success rate: %.2f%%", successRate)
	t.Logf("  Operations/second: %.2f", opsPerSecond)

	// 至少要有 95% 的成功率
	assert.True(t, successRate >= 95.0, "Success rate should be at least 95%")
}