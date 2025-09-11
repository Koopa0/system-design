// Package internal 包含計數服務的核心業務邏輯實現
//
// 實現了計數器的所有核心功能，包括：
//   - 原子計數操作（增加、減少、查詢）
//   - 去重計數（如 DAU 統計）
//   - 批量操作優化
//   - Redis 故障降級
//   - 數據持久化同步
//
// 設計原則：
//   - 優先使用 Redis 確保高效能
//   - PostgreSQL 作為備份保證可靠性
//   - 無鎖設計避免競爭條件
//   - 批量處理減少 I/O 開銷
package internal

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/koopa0/system-design/exercise-1/internal/sqlc"
	"github.com/redis/go-redis/v9"
)

// CounterType 定義計數器類型
type CounterType string

const (
	// CounterTypeNormal 普通計數器
	CounterTypeNormal CounterType = "normal"

	// CounterTypeUnique 去重計數器（如 DAU）
	CounterTypeUnique CounterType = "unique"

	// CounterTypeDaily 每日重置計數器
	CounterTypeDaily CounterType = "daily"
)

// Counter 計數器核心實作
type Counter struct {
	redis   *redis.Client
	pg      *pgxpool.Pool
	queries sqlc.Querier
	config  *Config
	logger  *slog.Logger

	// 降級控制
	fallbackMode atomic.Bool  // 是否處於降級模式
	redisErrors  atomic.Int32 // Redis 錯誤計數

	// 批量寫入緩衝
	batchBuffer chan *batchWrite
	wg          sync.WaitGroup
}

// batchWrite 批量寫入項目
type batchWrite struct {
	name      string
	operation string // increment, decrement
	value     int64
	userID    string
	timestamp time.Time
}

// NewCounter 創建計數器實例
func NewCounter(redis *redis.Client, pg *pgxpool.Pool, config *Config, logger *slog.Logger) *Counter {
	c := &Counter{
		redis:       redis,
		pg:          pg,
		queries:     sqlc.New(pg),
		config:      config,
		logger:      logger,
		batchBuffer: make(chan *batchWrite, config.Counter.BatchSize*2),
	}

	// 啟動批量寫入 worker
	c.wg.Add(1)
	go c.batchWorker()

	// 啟動恢復 worker（使用 sqlc）
	c.startRecoveryWorkerSQLc()

	return c
}

// Increment 增加計數器
func (c *Counter) Increment(ctx context.Context, name string, value int64, userID string) (int64, error) {
	// 根據時區設定獲取台北時間
	location, _ := time.LoadLocation("Asia/Taipei")
	today := time.Now().In(location).Format("20060102")

	// 降級模式檢查
	if c.fallbackMode.Load() {
		return c.incrementPostgresSQLc(ctx, name, value)
	}

	// 處理去重計數（如 DAU）
	if userID != "" {
		dauKey := fmt.Sprintf("counter:%s:users:%s", name, today)

		// 使用 SADD 實現去重，返回 1 表示新增，0 表示已存在
		added, err := c.redis.SAdd(ctx, dauKey, userID).Result()
		if err != nil {
			c.handleRedisError(err)
			return 0, err
		}

		// 設置過期時間（台北時間次日凌晨）
		if added > 0 {
			tomorrow := time.Now().In(location).AddDate(0, 0, 1)
			midnight := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, location)
			c.redis.ExpireAt(ctx, dauKey, midnight)

			// 只有新用戶才增加計數
			value = added
		} else {
			// 用戶已存在，不增加計數
			return c.GetValue(ctx, name)
		}
	}

	// 執行原子增加操作
	key := fmt.Sprintf("counter:%s", name)
	newVal, err := c.redis.IncrBy(ctx, key, value).Result()
	if err != nil {
		c.handleRedisError(err)
		// 降級到 PostgreSQL
		return c.incrementPostgresSQLc(ctx, name, value)
	}

	// 異步更新 PostgreSQL（最終一致性）
	select {
	case c.batchBuffer <- &batchWrite{
		name:      name,
		operation: "increment",
		value:     value,
		userID:    userID,
		timestamp: time.Now(),
	}:
	default:
		// 緩衝區滿，直接寫入
		go c.syncToPostgresSQLc(ctx, name, newVal)
	}

	// 重置錯誤計數
	c.redisErrors.Store(0)

	return newVal, nil
}

// Decrement 減少計數器
func (c *Counter) Decrement(ctx context.Context, name string, value int64) (int64, error) {
	if c.fallbackMode.Load() {
		return c.decrementPostgresSQLc(ctx, name, value)
	}

	key := fmt.Sprintf("counter:%s", name)

	// Lua script 確保不會減到負數
	script := redis.NewScript(`
		local key = KEYS[1]
		local decr = tonumber(ARGV[1])
		local current = redis.call('GET', key)
		if not current then
			current = 0
		else
			current = tonumber(current)
		end
		local new_val = math.max(0, current - decr)
		redis.call('SET', key, new_val)
		return new_val
	`)

	result, err := script.Run(ctx, c.redis, []string{key}, value).Result()
	if err != nil {
		c.handleRedisError(err)
		return c.decrementPostgresSQLc(ctx, name, value)
	}

	newVal := result.(int64)

	// 異步同步到 PostgreSQL
	select {
	case c.batchBuffer <- &batchWrite{
		name:      name,
		operation: "decrement",
		value:     value,
		timestamp: time.Now(),
	}:
	default:
		go c.syncToPostgresSQLc(ctx, name, newVal)
	}

	c.redisErrors.Store(0)
	return newVal, nil
}

// GetValue 獲取計數器當前值
func (c *Counter) GetValue(ctx context.Context, name string) (int64, error) {
	key := fmt.Sprintf("counter:%s", name)

	// 優先從 Redis 獲取（效能優先）
	val, err := c.redis.Get(ctx, key).Int64()
	if err == nil {
		return val, nil
	}

	// Redis 失敗或 key 不存在，從 PostgreSQL 獲取
	if err == redis.Nil || c.fallbackMode.Load() {
		return c.getValuePostgresSQLc(ctx, name)
	}

	return 0, err
}

// GetMultiple 批量獲取計數器值
func (c *Counter) GetMultiple(ctx context.Context, names []string) (map[string]int64, error) {
	result := make(map[string]int64, len(names))

	// 構建 Redis keys
	keys := make([]string, len(names))
	for i, name := range names {
		keys[i] = fmt.Sprintf("counter:%s", name)
	}

	// 使用 pipeline 批量獲取
	pipe := c.redis.Pipeline()
	cmds := make([]*redis.StringCmd, len(keys))
	for i, key := range keys {
		cmds[i] = pipe.Get(ctx, key)
	}

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		// Redis 失敗，降級到 PostgreSQL
		return c.getMultiplePostgresSQLc(ctx, names)
	}

	// 收集結果
	for i, cmd := range cmds {
		val, _ := cmd.Int64()
		result[names[i]] = val
	}

	return result, nil
}

// Reset 重置計數器
func (c *Counter) Reset(ctx context.Context, name string) error {
	// 重置需要同時更新 Redis 和 PostgreSQL
	key := fmt.Sprintf("counter:%s", name)

	// 事務性重置
	pipe := c.redis.Pipeline()
	pipe.Set(ctx, key, 0, 0)

	// 清理去重集合
	location, _ := time.LoadLocation("Asia/Taipei")
	today := time.Now().In(location).Format("20060102")
	dauKey := fmt.Sprintf("counter:%s:users:%s", name, today)
	pipe.Del(ctx, dauKey)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}

	// 同步到 PostgreSQL
	return c.resetPostgresSQLc(ctx, name)
}

// batchWorker 批量寫入 worker
func (c *Counter) batchWorker() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.Counter.FlushInterval)
	defer ticker.Stop()

	batch := make([]*batchWrite, 0, c.config.Counter.BatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}

		// 合併相同計數器的操作
		merged := make(map[string]int64)
		for _, item := range batch {
			delta := item.value
			if item.operation == "decrement" {
				delta = -delta
			}
			merged[item.name] += delta
		}

		// 批量更新 PostgreSQL
		ctx := context.Background()
		for name := range merged {
			// 獲取當前 Redis 值
			key := fmt.Sprintf("counter:%s", name)
			val, _ := c.redis.Get(ctx, key).Int64()

			// 同步到 PostgreSQL
			if err := c.syncToPostgresSQLc(ctx, name, val); err != nil {
				c.logger.Error("failed to sync to postgres",
					"counter", name,
					"value", val,
					"error", err)
			}
		}

		// 清空批次
		batch = batch[:0]
	}

	for {
		select {
		case item, ok := <-c.batchBuffer:
			if !ok {
				// 通道已關閉，最後一次刷新並退出
				flush()
				return
			}
			batch = append(batch, item)
			if len(batch) >= c.config.Counter.BatchSize {
				flush()
			}

		case <-ticker.C:
			flush()
		}
	}
}

// handleRedisError 處理 Redis 錯誤
func (c *Counter) handleRedisError(err error) {
	c.logger.Error("redis error", "error", err)

	// 增加錯誤計數
	errors := c.redisErrors.Add(1)

	// 超過閾值，進入降級模式
	// 安全檢查：確保閾值在 int32 範圍內
	threshold := c.config.Counter.FallbackThreshold
	const maxInt32 = 2147483647
	if threshold > maxInt32 {
		threshold = maxInt32
	}
	// 現在可以安全地比較
	if int(errors) >= threshold {
		if !c.fallbackMode.Load() {
			c.fallbackMode.Store(true)
			c.logger.Warn("entering fallback mode due to redis errors", "errors", errors)

			// 啟動恢復檢查
			go c.checkRedisHealth()
		}
	}
}

// checkRedisHealth 檢查 Redis 健康狀態
func (c *Counter) checkRedisHealth() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := c.redis.Ping(ctx).Err()
		cancel()

		if err == nil {
			// Redis 恢復
			c.fallbackMode.Store(false)
			c.redisErrors.Store(0)
			c.logger.Info("redis recovered, exiting fallback mode")
			return
		}
	}
}

// PostgreSQL 降級方法在 postgres.go 中實作

// Shutdown 優雅關閉
func (c *Counter) Shutdown() {
	close(c.batchBuffer)
	c.wg.Wait()
}
