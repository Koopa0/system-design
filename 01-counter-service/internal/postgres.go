package internal

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/koopa0/system-design/01-counter-service/internal/sqlc"
)

// incrementPostgresSQLc 使用 sqlc 從 PostgreSQL 增加計數器（降級模式）
func (c *Counter) incrementPostgresSQLc(ctx context.Context, name string, value int64) (int64, error) {
	// 確保計數器存在
	if err := c.ensureCounterSQLc(ctx, name); err != nil {
		return 0, fmt.Errorf("ensure counter: %w", err)
	}

	// 原子性增加計數器
	result, err := c.queries.IncrementCounter(ctx, sqlc.IncrementCounterParams{
		Name:         name,
		CurrentValue: pgtype.Int8{Int64: value, Valid: true},
	})
	if err != nil {
		c.logger.Error("postgres increment failed",
			"counter", name,
			"value", value,
			"error", err)
		return 0, fmt.Errorf("increment counter: %w", err)
	}

	// 同時加入佇列，等待 Redis 恢復後同步
	if c.config.Counter.EnableFallback {
		c.enqueueWriteSQLc(ctx, name, "increment", value, "")
	}

	return result.Int64, nil
}

// decrementPostgresSQLc 使用 sqlc 從 PostgreSQL 減少計數器（降級模式）
func (c *Counter) decrementPostgresSQLc(ctx context.Context, name string, value int64) (int64, error) {
	// 確保計數器存在
	if err := c.ensureCounterSQLc(ctx, name); err != nil {
		return 0, fmt.Errorf("ensure counter: %w", err)
	}

	// 原子性減少計數器
	result, err := c.queries.DecrementCounter(ctx, sqlc.DecrementCounterParams{
		Name:         name,
		CurrentValue: pgtype.Int8{Int64: value, Valid: true},
	})
	if err != nil {
		c.logger.Error("postgres decrement failed",
			"counter", name,
			"value", value,
			"error", err)
		return 0, fmt.Errorf("decrement counter: %w", err)
	}

	// 加入佇列
	if c.config.Counter.EnableFallback {
		c.enqueueWriteSQLc(ctx, name, "decrement", value, "")
	}

	return result.Int64, nil
}

// getValuePostgresSQLc 使用 sqlc 從 PostgreSQL 獲取計數器值（降級模式）
func (c *Counter) getValuePostgresSQLc(ctx context.Context, name string) (int64, error) {
	counter, err := c.queries.GetCounter(ctx, name)
	if err != nil {
		// 計數器不存在時返回 0
		if err.Error() == "no rows in result set" {
			c.logger.Debug("counter not found in postgres", "counter", name)
			return 0, nil
		}
		c.logger.Error("postgres get value failed",
			"counter", name,
			"error", err)
		return 0, fmt.Errorf("get counter value: %w", err)
	}

	return counter.CurrentValue.Int64, nil
}

// getMultiplePostgresSQLc 使用 sqlc 從 PostgreSQL 批量獲取計數器值（降級模式）
func (c *Counter) getMultiplePostgresSQLc(ctx context.Context, names []string) (map[string]int64, error) {
	result := make(map[string]int64, len(names))

	// 初始化所有計數器為 0
	for _, name := range names {
		result[name] = 0
	}

	// 批量查詢
	counters, err := c.queries.GetCounters(ctx, names)
	if err != nil {
		c.logger.Error("postgres get multiple failed",
			"counters", names,
			"error", err)
		return nil, fmt.Errorf("get multiple counters: %w", err)
	}

	// 收集結果
	for _, counter := range counters {
		result[counter.Name] = counter.CurrentValue.Int64
	}

	return result, nil
}

// resetPostgresSQLc 使用 sqlc 在 PostgreSQL 中重置計數器
func (c *Counter) resetPostgresSQLc(ctx context.Context, name string) error {
	err := c.queries.ResetCounter(ctx, name)
	if err != nil {
		c.logger.Error("postgres reset failed",
			"counter", name,
			"error", err)
		return fmt.Errorf("reset counter: %w", err)
	}

	return nil
}

// syncToPostgresSQLc 使用 sqlc 同步計數器值到 PostgreSQL（後台任務）
func (c *Counter) syncToPostgresSQLc(ctx context.Context, name string, value int64) error {
	err := c.queries.SetCounter(ctx, sqlc.SetCounterParams{
		Name:         name,
		CurrentValue: pgtype.Int8{Int64: value, Valid: true},
	})
	if err != nil {
		c.logger.Error("sync to postgres failed",
			"counter", name,
			"value", value,
			"error", err)
		return fmt.Errorf("sync to postgres: %w", err)
	}

	return nil
}

// ensureCounterSQLc 使用 sqlc 確保計數器存在
func (c *Counter) ensureCounterSQLc(ctx context.Context, name string) error {
	_, err := c.queries.CreateCounter(ctx, sqlc.CreateCounterParams{
		Name:     name,
		Column2:  sqlc.CounterTypeNormal,
		Metadata: nil,
	})
	if err != nil && err.Error() != "no rows in result set" {
		c.logger.Error("ensure counter failed",
			"counter", name,
			"error", err)
		return fmt.Errorf("ensure counter: %w", err)
	}

	return nil
}

// enqueueWriteSQLc 使用 sqlc 將寫入操作加入佇列（用於降級模式）
func (c *Counter) enqueueWriteSQLc(ctx context.Context, name, operation string, value int64, userID string) {
	userIDParam := pgtype.Text{String: userID, Valid: userID != ""}

	_, err := c.queries.EnqueueWrite(ctx, sqlc.EnqueueWriteParams{
		CounterName: name,
		Operation:   operation,
		Value:       value,
		UserID:      userIDParam,
		Metadata:    nil,
	})
	if err != nil {
		// 佇列寫入失敗不影響主流程，只記錄日誌
		c.logger.Warn("failed to enqueue write",
			"counter", name,
			"operation", operation,
			"error", err)
	}
}

// processWriteQueueSQLc 使用 sqlc 處理寫入佇列（Redis 恢復後執行）
func (c *Counter) processWriteQueueSQLc(ctx context.Context) error {
	// 批量獲取未處理的寫入操作
	writes, err := c.queries.DequeueWrites(ctx, 100)
	if err != nil {
		return fmt.Errorf("query write queue: %w", err)
	}

	// 處理每個佇列項目
	var processed []int32
	for _, write := range writes {
		// 重放操作到 Redis
		key := fmt.Sprintf("counter:%s", write.CounterName)
		var replayErr error

		switch write.Operation {
		case "increment":
			_, replayErr = c.redis.IncrBy(ctx, key, write.Value).Result()
		case "decrement":
			_, replayErr = c.redis.DecrBy(ctx, key, write.Value).Result()
		case "reset":
			replayErr = c.redis.Set(ctx, key, 0, 0).Err()
		}

		if replayErr != nil {
			c.logger.Error("replay operation failed",
				"id", write.ID,
				"counter", write.CounterName,
				"operation", write.Operation,
				"error", replayErr)
			continue
		}

		processed = append(processed, write.ID)
	}

	// 標記已處理的項目
	for _, id := range processed {
		if err := c.queries.MarkWriteProcessed(ctx, id); err != nil {
			c.logger.Error("mark processed failed", "id", id, "error", err)
		}
	}

	if len(processed) > 0 {
		c.logger.Info("processed write queue items", "count", len(processed))
	}

	// 清理舊的已處理項目
	_ = c.queries.CleanOldQueue(ctx)

	return nil
}

// recoverFromPostgresSQLc 使用 sqlc 從 PostgreSQL 恢復到 Redis
func (c *Counter) recoverFromPostgresSQLc(ctx context.Context) error {
	c.logger.Info("starting recovery from postgres")

	// 獲取所有計數器
	counters, err := c.queries.ListCounters(ctx, sqlc.ListCountersParams{
		Limit:  1000,
		Offset: 0,
	})
	if err != nil {
		return fmt.Errorf("query counters: %w", err)
	}

	// 同步到 Redis
	pipe := c.redis.Pipeline()
	count := 0

	for _, counter := range counters {
		key := fmt.Sprintf("counter:%s", counter.Name)
		pipe.Set(ctx, key, counter.CurrentValue.Int64, 0)
		count++

		// 每 100 個執行一次
		if count%100 == 0 {
			if _, err := pipe.Exec(ctx); err != nil {
				c.logger.Error("sync batch to redis failed", "error", err)
			}
			pipe = c.redis.Pipeline()
		}
	}

	// 執行剩餘的
	if count%100 != 0 {
		if _, err := pipe.Exec(ctx); err != nil {
			c.logger.Error("sync final batch to redis failed", "error", err)
		}
	}

	c.logger.Info("recovery completed", "counters", count)

	// 處理寫入佇列
	return c.processWriteQueueSQLc(ctx)
}

// startRecoveryWorkerSQLc 啟動使用 sqlc 的恢復 worker
func (c *Counter) startRecoveryWorkerSQLc() {
	go func() {
		// 等待一段時間讓系統穩定
		time.Sleep(5 * time.Second)

		// 首次恢復
		if !c.fallbackMode.Load() {
			ctx := context.Background()
			if err := c.recoverFromPostgresSQLc(ctx); err != nil {
				c.logger.Error("initial recovery failed", "error", err)
			}
		}

		// 定期處理佇列
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			if !c.fallbackMode.Load() {
				ctx := context.Background()
				if err := c.processWriteQueueSQLc(ctx); err != nil {
					c.logger.Error("process write queue failed", "error", err)
				}
			}
		}
	}()
}
