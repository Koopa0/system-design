package internal

import (
	"sort"
	"sync"
)

// AutocompleteService 自動補全服務
type AutocompleteService struct {
	trie      *Trie
	topWords  []Product // 緩存的熱門詞（用於模糊匹配優化）
	mu        sync.RWMutex
}

// NewAutocompleteService 創建自動補全服務
func NewAutocompleteService() *AutocompleteService {
	return &AutocompleteService{
		trie:     NewTrie(),
		topWords: []Product{},
	}
}

// AddWord 新增或更新詞條
func (s *AutocompleteService) AddWord(word string, searchCount int) {
	s.trie.Insert(word, searchCount)
	s.refreshTopWords()
}

// Search 搜尋自動補全建議
func (s *AutocompleteService) Search(prefix string, limit int) []Product {
	if limit <= 0 {
		limit = 5
	}

	// 使用 Top K 優化的搜尋
	return s.trie.SearchTopK(prefix, limit)
}

// FuzzySearch 模糊搜尋（拼寫糾正）
func (s *AutocompleteService) FuzzySearch(query string, maxDistance int, limit int) []FuzzySearchResult {
	if maxDistance <= 0 {
		maxDistance = 2
	}
	if limit <= 0 {
		limit = 5
	}

	// 先嘗試精確匹配
	exactResults := s.Search(query, limit)
	if len(exactResults) > 0 {
		// 找到精確匹配，不需要模糊搜尋
		results := make([]FuzzySearchResult, len(exactResults))
		for i, p := range exactResults {
			results[i] = FuzzySearchResult{
				Product:  p,
				Distance: 0,
			}
		}
		return results
	}

	// 找不到精確匹配，使用模糊搜尋
	// 優化：只在 Top 1000 個熱門詞中搜尋
	s.mu.RLock()
	topWords := s.topWords
	if len(topWords) > 1000 {
		topWords = topWords[:1000]
	}
	s.mu.RUnlock()

	return FuzzySearchTopWords(topWords, query, maxDistance, limit)
}

// refreshTopWords 刷新熱門詞緩存
func (s *AutocompleteService) refreshTopWords() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 獲取所有詞並按搜尋次數排序
	allWords := s.trie.GetAllWords()
	sort.Slice(allWords, func(i, j int) bool {
		return allWords[i].SearchCount > allWords[j].SearchCount
	})

	s.topWords = allWords
}

// GetStats 獲取統計信息
func (s *AutocompleteService) GetStats() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"total_words": s.trie.Size(),
		"top_words_cached": len(s.topWords),
	}
}

// LoadWords 批量加載詞條
func (s *AutocompleteService) LoadWords(words []Product) {
	for _, word := range words {
		s.trie.Insert(word.Word, word.SearchCount)
	}
	s.refreshTopWords()
}

// GetTopWords 獲取 Top K 熱門詞
func (s *AutocompleteService) GetTopWords(k int) []Product {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if k > len(s.topWords) {
		k = len(s.topWords)
	}

	result := make([]Product, k)
	copy(result, s.topWords[:k])
	return result
}
