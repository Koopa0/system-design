package limiter

import (
	"sync"
	"time"
)

// LeakyBucket 實作漏桶演算法。
//
// 演算法原理：
//   1. 固定容量的桶，請求進入桶中排隊
//   2. 桶以固定速率漏出請求（處理請求）
//   3. 桶滿時拒絕新請求
//
// 與 Token Bucket 的差異：
//   - Token Bucket: 令牌以固定速率產生，請求消耗令牌
//   - Leaky Bucket: 請求進入桶中，以固定速率流出
//
// 優點：
//   - 平滑輸出流量（嚴格控制處理速率）
//   - 防止突發流量衝擊下游系統
//
// 缺點：
//   - 不支援突發流量
//   - 可能導致請求排隊延遲
//
// 適用場景：
//   - 需要嚴格控制輸出速率
//   - 保護脆弱的下游服務
//   - 訊息佇列消費限流
type LeakyBucket struct {
	capacity  int64         // 桶容量（最多排隊多少請求）
	water     int64         // 當前桶中的水量（排隊的請求數）
	leakRate  int64         // 漏出速率（每秒處理多少請求）
	lastLeak  time.Time     // 上次漏水時間
	mu        sync.Mutex
}

// NewLeakyBucket 建立新的漏桶限流器。
//
// 參數：
//   capacity: 桶容量，決定最大排隊數
//   leakRate: 漏出速率，決定處理速率（QPS）
//
// 設計選擇：
//   - 與 Token Bucket 參數類似，但語意不同
//   - capacity 代表排隊容量而非令牌數
//   - leakRate 是處理速率而非填充速率
func NewLeakyBucket(capacity, leakRate int64) *LeakyBucket {
	return &LeakyBucket{
		capacity: capacity,
		water:    0,  // 初始化時桶是空的
		leakRate: leakRate,
		lastLeak: time.Now(),
	}
}

// Allow 檢查是否允許請求通過。
//
// 執行流程：
//   1. 計算距離上次漏水的時間
//   2. 根據時間和速率，計算已漏出的水量
//   3. 更新桶中水量
//   4. 檢查是否還有空間接受新請求
//
// 實作細節：
//   - 使用 "懶惰計算" 模式，只在需要時計算漏水
//   - 避免使用背景 goroutine（節省資源）
func (lb *LeakyBucket) Allow() bool {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	// 計算已漏出的水量
	now := time.Now()
	elapsed := now.Sub(lb.lastLeak)
	leaked := int64(elapsed.Seconds() * float64(lb.leakRate))

	if leaked > 0 {
		// 減少水量（但不能為負）
		lb.water = max(0, lb.water-leaked)
		lb.lastLeak = now
	}

	// 檢查是否有空間
	if lb.water < lb.capacity {
		lb.water++
		return true
	}

	return false
}

// Water 返回當前水量（用於監控）。
func (lb *LeakyBucket) Water() int64 {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return lb.water
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
