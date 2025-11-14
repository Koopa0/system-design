// Package limiter 實作多種限流演算法。
//
// 本包提供三種經典限流演算法的實作：
//   - Token Bucket: 支援突發流量
//   - Leaky Bucket: 平滑輸出
//   - Sliding Window: 精確計數
//
// 設計考量：
//   - 單機版使用本地記憶體（適合學習與單實例場景）
//   - 執行緒安全（使用 sync.Mutex）
//   - 高效能（避免不必要的記憶體分配）
package limiter

import (
	"sync"
	"time"
)

// TokenBucket 實作令牌桶演算法。
//
// 演算法原理：
//   1. 固定容量的桶，以固定速率填充令牌
//   2. 請求到達時，嘗試從桶中取出令牌
//   3. 有令牌則允許請求，無令牌則拒絕
//
// 優點：
//   - 支援突發流量（桶內可累積令牌）
//   - 實作簡單
//   - 記憶體占用低
//
// 缺點：
//   - 可能出現瞬間大量請求（桶滿時）
//
// 適用場景：
//   - API Gateway 限流
//   - 需要容忍短時突發的場景
type TokenBucket struct {
	capacity   int64         // 桶容量（最多存放多少令牌）
	tokens     int64         // 當前令牌數
	refillRate int64         // 填充速率（每秒填充多少令牌）
	lastRefill time.Time     // 上次填充時間
	mu         sync.Mutex    // 保護並發存取
}

// NewTokenBucket 建立新的令牌桶限流器。
//
// 參數：
//   capacity: 桶容量，決定最大突發流量
//   refillRate: 每秒填充速率，決定平均 QPS
//
// 範例：
//   limiter := NewTokenBucket(100, 10)  // 容量100，每秒填充10個
//   limiter.Allow()  // 檢查是否允許請求
func NewTokenBucket(capacity, refillRate int64) *TokenBucket {
	return &TokenBucket{
		capacity:   capacity,
		tokens:     capacity,  // 初始化時桶是滿的
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// Allow 檢查是否允許請求通過。
//
// 執行流程：
//   1. 計算距離上次填充的時間
//   2. 根據時間和速率，計算應填充的令牌數
//   3. 更新桶內令牌數（不超過容量）
//   4. 嘗試取出一個令牌
//
// 時間複雜度：O(1)
// 空間複雜度：O(1)
//
// 執行緒安全：使用 mutex 保護
func (tb *TokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	// 計算需要填充的令牌數
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)
	tokensToAdd := int64(elapsed.Seconds() * float64(tb.refillRate))

	if tokensToAdd > 0 {
		// 填充令牌，但不超過容量
		tb.tokens = min(tb.capacity, tb.tokens+tokensToAdd)
		tb.lastRefill = now
	}

	// 嘗試取出令牌
	if tb.tokens > 0 {
		tb.tokens--
		return true
	}

	return false
}

// Tokens 返回當前令牌數（用於監控）。
func (tb *TokenBucket) Tokens() int64 {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.tokens
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
