// Package internal 實現計數服務的核心功能
//
// 系統設計問題：
//
//	如何支持 10,000 QPS 的高並發計數更新，同時保證準確性和可靠性？
//
// 核心挑戰：
//  1. 高頻寫入：每秒數萬次計數操作（在線人數、DAU）
//  2. 準確性：併發環境下不能丟失計數
//  3. 去重計數：同一用戶一天只計算一次（DAU）
//  4. 高可用：Redis 故障不能影響服務
//  5. 低延遲：P99 < 10ms
//
// 設計方案：
//
//	✅ Redis + PostgreSQL 雙寫
//	✅ 批量異步同步（降低 DB 壓力）
//	✅ 降級機制（Redis 故障時用 PostgreSQL）
//	✅ Redis Set 去重（SADD 原子操作）
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

// Counter 計數器核心實現
//
// 架構設計：
//
//	Client → API → Redis (快速返回) → 批量寫入 PostgreSQL
//	                  ↓ 故障
//	             PostgreSQL (降級模式)
//
// 系統設計考量：
//
//  1. 為什麼雙寫（Redis + PostgreSQL）？
//     - Redis：內存操作，微秒級延遲，支持原子 INCR
//     - PostgreSQL：持久化，重啟不丟數據
//     - 權衡：最終一致性（數秒內同步）vs 強一致性
//
//  2. 為什麼批量寫入？
//     - 問題：每次 INCR 都寫 DB，10,000 QPS 無法承受
//     - 方案：緩衝合併，每秒批量同步
//     - 效果：10,000 → 100 次 DB 寫入（降低 100 倍）
//
//  3. 為什麼需要降級？
//     - 場景：Redis 故障（網絡問題、OOM、重啟）
//     - 影響：如果不降級，服務完全不可用
//     - 方案：自動切換到 PostgreSQL（犧牲性能保可用）
//
//  4. 容量規劃：
//     - 批量大小：100-1000（平衡延遲與吞吐）
//     - 刷新間隔：1-5 秒（根據一致性要求）
//     - 錯誤閾值：3-5 次（避免頻繁切換）
type Counter struct {
	redis   *redis.Client
	pg      *pgxpool.Pool
	queries sqlc.Querier
	config  *Config
	logger  *slog.Logger

	// 降級模式狀態
	fallbackMode atomic.Bool  // Redis 故障時自動切換
	redisErrors  atomic.Int32 // 錯誤計數（超閾值觸發降級）

	// 批量寫入緩衝
	batchBuffer chan *batchWrite // 異步同步通道
	wg          sync.WaitGroup   // 等待 worker 退出

	// 教學簡化：記憶體快取未實現
	// 生產環境可添加 in-memory cache 減少 PostgreSQL 壓力
	// 實現建議：LRU cache with TTL
	// cache *MemoryCache

	recoverySignal chan struct{}
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
		redis:          redis,
		pg:             pg,
		queries:        sqlc.New(pg),
		config:         config,
		logger:         logger,
		batchBuffer:    make(chan *batchWrite, config.Counter.BatchSize*2),
		recoverySignal: make(chan struct{}, 1),
	}

	// 教學簡化：記憶體快取未實現
	// 生產環境若需要可在此處初始化 in-memory cache
	// 用於降級模式時減少 PostgreSQL 壓力
	// if config.Counter.EnableMemoryCache {
	//     c.cache = NewMemoryCache(config.Counter.CacheSize, config.Counter.CacheTTL)
	// }

	// 設定預設 DAU 計數模式
	if config.Counter.DAUCountMode == "" {
		config.Counter.DAUCountMode = "exact" // 預設使用精確計數以保持向後相容
	}

	// 啟動批量寫入 worker
	c.wg.Add(1)
	go c.batchWorker()

	// 啟動恢復 worker（使用 sqlc）
	c.startRecoveryWorkerSQLc()

	return c
}

// Increment 增加計數器
//
// 系統設計重點：
//
//  1. 去重計數（DAU 統計）：
//     問題：同一用戶一天內多次登入，只能計算一次
//     方案：Redis Set (SADD) - 原子操作，天然去重
//     key: counter:{name}:users:{date}
//     TTL: 次日凌晨（自動清理，節省內存）
//
//  2. 原子性保證：
//     Redis INCR 是原子操作（單線程模型）
//     即使 10,000 個併發請求，也不會丟失計數
//
// 3. 性能優化：
//   - Redis 返回後立即響應（< 1ms）
//   - PostgreSQL 異步批量同步（不阻塞）
//   - 緩衝區滿時降級為同步（背壓機制）
//
// 4. 高可用：
//   - Redis 故障自動切換 PostgreSQL
//   - 犧牲性能（毫秒 → 數十毫秒）換取可用性
func (c *Counter) Increment(ctx context.Context, name string, value int64, userID string) (int64, error) {
	location, _ := time.LoadLocation("Asia/Taipei")
	today := time.Now().In(location).Format("20060102")

	// 降級模式檢查
	//
	// 已知限制：
	//   - 降級模式下，DAU 去重邏輯被繞過
	//   - 原因：DAU 去重依賴 Redis SADD，PostgreSQL 無對應實作
	//   - 影響：Redis 故障期間，同一用戶可能被重複計數
	//   - 緩解方案：
	//     1. 在 PostgreSQL 實作 DAU 去重表（user_id + date 唯一索引）
	//     2. 使用應用層記憶體快取暫存已計數的用戶
	//     3. 接受短暫的不準確性（Redis 恢復後自動修正）
	//   - Trade-off：完全實作需要額外的表結構和複雜度
	if c.fallbackMode.Load() {
		return c.incrementPostgresSQLc(ctx, name, value)
	}

	// 去重計數邏輯（DAU）
	if userID != "" {
		dauKey := fmt.Sprintf("counter:%s:users:%s", name, today)

		// SADD 返回新增的元素數量（0 = 已存在，1 = 新增）
		added, err := c.redis.SAdd(ctx, dauKey, userID).Result()
		if err != nil {
			c.handleRedisError(err)
			return 0, err
		}

		// 設置 TTL（次日凌晨自動刪除）
		//
		// 系統設計考量：
		//   - 為什麼要設置 TTL？
		//     → 自動清理過期數據（節省內存）
		//     → 避免手動清理的複雜性
		//   - 為什麼是次日凌晨？
		//     → DAU 統計以自然日為單位
		//     → 自動對齊業務邏輯
		if added > 0 {
			tomorrow := time.Now().In(location).AddDate(0, 0, 1)
			midnight := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, location)
			if err := c.redis.ExpireAt(ctx, dauKey, midnight).Err(); err != nil {
				// TTL 設置失敗不影響計數邏輯
				// 最壞情況：該 key 不會自動過期，需要定期清理
				c.logger.Warn("failed to set DAU TTL", "key", dauKey, "error", err)
			}
			value = added
		} else {
			// 用戶今天已計數，直接返回當前值
			return c.GetValue(ctx, name)
		}
	}

	// Redis 原子操作（INCR/INCRBY）
	key := fmt.Sprintf("counter:%s", name)
	newVal, err := c.redis.IncrBy(ctx, key, value).Result()
	if err != nil {
		c.handleRedisError(err)
		return c.incrementPostgresSQLc(ctx, name, value)
	}

	// 異步同步到 PostgreSQL（最終一致性）
	//
	// 已知限制：
	//   - DAU 計數器使用 batch merging 時，PostgreSQL 同步可能不準確
	//   - 原因：batch worker 按計數器名稱合併，不保留每個用戶的操作
	//   - 影響：極端情況下，同一批次內的多個用戶操作可能被合併
	//   - 緩解：Redis 層的 DAU 統計始終準確（SADD 原子性）
	//   - 建議：對 DAU 計數器禁用批處理，或使用專用的 DAU 服務
	select {
	case c.batchBuffer <- &batchWrite{
		name:      name,
		operation: "increment",
		value:     value,
		userID:    userID,
		timestamp: time.Now(),
	}:
		// 成功加入批量隊列
	default:
		// 緩衝區滿（背壓），同步寫入
		//
		// 系統設計考量：
		//   - 為什麼不啟動 goroutine？
		//     → 防止無限制的 goroutine 創建（資源耗盡）
		//     → 同步寫入產生自然背壓（保護系統）
		//   - Trade-off：
		//     → 延遲增加（P99 可能達到 50-100ms）
		//     → 換取系統穩定性（避免 OOM）
		if err := c.syncToPostgresSQLc(ctx, name, newVal); err != nil {
			c.logger.Error("sync to postgres failed during backpressure",
				"counter", name,
				"value", newVal,
				"error", err)
		}
	}

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
		// 緩衝區滿（背壓），同步寫入（同 Increment）
		c.syncToPostgresSQLc(ctx, name, newVal)
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

// batchWorker 批量寫入 worker（後台 goroutine）
//
// 系統設計重點：
//
// 1. 為什麼批量寫入？
//
//   - 問題：10,000 QPS 直接寫 DB → PostgreSQL 無法承受
//
//   - 方案：緩衝聚合，定期批量刷新
//
//   - 效果：10,000 次操作 → 100 次 DB 寫入
//
//     2. 刷新策略（兩種觸發條件）：
//     a) 批量大小：達到閾值（如 100）立即刷新
//     b) 定時器：每隔固定時間（如 1 秒）刷新
//     → 平衡延遲與吞吐：大批量提高效率，定時器保證延遲上限
//
//     3. 操作合併：
//     同一計數器的多次操作合併為一次 DB 更新
//     例：counter:online +1, +1, -1 → 最終 +1
//
// 4. 容量規劃：
//   - 批量大小 100：平衡內存與效率
//   - 刷新間隔 1 秒：最終一致性延遲 < 1 秒
//   - 緩衝區 200：允許突發流量（2x 批量大小）
func (c *Counter) batchWorker() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.Counter.FlushInterval)
	defer ticker.Stop()

	batch := make([]*batchWrite, 0, c.config.Counter.BatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}

		// 合併同一計數器的操作（減少 DB 寫入）
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
			key := fmt.Sprintf("counter:%s", name)
			val, _ := c.redis.Get(ctx, key).Int64()

			if err := c.syncToPostgresSQLc(ctx, name, val); err != nil {
				c.logger.Error("failed to sync to postgres",
					"counter", name,
					"value", val,
					"error", err)
			}
		}

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

// handleRedisError 處理 Redis 錯誤（降級機制）
//
// 系統設計：降級策略
//
// 1. 為什麼需要降級？
//   - 場景：Redis 故障（網絡問題、OOM、重啟、主從切換）
//   - 影響：如果不處理，服務完全不可用（計數功能掛掉）
//   - 方案：自動切換到 PostgreSQL
//
// 2. 降級觸發條件：
//   - 連續錯誤次數達到閾值（如 3-5 次）
//   - 避免單次偶發錯誤觸發（網絡抖動）
//   - 避免頻繁切換（影響穩定性）
//
// 3. 降級代價：
//   - 性能下降：Redis < 1ms → PostgreSQL 10-50ms
//   - 吞吐下降：Redis 10,000 QPS → PostgreSQL 1,000 QPS
//   - 但保證可用：降級總比掛掉好
//
// 4. 自動恢復：
//   - 後台定期健康檢查（每 10 秒 Ping Redis）
//   - Redis 恢復後自動切回（重置錯誤計數）
func (c *Counter) handleRedisError(err error) {
	c.logger.Error("redis error", "error", err)

	// 累加錯誤計數
	errors := c.redisErrors.Add(1)

	threshold := c.config.Counter.FallbackThreshold
	const maxInt32 = 2147483647
	if threshold > maxInt32 {
		threshold = maxInt32
	}

	// 達到閾值，觸發降級
	if int(errors) >= threshold {
		if !c.fallbackMode.Load() {
			c.fallbackMode.Store(true)
			c.logger.Warn("entering fallback mode due to redis errors", "errors", errors)

			// 啟動健康檢查（自動恢復）
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
