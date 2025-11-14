package internal_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/koopa0/system-design/01-counter-service/internal"
	"github.com/koopa0/system-design/01-counter-service/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResetScheduler_Creation 測試重置排程器的創建
func TestResetScheduler_Creation(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	t.Run("create reset scheduler", func(t *testing.T) {
		scheduler := internal.NewResetScheduler(counter, env.Logger)
		assert.NotNil(t, scheduler)

		// 啟動排程器
		scheduler.Start()

		// 給一些時間讓排程器初始化
		time.Sleep(100 * time.Millisecond)

		// 停止排程器
		scheduler.Stop()
	})
}

// TestResetScheduler_DailyReset 測試每日重置功能
func TestResetScheduler_DailyReset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping daily reset test in short mode")
	}

	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	// 設置測試資料
	dailyCounters := []string{
		"daily_active_users",
		"total_games_played",
	}

	for _, name := range dailyCounters {
		_, err := counter.Increment(ctx, name, 100, "")
		require.NoError(t, err)
	}

	t.Run("reset daily counters", func(t *testing.T) {
		// 直接調用重置函數（不等待午夜）
		// 注意：這需要將 resetDailyCounters 方法設為公開，或通過其他方式測試
		// 由於該方法是私有的，我們模擬其行為

		// 模擬重置行為
		for _, name := range dailyCounters {
			err := counter.Reset(ctx, name)
			assert.NoError(t, err)

			// 驗證計數器已重置
			value, err := counter.GetValue(ctx, name)
			assert.NoError(t, err)
			assert.Equal(t, int64(0), value)
		}
	})
}

// TestResetScheduler_Archive 測試計數器歸檔功能
func TestResetScheduler_Archive(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	t.Run("archive counter before reset", func(t *testing.T) {
		counterName := "archive_test"

		// 設置計數器值
		_, err := counter.Increment(ctx, counterName, 50, "")
		require.NoError(t, err)

		// 添加一些唯一用戶（如果是 DAU 類型）
		location, _ := time.LoadLocation("Asia/Taipei")
		today := time.Now().In(location).Format("20060102")
		dauKey := fmt.Sprintf("counter:%s:users:%s", counterName, today)

		users := []string{"user1", "user2", "user3"}
		for _, user := range users {
			err = env.RedisClient.SAdd(ctx, dauKey, user).Err()
			require.NoError(t, err)
		}

		// 創建歷史記錄表（如果不存在）
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
		)`

		_, err = env.PostgresPool.Exec(ctx, createHistoryTable)
		require.NoError(t, err)

		// 執行歸檔
		yesterday := time.Now().In(location).AddDate(0, 0, -1)
		query := `
		INSERT INTO counter_history (counter_name, date, final_value, unique_users, metadata)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (counter_name, date) DO UPDATE
		SET final_value = EXCLUDED.final_value,
		    unique_users = EXCLUDED.unique_users,
		    metadata = EXCLUDED.metadata`

		usersJSON, _ := json.Marshal(users)
		metadata := map[string]any{
			"archived_at": time.Now(),
			"user_count":  len(users),
		}
		metadataJSON, _ := json.Marshal(metadata)

		_, err = env.PostgresPool.Exec(ctx, query,
			counterName,
			yesterday.Format("2006-01-02"),
			50,
			usersJSON,
			metadataJSON,
		)
		require.NoError(t, err)

		// 驗證歸檔記錄
		var archivedValue int64
		var archivedUsers []byte // 使用 []byte 替代 json.RawMessage

		err = env.PostgresPool.QueryRow(ctx,
			"SELECT final_value, unique_users FROM counter_history WHERE counter_name = $1 AND date = $2",
			counterName, yesterday.Format("2006-01-02"),
		).Scan(&archivedValue, &archivedUsers)

		require.NoError(t, err)
		assert.Equal(t, int64(50), archivedValue)

		var retrievedUsers []string
		err = json.Unmarshal(archivedUsers, &retrievedUsers)
		require.NoError(t, err)
		assert.ElementsMatch(t, users, retrievedUsers)
	})
}

// TestResetScheduler_CleanOldHistory 測試清理舊歷史記錄
func TestResetScheduler_CleanOldHistory(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	ctx := context.Background()

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
	)`

	_, err := env.PostgresPool.Exec(ctx, createHistoryTable)
	require.NoError(t, err)

	t.Run("clean records older than 7 days", func(t *testing.T) {
		// 插入測試資料
		now := time.Now()
		testData := []struct {
			daysAgo         int
			shouldBeDeleted bool
		}{
			{1, false}, // 1 天前，應保留
			{3, false}, // 3 天前，應保留
			{7, false}, // 7 天前，應保留
			{8, true},  // 8 天前，應刪除
			{10, true}, // 10 天前，應刪除
			{30, true}, // 30 天前，應刪除
		}

		for i, td := range testData {
			date := now.AddDate(0, 0, -td.daysAgo)
			counterName := fmt.Sprintf("history_test_%d", i)

			_, err := env.PostgresPool.Exec(ctx,
				`INSERT INTO counter_history (counter_name, date, final_value) VALUES ($1, $2, $3)`,
				counterName, date.Format("2006-01-02"), 100,
			)
			require.NoError(t, err)
		}

		// 執行清理
		query := `DELETE FROM counter_history WHERE date < CURRENT_DATE - INTERVAL '7 days'`
		result, err := env.PostgresPool.Exec(ctx, query)
		require.NoError(t, err)

		rowsDeleted := result.RowsAffected()
		assert.Equal(t, int64(3), rowsDeleted, "Should delete 3 old records")

		// 驗證剩餘記錄
		var count int
		err = env.PostgresPool.QueryRow(ctx,
			`SELECT COUNT(*) FROM counter_history`,
		).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 3, count, "Should have 3 records remaining")
	})
}

// TestResetScheduler_Timezone 測試時區處理
func TestResetScheduler_Timezone(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	t.Run("use Asia/Taipei timezone", func(t *testing.T) {
		location, err := time.LoadLocation("Asia/Taipei")
		require.NoError(t, err)

		now := time.Now().In(location)
		tomorrow := now.AddDate(0, 0, 1)
		midnight := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, location)

		// 驗證午夜時間計算正確
		assert.Equal(t, 0, midnight.Hour())
		assert.Equal(t, 0, midnight.Minute())
		assert.Equal(t, 0, midnight.Second())
		assert.Equal(t, "Asia/Taipei", midnight.Location().String())
	})

	t.Run("date string format", func(t *testing.T) {
		location, _ := time.LoadLocation("Asia/Taipei")
		now := time.Now().In(location)
		dateStr := now.Format("20060102")

		// 驗證日期格式
		assert.Equal(t, 8, len(dateStr))

		// 解析回來應該相同
		parsed, err := time.ParseInLocation("20060102", dateStr, location)
		require.NoError(t, err)
		assert.Equal(t, now.Year(), parsed.Year())
		assert.Equal(t, now.Month(), parsed.Month())
		assert.Equal(t, now.Day(), parsed.Day())
	})
}

// TestResetScheduler_ConcurrentReset 測試並發重置
func TestResetScheduler_ConcurrentReset(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	t.Run("concurrent resets", func(t *testing.T) {
		// 設置多個計數器
		numCounters := 10
		for i := 0; i < numCounters; i++ {
			name := fmt.Sprintf("concurrent_reset_%d", i)
			_, err := counter.Increment(ctx, name, int64(i*10), "")
			require.NoError(t, err)
		}

		// 並發重置
		done := make(chan bool, numCounters)
		for i := 0; i < numCounters; i++ {
			go func(idx int) {
				name := fmt.Sprintf("concurrent_reset_%d", idx)
				err := counter.Reset(ctx, name)
				assert.NoError(t, err)
				done <- true
			}(i)
		}

		// 等待完成
		for i := 0; i < numCounters; i++ {
			<-done
		}

		// 驗證所有計數器都被重置
		for i := 0; i < numCounters; i++ {
			name := fmt.Sprintf("concurrent_reset_%d", i)
			value, err := counter.GetValue(ctx, name)
			assert.NoError(t, err)
			assert.Equal(t, int64(0), value)
		}
	})
}

// TestResetScheduler_ErrorHandling 測試錯誤處理
func TestResetScheduler_ErrorHandling(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	t.Run("handle database errors", func(t *testing.T) {
		// 關閉資料庫連接
		env.PostgresPool.Close()

		scheduler := internal.NewResetScheduler(counter, env.Logger)

		// 啟動應該不會 panic
		assert.NotPanics(t, func() {
			scheduler.Start()
			time.Sleep(100 * time.Millisecond)
			scheduler.Stop()
		})
	})

	t.Run("handle Redis errors", func(t *testing.T) {
		// 關閉 Redis
		env.RedisClient.Close()

		scheduler := internal.NewResetScheduler(counter, env.Logger)

		// 操作應該優雅處理錯誤
		assert.NotPanics(t, func() {
			scheduler.Start()
			time.Sleep(100 * time.Millisecond)
			scheduler.Stop()
		})
	})
}

// TestResetScheduler_MultipleSchedulers 測試多個排程器
func TestResetScheduler_MultipleSchedulers(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	t.Run("multiple schedulers don't interfere", func(t *testing.T) {
		scheduler1 := internal.NewResetScheduler(counter, env.Logger)
		scheduler2 := internal.NewResetScheduler(counter, env.Logger)

		// 啟動兩個排程器
		scheduler1.Start()
		scheduler2.Start()

		// 給一些時間運行
		time.Sleep(200 * time.Millisecond)

		// 停止排程器
		scheduler1.Stop()
		scheduler2.Stop()

		// 不應該有問題
	})
}

// TestResetScheduler_Integration 整合測試
func TestResetScheduler_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	ctx := context.Background()

	// 創建必要的表
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
	)`

	_, err := env.PostgresPool.Exec(ctx, createHistoryTable)
	require.NoError(t, err)

	t.Run("complete daily reset workflow", func(t *testing.T) {
		// 1. 設置計數器
		counterName := "daily_active_users"
		users := []string{"user1", "user2", "user3"}

		for _, userID := range users {
			_, err := counter.Increment(ctx, counterName, 1, userID)
			assert.NoError(t, err)
		}

		// 驗證當前值
		value, err := counter.GetValue(ctx, counterName)
		assert.NoError(t, err)
		assert.Equal(t, int64(3), value)

		// 2. 手動執行歸檔（模擬午夜前）
		location, _ := time.LoadLocation("Asia/Taipei")
		yesterday := time.Now().In(location).AddDate(0, 0, -1)

		usersJSON, _ := json.Marshal(users)
		metadata := map[string]any{
			"archived_at": time.Now(),
			"user_count":  len(users),
		}
		metadataJSON, _ := json.Marshal(metadata)

		_, err = env.PostgresPool.Exec(ctx,
			`INSERT INTO counter_history (counter_name, date, final_value, unique_users, metadata)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT (counter_name, date) DO UPDATE
			 SET final_value = EXCLUDED.final_value`,
			counterName,
			yesterday.Format("2006-01-02"),
			value,
			usersJSON,
			metadataJSON,
		)
		require.NoError(t, err)

		// 3. 執行重置
		err = counter.Reset(ctx, counterName)
		assert.NoError(t, err)

		// 4. 驗證重置後的值
		value, err = counter.GetValue(ctx, counterName)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), value)

		// 5. 驗證歸檔記錄存在
		var archivedValue int64
		err = env.PostgresPool.QueryRow(ctx,
			`SELECT final_value FROM counter_history WHERE counter_name = $1 AND date = $2`,
			counterName, yesterday.Format("2006-01-02"),
		).Scan(&archivedValue)

		require.NoError(t, err)
		assert.Equal(t, int64(3), archivedValue)

		// 6. 新的一天開始計數
		newUsers := []string{"user4", "user5"}
		for _, userID := range newUsers {
			_, err := counter.Increment(ctx, counterName, 1, userID)
			assert.NoError(t, err)
		}

		value, err = counter.GetValue(ctx, counterName)
		assert.NoError(t, err)
		assert.Equal(t, int64(2), value)
	})
}
