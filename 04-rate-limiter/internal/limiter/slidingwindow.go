package limiter

import (
	"sync"
	"time"
)

// SlidingWindow 實作滑動視窗演算法。
//
// 演算法原理：
//   1. 記錄每個請求的時間戳記
//   2. 統計滑動視窗內的請求數
//   3. 超過限制則拒絕請求
//
// 與固定視窗的差異：
//   - 固定視窗：每分鐘重置計數器，存在邊界問題
//   - 滑動視窗：視窗隨時間滑動，精確控制
//
// 邊界問題範例（固定視窗）：
//   限制 100 req/min
//   00:59 收到 100 個請求（允許）
//   01:00 計數器重置
//   01:01 收到 100 個請求（允許）
//   結果：2 秒內處理 200 個請求（超過限制！）
//
// 滑動視窗解決方案：
//   任意 1 分鐘內最多 100 個請求
//   從當前時間往回推 1 分鐘計算
//
// 優點：
//   - 精確控制流量
//   - 無邊界問題
//   - 符合直覺
//
// 缺點：
//   - 記憶體占用較高（需記錄所有請求時間）
//   - 效能較低（需遍歷清理過期請求）
//
// 適用場景：
//   - 需要精確限流
//   - QPS 不是特別高的場景
type SlidingWindow struct {
	limit      int64           // 視窗內最大請求數
	window     time.Duration   // 視窗大小
	requests   []time.Time     // 請求時間記錄
	mu         sync.Mutex
}

// NewSlidingWindow 建立新的滑動視窗限流器。
//
// 參數：
//   limit: 視窗內允許的最大請求數
//   window: 視窗大小（如 1 分鐘、1 秒）
//
// 記憶體估算：
//   假設限制 1000 req/s，視窗 1 秒
//   每個 time.Time 約 24 bytes
//   記憶體占用：1000 * 24 = 24 KB
//
// 優化建議：
//   - 若 QPS 極高，考慮使用計數器 + 分段視窗
//   - 若記憶體受限，考慮使用 Redis Sorted Set
func NewSlidingWindow(limit int64, window time.Duration) *SlidingWindow {
	return &SlidingWindow{
		limit:    limit,
		window:   window,
		requests: make([]time.Time, 0, limit),
	}
}

// Allow 檢查是否允許請求通過。
//
// 執行流程：
//   1. 清理過期請求（視窗外的請求）
//   2. 檢查當前視窗內的請求數
//   3. 未超過限制則記錄新請求
//
// 時間複雜度：
//   - 最壞情況：O(n)，需遍歷所有請求
//   - 平均情況：O(k)，k 為過期請求數
//
// 優化策略：
//   - 使用環形緩衝區避免頻繁記憶體分配
//   - 使用二分搜尋加速過期請求清理
func (sw *SlidingWindow) Allow() bool {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-sw.window)

	// 清理過期請求
	// 實作說明：找到第一個未過期的請求位置
	validIdx := 0
	for i, reqTime := range sw.requests {
		if reqTime.After(windowStart) {
			validIdx = i
			break
		}
	}

	// 移除過期請求
	// 優化：使用 slice reslice 而非逐個刪除
	if validIdx > 0 {
		sw.requests = sw.requests[validIdx:]
	}

	// 檢查是否超過限制
	if int64(len(sw.requests)) < sw.limit {
		sw.requests = append(sw.requests, now)
		return true
	}

	return false
}

// Count 返回當前視窗內的請求數（用於監控）。
func (sw *SlidingWindow) Count() int {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return len(sw.requests)
}

// SlidingWindowCounter 使用計數器優化的滑動視窗。
//
// 實作原理：
//   將視窗分為多個小區間，記錄每個區間的計數
//   計算時根據當前時間的位置，加權平均兩個區間
//
// 範例：視窗 1 分鐘，分為 60 個區間（每秒一個）
//   當前時間：10:00:30.5
//   前一個完整分鐘（09:59:31 - 10:00:30）的請求數：80
//   當前秒（10:00:30 - 10:00:31）的請求數：10
//   加權計算：80 * 0.5 + 10 = 45
//
// 優點：
//   - 記憶體占用固定（只存 N 個計數器）
//   - 效能高（O(1) 計算）
//
// 缺點：
//   - 近似演算法，非精確限流
//   - 實作較複雜
//
// Trade-off：
//   精確度 vs 效能與記憶體
//   大部分場景下，近似演算法已足夠
type SlidingWindowCounter struct {
	limit      int64
	window     time.Duration
	buckets    int           // 分桶數量
	counts     []int64       // 每個桶的計數
	timestamps []time.Time   // 每個桶的時間戳記
	mu         sync.Mutex
}

// NewSlidingWindowCounter 建立計數器優化的滑動視窗。
//
// 參數：
//   limit: 限制
//   window: 視窗大小
//   buckets: 分桶數量（建議：視窗秒數）
//
// 範例：
//   限制 1000 req/min，分 60 桶
//   每桶代表 1 秒
//   記憶體：60 * (8 + 24) = 1920 bytes
func NewSlidingWindowCounter(limit int64, window time.Duration, buckets int) *SlidingWindowCounter {
	return &SlidingWindowCounter{
		limit:      limit,
		window:     window,
		buckets:    buckets,
		counts:     make([]int64, buckets),
		timestamps: make([]time.Time, buckets),
	}
}

// Allow 檢查是否允許請求。
//
// 實作細節：
//   1. 計算當前時間所在的桶索引
//   2. 清理過期的桶
//   3. 統計視窗內的總請求數
//   4. 判斷是否超過限制
func (swc *SlidingWindowCounter) Allow() bool {
	swc.mu.Lock()
	defer swc.mu.Unlock()

	now := time.Now()
	bucketDuration := swc.window / time.Duration(swc.buckets)
	currentBucket := int(now.Unix() / int64(bucketDuration.Seconds())) % swc.buckets

	// 清理過期桶
	if !swc.timestamps[currentBucket].IsZero() {
		elapsed := now.Sub(swc.timestamps[currentBucket])
		if elapsed >= bucketDuration {
			swc.counts[currentBucket] = 0
		}
	}

	// 統計視窗內的總請求數
	var total int64
	windowStart := now.Add(-swc.window)
	for i := 0; i < swc.buckets; i++ {
		if !swc.timestamps[i].IsZero() && swc.timestamps[i].After(windowStart) {
			total += swc.counts[i]
		}
	}

	// 檢查限制
	if total < swc.limit {
		swc.counts[currentBucket]++
		swc.timestamps[currentBucket] = now
		return true
	}

	return false
}
