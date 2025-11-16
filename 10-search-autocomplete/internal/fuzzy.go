package internal

import "sort"

// LevenshteinDistance 計算兩個字符串的編輯距離
// 使用動態規劃實現
// 時間複雜度：O(M × N)，M, N 為字符串長度
func LevenshteinDistance(s1, s2 string) int {
	r1, r2 := []rune(s1), []rune(s2)
	m, n := len(r1), len(r2)

	// dp[i][j] 表示 s1[0:i] 轉換到 s2[0:j] 的最小操作數
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}

	// 初始化：空字符串到 s2[0:j] 需要 j 次插入
	for i := 0; i <= m; i++ {
		dp[i][0] = i
	}
	for j := 0; j <= n; j++ {
		dp[0][j] = j
	}

	// 動態規劃
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if r1[i-1] == r2[j-1] {
				// 字符相同，無需操作
				dp[i][j] = dp[i-1][j-1]
			} else {
				// 取三種操作的最小值
				dp[i][j] = min(
					dp[i-1][j]+1,   // 刪除 s1[i-1]
					dp[i][j-1]+1,   // 插入 s2[j-1]
					dp[i-1][j-1]+1, // 替換 s1[i-1] 為 s2[j-1]
				)
			}
		}
	}

	return dp[m][n]
}

// FuzzySearchResult 模糊搜尋結果
type FuzzySearchResult struct {
	Product
	Distance int // 編輯距離
}

// FuzzySearch 模糊搜尋，返回編輯距離 <= maxDistance 的詞
func (t *Trie) FuzzySearch(query string, maxDistance int) []FuzzySearchResult {
	t.mu.RLock()
	defer t.mu.RUnlock()

	results := []FuzzySearchResult{}

	// 遍歷所有詞，計算編輯距離
	// 注意：這是暴力方法，適用於小規模數據
	// 生產環境建議使用 BK-Tree 或限制在 Top 詞中搜尋
	t.fuzzySearchHelper(t.Root, query, maxDistance, &results)

	// 按編輯距離排序（距離小的優先）
	sort.Slice(results, func(i, j int) bool {
		if results[i].Distance == results[j].Distance {
			// 距離相同，按搜尋次數降序
			return results[i].SearchCount > results[j].SearchCount
		}
		return results[i].Distance < results[j].Distance
	})

	return results
}

func (t *Trie) fuzzySearchHelper(node *TrieNode, query string, maxDistance int, results *[]FuzzySearchResult) {
	if node.IsEnd {
		distance := LevenshteinDistance(query, node.Word)
		if distance <= maxDistance {
			*results = append(*results, FuzzySearchResult{
				Product: Product{
					Word:        node.Word,
					SearchCount: node.SearchCount,
				},
				Distance: distance,
			})
		}
	}

	for _, child := range node.Children {
		t.fuzzySearchHelper(child, query, maxDistance, results)
	}
}

// FuzzySearchTopWords 在熱門詞中模糊搜尋（優化版本）
// 只在 Top N 個熱門詞中搜尋，避免遍歷所有詞
func FuzzySearchTopWords(topWords []Product, query string, maxDistance int, limit int) []FuzzySearchResult {
	results := []FuzzySearchResult{}

	for _, word := range topWords {
		distance := LevenshteinDistance(query, word.Word)
		if distance <= maxDistance {
			results = append(results, FuzzySearchResult{
				Product: word,
				Distance: distance,
			})
		}
	}

	// 排序
	sort.Slice(results, func(i, j int) bool {
		if results[i].Distance == results[j].Distance {
			return results[i].SearchCount > results[j].SearchCount
		}
		return results[i].Distance < results[j].Distance
	})

	// 限制返回數量
	if len(results) > limit {
		results = results[:limit]
	}

	return results
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
