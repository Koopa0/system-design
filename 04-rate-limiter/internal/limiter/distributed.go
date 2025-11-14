// Package limiter 實作分散式限流器。
//
// 設計考量：
//
// 為何需要分散式限流？
//   - 單機限流無法在多實例間共享狀態
//   - 範例：限制 1000 req/s，3 個實例各自限流 → 實際 3000 req/s
//
// 為何使用 Redis + Lua？
//   - Redis：集中式狀態儲存，所有實例共享計數器
//   - Lua：保證操作原子性，避免 race condition
//
// Lua 腳本的必要性：
//   若不使用 Lua，需要多次 Redis 操作：
//     1. GET 計數器
//     2. 檢查限制
//     3. INCR 計數器
//   問題：步驟 1-3 之間可能有其他請求，導致超過限制
//
//   使用 Lua 後：
//     單次執行完整邏輯，Redis 保證原子性
//
// 效能考量：
//   - 網路延遲：每次限流需要一次 Redis 呼叫（約 1-2ms）
//   - QPS 限制：單 Redis 實例約 50K-100K ops/s
//   - 優化方案：本地快取 + 定期同步
package limiter

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// DistributedTokenBucket 分散式令牌桶限流器。
//
// 實作策略：
//   使用 Redis 儲存桶狀態：
//     - {key}:tokens - 當前令牌數
//     - {key}:last_refill - 上次填充時間（Unix 時間戳記）
//
// Lua 腳本邏輯：
//   1. 讀取當前令牌數和上次填充時間
//   2. 計算需要填充的令牌數
//   3. 更新令牌數和時間
//   4. 嘗試扣除令牌
//   5. 返回是否成功
type DistributedTokenBucket struct {
	client     *redis.Client
	capacity   int64
	refillRate int64
	script     *redis.Script
}

// Lua 腳本：令牌桶演算法
//
// KEYS[1]: 令牌計數器的 key
// ARGV[1]: 容量
// ARGV[2]: 填充速率（每秒）
// ARGV[3]: 當前時間（Unix timestamp）
//
// 返回值：
//   1: 允許請求
//   0: 拒絕請求
var tokenBucketScript = `
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

-- 取得當前狀態
local tokens = tonumber(redis.call('GET', key .. ':tokens') or capacity)
local last_refill = tonumber(redis.call('GET', key .. ':last_refill') or now)

-- 計算需要填充的令牌
local elapsed = math.max(0, now - last_refill)
local tokens_to_add = elapsed * refill_rate
tokens = math.min(capacity, tokens + tokens_to_add)

-- 嘗試扣除令牌
if tokens >= 1 then
    tokens = tokens - 1

    -- 更新 Redis
    redis.call('SET', key .. ':tokens', tokens)
    redis.call('SET', key .. ':last_refill', now)
    redis.call('EXPIRE', key .. ':tokens', 3600)
    redis.call('EXPIRE', key .. ':last_refill', 3600)

    return 1
else
    return 0
end
`

// NewDistributedTokenBucket 建立分散式令牌桶。
//
// 參數：
//   client: Redis 客戶端
//   capacity: 桶容量
//   refillRate: 填充速率（每秒）
//
// Redis 連線設定建議：
//   - PoolSize: 10-50（根據 QPS 調整）
//   - ReadTimeout: 100ms
//   - WriteTimeout: 100ms
//   - MaxRetries: 3
func NewDistributedTokenBucket(client *redis.Client, capacity, refillRate int64) *DistributedTokenBucket {
	return &DistributedTokenBucket{
		client:     client,
		capacity:   capacity,
		refillRate: refillRate,
		script:     redis.NewScript(tokenBucketScript),
	}
}

// Allow 檢查是否允許請求。
//
// 參數：
//   ctx: 上下文（用於逾時控制）
//   key: 限流 key（如 "api:/users", "ip:1.2.3.4", "user:123"）
//
// 錯誤處理策略：
//   - Redis 不可用時：建議降級允許請求（避免服務完全不可用）
//   - 網路逾時：設定合理的 context timeout（如 50ms）
//
// 監控指標：
//   - Redis 延遲
//   - 限流拒絕率
//   - Redis 錯誤率
func (dtb *DistributedTokenBucket) Allow(ctx context.Context, key string) (bool, error) {
	now := time.Now().Unix()

	result, err := dtb.script.Run(
		ctx,
		dtb.client,
		[]string{key},
		dtb.capacity,
		dtb.refillRate,
		now,
	).Int()

	if err != nil {
		// 降級策略：Redis 錯誤時允許請求
		// Trade-off: 可用性 > 精確限流
		return true, fmt.Errorf("redis error: %w", err)
	}

	return result == 1, nil
}

// DistributedSlidingWindow 分散式滑動視窗限流器。
//
// 實作策略：
//   使用 Redis Sorted Set 儲存請求時間：
//     ZADD key score member
//     score: 請求時間戳記（毫秒）
//     member: 請求 ID（可用 UUID 或時間戳記）
//
// 優點：
//   - Sorted Set 天然支援時間範圍查詢
//   - ZREMRANGEBYSCORE 自動清理過期資料
//   - ZCARD 快速計數
//
// 記憶體優化：
//   設定 TTL 自動過期
//   定期清理舊資料
type DistributedSlidingWindow struct {
	client *redis.Client
	limit  int64
	window time.Duration
	script *redis.Script
}

// Lua 腳本：滑動視窗演算法
//
// KEYS[1]: Sorted Set 的 key
// ARGV[1]: 視窗大小（秒）
// ARGV[2]: 限制數量
// ARGV[3]: 當前時間（毫秒時間戳記）
// ARGV[4]: 請求 ID
//
// 邏輯：
//   1. 移除視窗外的請求
//   2. 統計視窗內的請求數
//   3. 檢查是否超過限制
//   4. 未超過則新增當前請求
var slidingWindowScript = `
local key = KEYS[1]
local window = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local request_id = ARGV[4]

-- 計算視窗起始時間
local window_start = now - (window * 1000)

-- 移除過期請求
redis.call('ZREMRANGEBYSCORE', key, 0, window_start)

-- 統計當前請求數
local count = redis.call('ZCARD', key)

-- 檢查限制
if count < limit then
    -- 新增請求記錄
    redis.call('ZADD', key, now, request_id)
    -- 設定過期時間（視窗大小 + 緩衝）
    redis.call('EXPIRE', key, window + 60)
    return 1
else
    return 0
end
`

// NewDistributedSlidingWindow 建立分散式滑動視窗限流器。
func NewDistributedSlidingWindow(client *redis.Client, limit int64, window time.Duration) *DistributedSlidingWindow {
	return &DistributedSlidingWindow{
		client: client,
		limit:  limit,
		window: window,
		script: redis.NewScript(slidingWindowScript),
	}
}

// Allow 檢查是否允許請求。
//
// 參數：
//   ctx: 上下文
//   key: 限流 key
//   requestID: 請求唯一識別（建議使用 UUID）
//
// 為何需要 requestID？
//   - Sorted Set 的 member 必須唯一
//   - 避免同一毫秒內的請求覆蓋
func (dsw *DistributedSlidingWindow) Allow(ctx context.Context, key, requestID string) (bool, error) {
	now := time.Now().UnixMilli()
	windowSeconds := int64(dsw.window.Seconds())

	result, err := dsw.script.Run(
		ctx,
		dsw.client,
		[]string{key},
		windowSeconds,
		dsw.limit,
		now,
		requestID,
	).Int()

	if err != nil {
		return true, fmt.Errorf("redis error: %w", err)
	}

	return result == 1, nil
}

// DistributedMultiDimension 多維度限流器。
//
// 設計場景：
//   同時限制：
//     - IP 維度：每個 IP 100 req/s
//     - User 維度：每個使用者 50 req/s
//     - API 維度：整個 API 1000 req/s
//
// 實作策略：
//   串聯多個限流器，全部通過才允許請求
//
// 範例：
//   dimensions := []string{
//       fmt.Sprintf("ip:%s", clientIP),
//       fmt.Sprintf("user:%s", userID),
//       fmt.Sprintf("api:%s", apiPath),
//   }
//   limiter.Allow(ctx, dimensions)
type DistributedMultiDimension struct {
	limiters map[string]*DistributedTokenBucket
}

// NewDistributedMultiDimension 建立多維度限流器。
//
// 參數：
//   limiters: key 為維度名稱，value 為限流器
//
// 範例：
//   limiters := map[string]*DistributedTokenBucket{
//       "ip":   NewDistributedTokenBucket(client, 100, 100),
//       "user": NewDistributedTokenBucket(client, 50, 50),
//       "api":  NewDistributedTokenBucket(client, 1000, 1000),
//   }
func NewDistributedMultiDimension(limiters map[string]*DistributedTokenBucket) *DistributedMultiDimension {
	return &DistributedMultiDimension{
		limiters: limiters,
	}
}

// Allow 檢查多維度限流。
//
// 參數：
//   ctx: 上下文
//   keys: map[維度名稱]具體key
//
// 範例：
//   keys := map[string]string{
//       "ip":   "192.168.1.1",
//       "user": "user123",
//       "api":  "/api/users",
//   }
//
// 執行流程：
//   依序檢查每個維度，任一維度超限則拒絕
//
// 優化考量：
//   可並行檢查所有維度（使用 goroutine）
//   但需要處理部分成功的回滾問題
func (dmd *DistributedMultiDimension) Allow(ctx context.Context, keys map[string]string) (bool, error) {
	for dimension, limiter := range dmd.limiters {
		key, ok := keys[dimension]
		if !ok {
			continue
		}

		allowed, err := limiter.Allow(ctx, key)
		if err != nil {
			return false, err
		}

		if !allowed {
			return false, nil
		}
	}

	return true, nil
}
