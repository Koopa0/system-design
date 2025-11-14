// Package consistent 實作一致性雜湊演算法。
//
// 一致性雜湊（Consistent Hashing）解決的問題：
//   傳統雜湊：hash(key) % N
//   問題：節點增減時，大部分資料需要重新分配
//
//   一致性雜湊：節點增減時，只有少部分資料需要重新分配
//   影響範圍：平均只有 1/N 的資料需要移動
//
// 應用場景：
//   - 分散式快取（Memcached、Redis Cluster）
//   - 分散式儲存（Cassandra、DynamoDB）
//   - 負載均衡（一致性雜湊可保持會話親和性）
package consistent

import (
	"hash/crc32"
	"sort"
	"strconv"
	"sync"
)

// Hash 是雜湊函數介面。
//
// 預設使用 CRC32，也可自訂其他雜湊函數（如 MD5、MurmurHash）
type Hash func(data []byte) uint32

// ConsistentHash 是一致性雜湊環。
//
// 資料結構：
//   - 雜湊環：0 到 2^32-1 的環狀空間
//   - 實體節點：實際的伺服器節點
//   - 虛擬節點：每個實體節點對應多個虛擬節點
//
// 為何需要虛擬節點？
//   問題：節點數量少時，資料分布可能不均勻
//   解決：每個實體節點對應多個虛擬節點（如 150 個）
//   效果：虛擬節點越多，分布越均勻，但記憶體開銷越大
//
// 範例：
//   實體節點：[node1, node2, node3]
//   虛擬節點（replicas=2）：
//     node1-0 → hash → 100
//     node1-1 → hash → 500
//     node2-0 → hash → 200
//     node2-1 → hash → 600
//     node3-0 → hash → 300
//     node3-1 → hash → 700
//   雜湊環：[100, 200, 300, 500, 600, 700]
type ConsistentHash struct {
	hash     Hash              // 雜湊函數
	replicas int               // 每個節點的虛擬節點數
	keys     []int             // 雜湊環（已排序的雜湊值）
	hashMap  map[int]string    // 雜湊值 -> 實體節點名稱
	mu       sync.RWMutex
}

// New 建立新的一致性雜湊環。
//
// 參數：
//   replicas: 每個節點的虛擬節點數（建議 150-300）
//   fn: 雜湊函數（nil 則使用 CRC32）
//
// Trade-offs：
//   虛擬節點數：
//     - 太少：資料分布不均勻
//     - 太多：記憶體開銷大、查找變慢
//     - 建議：150-300 之間
func New(replicas int, fn Hash) *ConsistentHash {
	if fn == nil {
		fn = crc32.ChecksumIEEE
	}

	return &ConsistentHash{
		hash:     fn,
		replicas: replicas,
		hashMap:  make(map[int]string),
	}
}

// Add 新增節點到雜湊環。
//
// 參數：
//   nodes: 節點名稱列表（如 "node1", "192.168.1.100:11211"）
//
// 執行流程：
//   1. 為每個節點建立 replicas 個虛擬節點
//   2. 計算虛擬節點的雜湊值
//   3. 將雜湊值加入雜湊環
//   4. 排序雜湊環
//
// 虛擬節點命名：
//   node1-0, node1-1, ..., node1-N
//   目的：確保不同虛擬節點的雜湊值分散
func (ch *ConsistentHash) Add(nodes ...string) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	for _, node := range nodes {
		// 為每個節點建立虛擬節點
		for i := 0; i < ch.replicas; i++ {
			// 虛擬節點名稱：node-i
			virtualNode := node + "-" + strconv.Itoa(i)
			hash := int(ch.hash([]byte(virtualNode)))

			ch.keys = append(ch.keys, hash)
			ch.hashMap[hash] = node
		}
	}

	// 排序雜湊環（用於二分搜尋）
	sort.Ints(ch.keys)
}

// Get 根據 key 找到對應的節點。
//
// 執行流程：
//   1. 計算 key 的雜湊值
//   2. 在雜湊環上順時針查找第一個 >= hash 的虛擬節點
//   3. 返回虛擬節點對應的實體節點
//
// 查找策略：
//   使用二分搜尋（時間複雜度 O(log N)）
//   如果找不到（超過最大值），則環繞到第一個節點
//
// 範例：
//   雜湊環：[100, 200, 300, 500]
//   key 雜湊值：250
//   查找：250 在 200 和 300 之間，順時針找到 300
func (ch *ConsistentHash) Get(key string) string {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if len(ch.keys) == 0 {
		return ""
	}

	hash := int(ch.hash([]byte(key)))

	// 二分搜尋：找到第一個 >= hash 的虛擬節點
	idx := sort.Search(len(ch.keys), func(i int) bool {
		return ch.keys[i] >= hash
	})

	// 環繞：如果超過最大值，則環繞到第一個節點
	if idx == len(ch.keys) {
		idx = 0
	}

	return ch.hashMap[ch.keys[idx]]
}

// Remove 從雜湊環移除節點。
//
// 執行流程：
//   1. 移除節點的所有虛擬節點
//   2. 重新排序雜湊環
//
// 影響：
//   該節點的資料會重新分配到順時針的下一個節點
//   平均只影響 1/N 的資料（N 為節點數）
func (ch *ConsistentHash) Remove(node string) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	// 移除所有虛擬節點
	for i := 0; i < ch.replicas; i++ {
		virtualNode := node + "-" + strconv.Itoa(i)
		hash := int(ch.hash([]byte(virtualNode)))

		// 從雜湊環移除
		idx := sort.SearchInts(ch.keys, hash)
		if idx < len(ch.keys) && ch.keys[idx] == hash {
			ch.keys = append(ch.keys[:idx], ch.keys[idx+1:]...)
		}

		// 從 hashMap 移除
		delete(ch.hashMap, hash)
	}
}

// Nodes 返回所有實體節點。
func (ch *ConsistentHash) Nodes() []string {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	nodes := make(map[string]bool)
	for _, node := range ch.hashMap {
		nodes[node] = true
	}

	result := make([]string, 0, len(nodes))
	for node := range nodes {
		result = append(result, node)
	}
	return result
}

// GetN 返回 key 對應的 N 個節點（用於副本）。
//
// 應用場景：
//   需要多副本保證可靠性
//   例如：將資料複製到 3 個節點
//
// 執行流程：
//   順時針查找 N 個不同的實體節點
func (ch *ConsistentHash) GetN(key string, n int) []string {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if len(ch.keys) == 0 || n <= 0 {
		return nil
	}

	hash := int(ch.hash([]byte(key)))
	idx := sort.Search(len(ch.keys), func(i int) bool {
		return ch.keys[i] >= hash
	})

	// 收集 N 個不同的實體節點
	seen := make(map[string]bool)
	result := make([]string, 0, n)

	for len(result) < n && len(seen) < len(ch.Nodes()) {
		if idx >= len(ch.keys) {
			idx = 0
		}

		node := ch.hashMap[ch.keys[idx]]
		if !seen[node] {
			result = append(result, node)
			seen[node] = true
		}

		idx++
	}

	return result
}

// Distribution 返回資料分布統計（用於監控）。
//
// 返回：
//   map[節點名稱]虛擬節點數量
//
// 用途：
//   檢查資料是否均勻分布
//   理想情況：每個節點的虛擬節點數量相同
func (ch *ConsistentHash) Distribution() map[string]int {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	dist := make(map[string]int)
	for _, node := range ch.hashMap {
		dist[node]++
	}
	return dist
}
