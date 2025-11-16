package internal

import (
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
)

// ConsistentHash 一致性哈希（帶虛擬節點）
type ConsistentHash struct {
	ring           map[uint32]string  // 哈希值 → 物理節點地址
	sortedKeys     []uint32           // 已排序的哈希值
	nodes          map[string]bool    // 物理節點集合
	virtualNodes   int                // 每個物理節點的虛擬節點數量
	mu             sync.RWMutex
}

// NewConsistentHash 創建一致性哈希
func NewConsistentHash(virtualNodes int) *ConsistentHash {
	return &ConsistentHash{
		ring:         make(map[uint32]string),
		nodes:        make(map[string]bool),
		sortedKeys:   make([]uint32, 0),
		virtualNodes: virtualNodes,
	}
}

// AddNode 添加節點
func (ch *ConsistentHash) AddNode(nodeAddr string) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if ch.nodes[nodeAddr] {
		return // 節點已存在
	}

	// 為每個物理節點創建 N 個虛擬節點
	for i := 0; i < ch.virtualNodes; i++ {
		// 虛擬節點命名：node-A#0, node-A#1, ..., node-A#149
		virtualNodeKey := fmt.Sprintf("%s#%d", nodeAddr, i)

		// 計算虛擬節點的哈希值
		hashValue := ch.hash(virtualNodeKey)

		// 將虛擬節點映射到物理節點
		ch.ring[hashValue] = nodeAddr
		ch.sortedKeys = append(ch.sortedKeys, hashValue)
	}

	ch.nodes[nodeAddr] = true

	// 重新排序
	sort.Slice(ch.sortedKeys, func(i, j int) bool {
		return ch.sortedKeys[i] < ch.sortedKeys[j]
	})
}

// RemoveNode 移除節點
func (ch *ConsistentHash) RemoveNode(nodeAddr string) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	if !ch.nodes[nodeAddr] {
		return // 節點不存在
	}

	// 移除所有虛擬節點
	newSortedKeys := make([]uint32, 0)

	for _, hashValue := range ch.sortedKeys {
		if ch.ring[hashValue] != nodeAddr {
			newSortedKeys = append(newSortedKeys, hashValue)
		} else {
			delete(ch.ring, hashValue)
		}
	}

	ch.sortedKeys = newSortedKeys
	delete(ch.nodes, nodeAddr)
}

// GetNode 獲取 key 對應的節點
func (ch *ConsistentHash) GetNode(key string) string {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if len(ch.sortedKeys) == 0 {
		return ""
	}

	// 計算 key 的哈希值
	hashValue := ch.hash(key)

	// 二分查找：找到第一個 >= hashValue 的節點
	idx := sort.Search(len(ch.sortedKeys), func(i int) bool {
		return ch.sortedKeys[i] >= hashValue
	})

	// 如果找不到，說明 key 在最後，順時針回到第一個節點
	if idx == len(ch.sortedKeys) {
		idx = 0
	}

	// 返回物理節點地址
	return ch.ring[ch.sortedKeys[idx]]
}

// GetNodes 獲取 key 的 N 個副本節點（順時針）
func (ch *ConsistentHash) GetNodes(key string, count int) []string {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if len(ch.nodes) == 0 {
		return []string{}
	}

	if count > len(ch.nodes) {
		count = len(ch.nodes)
	}

	// 計算 key 的哈希值
	hashValue := ch.hash(key)

	// 二分查找起始位置
	idx := sort.Search(len(ch.sortedKeys), func(i int) bool {
		return ch.sortedKeys[i] >= hashValue
	})

	if idx == len(ch.sortedKeys) {
		idx = 0
	}

	// 順時針找到 count 個不同的物理節點
	result := make([]string, 0, count)
	seen := make(map[string]bool)

	for len(result) < count {
		nodeAddr := ch.ring[ch.sortedKeys[idx]]

		if !seen[nodeAddr] {
			result = append(result, nodeAddr)
			seen[nodeAddr] = true
		}

		idx = (idx + 1) % len(ch.sortedKeys)
	}

	return result
}

// GetAllNodes 獲取所有節點
func (ch *ConsistentHash) GetAllNodes() []string {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	nodes := make([]string, 0, len(ch.nodes))
	for node := range ch.nodes {
		nodes = append(nodes, node)
	}

	return nodes
}

// hash 哈希函數
func (ch *ConsistentHash) hash(key string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(key))
	return h.Sum32()
}

// GetStats 獲取統計數據
func (ch *ConsistentHash) GetStats() map[string]interface{} {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	return map[string]interface{}{
		"total_nodes":         len(ch.nodes),
		"total_virtual_nodes": len(ch.sortedKeys),
		"virtual_nodes_per_physical": ch.virtualNodes,
	}
}
