package internal

import (
	"container/heap"
	"sync"
)

// TrieNode 表示 Trie 樹的節點
type TrieNode struct {
	Children    map[rune]*TrieNode // 子節點
	IsEnd       bool                // 是否為詞的結尾
	Word        string              // 完整的詞（僅葉節點有值）
	SearchCount int                 // 搜尋次數（熱度）
}

// Trie 前綴樹
type Trie struct {
	Root *TrieNode
	mu   sync.RWMutex // 讀寫鎖保護並發訪問
	size int          // 詞的總數
}

// Product 表示一個商品/詞條
type Product struct {
	Word        string
	SearchCount int
}

// NewTrie 創建一個新的 Trie 樹
func NewTrie() *Trie {
	return &Trie{
		Root: &TrieNode{
			Children: make(map[rune]*TrieNode),
		},
		size: 0,
	}
}

// Insert 插入一個詞到 Trie 中
func (t *Trie) Insert(word string, searchCount int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	node := t.Root

	// 逐字符建立路徑
	for _, char := range word {
		if node.Children[char] == nil {
			node.Children[char] = &TrieNode{
				Children: make(map[rune]*TrieNode),
			}
		}
		node = node.Children[char]
	}

	// 標記詞的結尾
	if !node.IsEnd {
		t.size++
	}
	node.IsEnd = true
	node.Word = word
	node.SearchCount = searchCount
}

// Search 搜尋以指定前綴開頭的所有詞
func (t *Trie) Search(prefix string) []Product {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// 1. 找到前綴對應的節點
	node := t.Root
	for _, char := range prefix {
		if node.Children[char] == nil {
			return []Product{} // 前綴不存在
		}
		node = node.Children[char]
	}

	// 2. 從該節點開始，收集所有完整的詞
	results := []Product{}
	t.collectWords(node, &results)

	return results
}

// SearchTopK 搜尋以指定前綴開頭的 Top K 熱門詞
// 使用最小堆優化，避免對所有結果排序
func (t *Trie) SearchTopK(prefix string, k int) []Product {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// 1. 定位到前綴節點
	node := t.Root
	for _, char := range prefix {
		if node.Children[char] == nil {
			return []Product{}
		}
		node = node.Children[char]
	}

	// 2. 用最小堆收集 Top K
	h := &MinHeap{}
	heap.Init(h)

	t.collectTopK(node, h, k)

	// 3. 從堆中取出結果（降序）
	results := make([]Product, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		results[i] = heap.Pop(h).(Product)
	}

	return results
}

// collectWords 遞歸收集所有完整的詞
func (t *Trie) collectWords(node *TrieNode, results *[]Product) {
	if node.IsEnd {
		*results = append(*results, Product{
			Word:        node.Word,
			SearchCount: node.SearchCount,
		})
	}

	for _, child := range node.Children {
		t.collectWords(child, results)
	}
}

// collectTopK 遞歸收集 Top K 熱門詞（使用最小堆）
func (t *Trie) collectTopK(node *TrieNode, h *MinHeap, k int) {
	if node.IsEnd {
		product := Product{
			Word:        node.Word,
			SearchCount: node.SearchCount,
		}

		if h.Len() < k {
			// 堆未滿，直接插入
			heap.Push(h, product)
		} else if product.SearchCount > (*h)[0].SearchCount {
			// 新元素比堆頂大，替換堆頂
			heap.Pop(h)
			heap.Push(h, product)
		}
	}

	for _, child := range node.Children {
		t.collectTopK(child, h, k)
	}
}

// GetAllWords 獲取所有詞（用於模糊匹配）
func (t *Trie) GetAllWords() []Product {
	t.mu.RLock()
	defer t.mu.RUnlock()

	results := []Product{}
	t.collectWords(t.Root, &results)
	return results
}

// Size 返回 Trie 中詞的總數
func (t *Trie) Size() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.size
}

// Contains 檢查詞是否存在
func (t *Trie) Contains(word string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	node := t.Root
	for _, char := range word {
		if node.Children[char] == nil {
			return false
		}
		node = node.Children[char]
	}

	return node.IsEnd
}

// Delete 刪除一個詞
func (t *Trie) Delete(word string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.deleteHelper(t.Root, word, 0)
}

func (t *Trie) deleteHelper(node *TrieNode, word string, index int) bool {
	if index == len([]rune(word)) {
		if !node.IsEnd {
			return false // 詞不存在
		}
		node.IsEnd = false
		node.Word = ""
		node.SearchCount = 0
		t.size--
		return len(node.Children) == 0 // 如果沒有子節點，可以刪除
	}

	char := []rune(word)[index]
	child := node.Children[char]
	if child == nil {
		return false // 詞不存在
	}

	shouldDeleteChild := t.deleteHelper(child, word, index+1)

	if shouldDeleteChild {
		delete(node.Children, char)
		return !node.IsEnd && len(node.Children) == 0
	}

	return false
}

// MinHeap 最小堆（基於 container/heap）
// 用於維護 Top K 最大值
type MinHeap []Product

func (h MinHeap) Len() int           { return len(h) }
func (h MinHeap) Less(i, j int) bool { return h[i].SearchCount < h[j].SearchCount }
func (h MinHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *MinHeap) Push(x interface{}) {
	*h = append(*h, x.(Product))
}

func (h *MinHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}
