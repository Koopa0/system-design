package cache

import (
	"container/list"
	"sync"
)

// LFU 實作 Least Frequently Used 快取淘汰演算法。
//
// 演算法原理：
//   使用頻率最低的資料最先被淘汰
//   頻率相同時，淘汰最久未使用的（LRU 作為 tie-breaker）
//
// 資料結構：
//   - HashMap: key -> 節點資訊（value, 頻率）
//   - 頻率桶: 每個頻率對應一個 LRU 鏈表
//   - minFreq: 追蹤當前最小頻率
//
// 時間複雜度：
//   - Get: O(1)
//   - Put: O(1)
//
// 空間複雜度：O(n)
//
// 適用場景：
//   - 熱點資料明確
//   - 存取模式穩定
//   - 需要防止快取污染（一次性大量存取不會影響熱點資料）
//
// 優點：
//   - 考慮存取頻率，更精確
//   - 防止快取污染
//   - 熱點資料保護好
//
// 缺點：
//   - 實作較複雜
//   - 冷啟動問題（新資料頻率低，容易被淘汰）
//   - 空間開銷較大（需維護頻率資訊）
//
// LRU vs LFU 選擇：
//   - 存取模式穩定：LFU 更好
//   - 存取模式多變：LRU 更好
//   - 防止突發流量：LFU 更好
//   - 實作簡單：LRU 更好
type LFU struct {
	capacity int                       // 容量
	minFreq  int                       // 當前最小頻率
	cache    map[string]*lfuNode       // key -> 節點
	freqMap  map[int]*list.List        // 頻率 -> LRU 鏈表
	mu       sync.RWMutex
}

// lfuNode 是 LFU 快取節點。
type lfuNode struct {
	key   string
	value interface{}
	freq  int            // 存取頻率
	elem  *list.Element  // 在頻率鏈表中的位置
}

// NewLFU 建立新的 LFU 快取。
//
// 實作細節：
//   使用頻率分桶策略：
//     - 每個頻率維護一個 LRU 鏈表
//     - 相同頻率的項目按 LRU 順序排列
//     - minFreq 追蹤最小頻率，加速淘汰
//
// 範例：
//   freq=1: [key1, key2, key3]  // 存取 1 次的項目
//   freq=2: [key4, key5]        // 存取 2 次的項目
//   freq=5: [key6]              // 存取 5 次的項目
func NewLFU(capacity int) *LFU {
	return &LFU{
		capacity: capacity,
		minFreq:  0,
		cache:    make(map[string]*lfuNode),
		freqMap:  make(map[int]*list.List),
	}
}

// Get 取得快取值。
//
// 行為：
//   1. 查找項目
//   2. 增加頻率（從舊頻率鏈表移到新頻率鏈表）
//   3. 更新 minFreq
func (lfu *LFU) Get(key string) (interface{}, bool) {
	lfu.mu.Lock()
	defer lfu.mu.Unlock()

	node, ok := lfu.cache[key]
	if !ok {
		return nil, false
	}

	// 增加頻率
	lfu.increaseFreq(node)
	return node.value, true
}

// Put 設定快取值。
//
// 行為：
//   1. 如果 key 已存在：
//      - 更新值
//      - 增加頻率
//   2. 如果 key 不存在：
//      - 容量已滿：淘汰頻率最低且最久未使用的項目
//      - 新增項目（頻率=1）
func (lfu *LFU) Set(key string, value interface{}) {
	lfu.mu.Lock()
	defer lfu.mu.Unlock()

	if lfu.capacity <= 0 {
		return
	}

	// 如果 key 已存在，更新值並增加頻率
	if node, ok := lfu.cache[key]; ok {
		node.value = value
		lfu.increaseFreq(node)
		return
	}

	// 容量已滿，淘汰
	if len(lfu.cache) >= lfu.capacity {
		lfu.evict()
	}

	// 新增項目（頻率=1）
	node := &lfuNode{
		key:   key,
		value: value,
		freq:  1,
	}

	// 加入頻率=1 的鏈表
	if lfu.freqMap[1] == nil {
		lfu.freqMap[1] = list.New()
	}
	node.elem = lfu.freqMap[1].PushFront(node)
	lfu.cache[key] = node
	lfu.minFreq = 1
}

// increaseFreq 增加節點的頻率。
//
// 執行流程：
//   1. 從舊頻率鏈表中移除
//   2. 頻率 +1
//   3. 加入新頻率鏈表
//   4. 更新 minFreq
func (lfu *LFU) increaseFreq(node *lfuNode) {
	oldFreq := node.freq

	// 從舊頻率鏈表移除
	lfu.freqMap[oldFreq].Remove(node.elem)

	// 如果舊頻率鏈表為空且是最小頻率，更新 minFreq
	if lfu.freqMap[oldFreq].Len() == 0 {
		delete(lfu.freqMap, oldFreq)
		if lfu.minFreq == oldFreq {
			lfu.minFreq++
		}
	}

	// 頻率 +1
	node.freq++

	// 加入新頻率鏈表
	if lfu.freqMap[node.freq] == nil {
		lfu.freqMap[node.freq] = list.New()
	}
	node.elem = lfu.freqMap[node.freq].PushFront(node)
}

// evict 淘汰頻率最低且最久未使用的項目。
//
// 執行流程：
//   1. 找到 minFreq 的鏈表
//   2. 移除鏈表尾部項目（該頻率中最久未使用）
//   3. 從 cache 中刪除
//
// 為何從尾部移除？
//   每個頻率的鏈表是 LRU 順序
//   頭部是最近使用，尾部是最久未使用
func (lfu *LFU) evict() {
	minFreqList := lfu.freqMap[lfu.minFreq]
	if minFreqList == nil || minFreqList.Len() == 0 {
		return
	}

	// 移除尾部項目（最久未使用）
	elem := minFreqList.Back()
	if elem != nil {
		node := elem.Value.(*lfuNode)
		minFreqList.Remove(elem)
		delete(lfu.cache, node.key)

		// 清理空鏈表
		if minFreqList.Len() == 0 {
			delete(lfu.freqMap, lfu.minFreq)
			// 修復：更新 minFreq 到下一個最小頻率
			//
			// 問題：如果不更新，下次 evict() 時會查找不存在的頻率
			//
			// 解決：找到 freqMap 中的最小鍵
			// 注意：教學簡化，不追蹤 minFreq 變化（下次訪問時會自動更正）
			// 生產環境可用 heap 優化
			lfu.minFreq++
			// 註：這裡簡化為 minFreq++，實際上可能需要遍歷找到真正的最小值
			// 但在大多數情況下，minFreq++ 是正確的（頻率連續）
		}
	}
}

// Delete 刪除快取項目。
func (lfu *LFU) Delete(key string) {
	lfu.mu.Lock()
	defer lfu.mu.Unlock()

	if node, ok := lfu.cache[key]; ok {
		lfu.freqMap[node.freq].Remove(node.elem)
		delete(lfu.cache, key)

		// 清理空鏈表
		if lfu.freqMap[node.freq].Len() == 0 {
			delete(lfu.freqMap, node.freq)
		}
	}
}

// Len 返回當前快取項目數量。
func (lfu *LFU) Len() int {
	lfu.mu.RLock()
	defer lfu.mu.RUnlock()
	return len(lfu.cache)
}

// Clear 清空快取。
func (lfu *LFU) Clear() {
	lfu.mu.Lock()
	defer lfu.mu.Unlock()

	lfu.cache = make(map[string]*lfuNode)
	lfu.freqMap = make(map[int]*list.List)
	lfu.minFreq = 0
}

// Stats 返回快取統計資訊（用於監控）。
type LFUStats struct {
	Size    int            // 當前項目數
	MinFreq int            // 最小頻率
	FreqDist map[int]int   // 頻率分布（頻率 -> 項目數）
}

// GetStats 返回統計資訊。
func (lfu *LFU) GetStats() LFUStats {
	lfu.mu.RLock()
	defer lfu.mu.RUnlock()

	stats := LFUStats{
		Size:     len(lfu.cache),
		MinFreq:  lfu.minFreq,
		FreqDist: make(map[int]int),
	}

	for freq, lst := range lfu.freqMap {
		stats.FreqDist[freq] = lst.Len()
	}

	return stats
}
