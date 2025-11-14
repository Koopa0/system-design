package cache

import (
	"fmt"
	"sync"

	"github.com/koopa0/system-design/05-distributed-cache/pkg/consistent"
)

// DistributedCache 是分散式快取實作。
//
// 架構：
//   Client → DistributedCache → ConsistentHash → Cache Nodes
//
// 設計目標：
//   1. 支援節點動態增減
//   2. 資料分散存儲（減少單節點壓力）
//   3. 資料分布均勻（一致性雜湊）
//
// 與單機快取的差異：
//   單機：所有資料在一個節點
//   分散式：資料分散在多個節點
//
// 使用場景：
//   - 資料量超過單機記憶體
//   - 需要橫向擴展
//   - 需要高可用性（多副本）
type DistributedCache struct {
	nodes  map[string]Cache         // 節點名稱 -> 本地快取
	hash   *consistent.ConsistentHash
	mu     sync.RWMutex
}

// NewDistributedCache 建立分散式快取。
//
// 參數：
//   nodes: 節點列表（如 ["node1:11211", "node2:11211"]）
//   cacheFactory: 快取工廠函數（用於建立本地快取）
//
// 實作細節：
//   每個節點使用本地快取（LRU 或 LFU）
//   使用一致性雜湊決定 key 存放在哪個節點
func NewDistributedCache(nodeAddrs []string, cacheFactory func() Cache) *DistributedCache {
	dc := &DistributedCache{
		nodes: make(map[string]Cache),
		hash:  consistent.New(150, nil), // 150 個虛擬節點
	}

	// 建立節點
	for _, addr := range nodeAddrs {
		dc.nodes[addr] = cacheFactory()
	}

	// 將節點加入一致性雜湊環
	dc.hash.Add(nodeAddrs...)

	return dc
}

// Get 取得快取值。
//
// 執行流程：
//   1. 使用一致性雜湊找到對應節點
//   2. 從該節點的本地快取查詢
func (dc *DistributedCache) Get(key string) (interface{}, bool) {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	// 找到對應節點
	node := dc.hash.Get(key)
	if node == "" {
		return nil, false
	}

	// 從節點查詢
	cache, ok := dc.nodes[node]
	if !ok {
		return nil, false
	}

	return cache.Get(key)
}

// Set 設定快取值。
//
// 執行流程：
//   1. 使用一致性雜湊找到對應節點
//   2. 寫入該節點的本地快取
func (dc *DistributedCache) Set(key string, value interface{}) {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	// 找到對應節點
	node := dc.hash.Get(key)
	if node == "" {
		return
	}

	// 寫入節點
	if cache, ok := dc.nodes[node]; ok {
		cache.Set(key, value)
	}
}

// Delete 刪除快取值。
func (dc *DistributedCache) Delete(key string) {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	node := dc.hash.Get(key)
	if node == "" {
		return
	}

	if cache, ok := dc.nodes[node]; ok {
		cache.Delete(key)
	}
}

// AddNode 新增節點。
//
// 執行流程：
//   1. 建立新節點的本地快取
//   2. 將節點加入一致性雜湊環
//
// 資料遷移：
//   新增節點後，部分 key 會映射到新節點
//   但舊節點上的資料不會自動遷移
//   需要應用層處理資料遷移（或使用 lazy migration）
//
// Lazy Migration：
//   不主動遷移，等資料過期後自然淘汰
//   讀取時，如果舊節點有資料則返回，否則查詢資料庫
func (dc *DistributedCache) AddNode(addr string, cacheFactory func() Cache) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	// 建立新節點
	dc.nodes[addr] = cacheFactory()

	// 加入雜湊環
	dc.hash.Add(addr)
}

// RemoveNode 移除節點。
//
// 執行流程：
//   1. 從一致性雜湊環移除
//   2. 刪除節點的本地快取
//
// 資料遷移：
//   移除節點後，其資料會映射到下一個節點
//   但資料已遺失，需要從資料庫重新載入
func (dc *DistributedCache) RemoveNode(addr string) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	// 從雜湊環移除
	dc.hash.Remove(addr)

	// 刪除節點
	delete(dc.nodes, addr)
}

// Nodes 返回所有節點。
func (dc *DistributedCache) Nodes() []string {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	nodes := make([]string, 0, len(dc.nodes))
	for addr := range dc.nodes {
		nodes = append(nodes, addr)
	}
	return nodes
}

// Stats 返回統計資訊。
type CacheStats struct {
	Node string
	Size int
}

// GetStats 返回所有節點的統計資訊。
func (dc *DistributedCache) GetStats() []CacheStats {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	stats := make([]CacheStats, 0, len(dc.nodes))
	for addr, cache := range dc.nodes {
		stats = append(stats, CacheStats{
			Node: addr,
			Size: cache.Len(),
		})
	}
	return stats
}

// DistributedCacheWithReplication 是支援副本的分散式快取。
//
// 副本策略：
//   將資料複製到多個節點（如 3 個）
//   提高可用性和讀取效能
//
// Trade-offs：
//   優點：
//     - 高可用：一個節點失敗不影響服務
//     - 讀取效能：可以從任意副本讀取
//   缺點：
//     - 儲存空間增加：N 倍副本 = N 倍空間
//     - 一致性問題：副本間可能不一致
type DistributedCacheWithReplication struct {
	nodes      map[string]Cache
	hash       *consistent.ConsistentHash
	replicas   int // 副本數量
	mu         sync.RWMutex
}

// NewDistributedCacheWithReplication 建立支援副本的分散式快取。
//
// 參數：
//   nodeAddrs: 節點列表
//   cacheFactory: 快取工廠函數
//   replicas: 副本數量（建議 3）
func NewDistributedCacheWithReplication(nodeAddrs []string, cacheFactory func() Cache, replicas int) *DistributedCacheWithReplication {
	dc := &DistributedCacheWithReplication{
		nodes:    make(map[string]Cache),
		hash:     consistent.New(150, nil),
		replicas: replicas,
	}

	for _, addr := range nodeAddrs {
		dc.nodes[addr] = cacheFactory()
	}

	dc.hash.Add(nodeAddrs...)

	return dc
}

// Set 寫入資料到多個副本。
func (dc *DistributedCacheWithReplication) Set(key string, value interface{}) error {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	// 找到 N 個節點
	nodes := dc.hash.GetN(key, dc.replicas)
	if len(nodes) == 0 {
		return fmt.Errorf("no nodes available")
	}

	// 寫入所有副本
	for _, node := range nodes {
		if cache, ok := dc.nodes[node]; ok {
			cache.Set(key, value)
		}
	}

	return nil
}

// Get 從任意副本讀取資料。
//
// 策略：
//   從第一個副本讀取
//   如果失敗，嘗試下一個副本
func (dc *DistributedCacheWithReplication) Get(key string) (interface{}, bool) {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	nodes := dc.hash.GetN(key, dc.replicas)

	// 嘗試從各個副本讀取
	for _, node := range nodes {
		if cache, ok := dc.nodes[node]; ok {
			if value, found := cache.Get(key); found {
				return value, true
			}
		}
	}

	return nil, false
}

// Delete 刪除所有副本的資料。
func (dc *DistributedCacheWithReplication) Delete(key string) {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	nodes := dc.hash.GetN(key, dc.replicas)

	for _, node := range nodes {
		if cache, ok := dc.nodes[node]; ok {
			cache.Delete(key)
		}
	}
}
