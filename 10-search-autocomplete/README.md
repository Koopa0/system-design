# Search Autocomplete

搜尋自動補全系統，使用 Trie 樹（前綴樹）實現高性能的前綴匹配和 Top K 熱門建議。

## 功能特性

- **前綴匹配**：Trie 樹實現 O(M) 複雜度查詢（M = 前綴長度）
- **Top K 優化**：最小堆維護熱門建議，避免全量排序
- **模糊匹配**：Levenshtein Distance 拼寫糾正（編輯距離 ≤ 2）
- **內存優化**：Radix Tree 壓縮單子節點路徑
- **並發安全**：讀寫鎖保護 Trie 結構

## 快速開始

```bash
# 啟動服務
go run cmd/server/main.go

# 服務運行在 http://localhost:8080
```

## API 文檔

### 1. 搜尋自動補全

**請求：**
```bash
GET /api/v1/autocomplete?q=iph&limit=5
```

**參數：**
- `q` (必填): 搜尋前綴
- `limit` (選填): 返回結果數量，默認 5

**響應：**
```json
{
  "query": "iph",
  "suggestions": [
    {
      "text": "iphone 15 pro max",
      "search_count": 2340000
    },
    {
      "text": "iphone 充電線",
      "search_count": 890000
    },
    {
      "text": "iphone 手機殼",
      "search_count": 670000
    }
  ],
  "latency_ms": 3
}
```

### 2. 新增/更新詞條

**請求：**
```bash
POST /api/v1/words
Content-Type: application/json

{
  "word": "iphone 16",
  "search_count": 1000
}
```

**響應：**
```json
{
  "success": true,
  "word": "iphone 16"
}
```

### 3. 模糊搜尋（拼寫糾正）

**請求：**
```bash
GET /api/v1/fuzzy?q=ipone&max_distance=2
```

**參數：**
- `q` (必填): 錯誤拼寫的詞
- `max_distance` (選填): 最大編輯距離，默認 2

**響應：**
```json
{
  "query": "ipone",
  "suggestions": [
    {
      "text": "iphone 15 pro max",
      "search_count": 2340000,
      "distance": 1
    }
  ],
  "did_you_mean": "iphone"
}
```

### 4. 健康檢查

**請求：**
```bash
GET /health
```

**響應：**
```json
{
  "status": "healthy",
  "words_count": 125430,
  "memory_mb": 85
}
```

## 性能指標

### 單機性能

```
配置：
- CPU: 4 核
- 內存: 8 GB
- 商品數：100 萬

性能：
- 查詢延遲：P99 < 5ms
- QPS：5,000
- 內存占用：約 3.5 GB
```

### 複雜度分析

| 操作 | 時間複雜度 | 空間複雜度 |
|------|-----------|-----------|
| 插入 | O(M) | O(M) |
| 前綴查詢 | O(M + K) | O(1) |
| Top K 查詢 | O(N log K) | O(K) |
| 模糊匹配 | O(N × M²) | O(1) |

其中：
- M = 詞的長度
- K = 返回結果數量
- N = 總詞數

## 核心實現

### Trie 樹結構

```go
type TrieNode struct {
    Children    map[rune]*TrieNode
    IsEnd       bool
    Word        string
    SearchCount int
}
```

### Top K 最小堆

```go
type MinHeap []Product

func (h MinHeap) Less(i, j int) bool {
    return h[i].SearchCount < h[j].SearchCount
}
```

### 編輯距離（Levenshtein Distance）

```go
func levenshteinDistance(s1, s2 string) int {
    // 動態規劃實現
    // 時間複雜度：O(M × N)
}
```

## 使用範例

### Go 客戶端

```go
package main

import (
    "encoding/json"
    "fmt"
    "net/http"
)

func main() {
    // 搜尋自動補全
    resp, _ := http.Get("http://localhost:8080/api/v1/autocomplete?q=iph&limit=5")
    defer resp.Body.Close()

    var result struct {
        Suggestions []struct {
            Text        string `json:"text"`
            SearchCount int    `json:"search_count"`
        } `json:"suggestions"`
    }

    json.NewDecoder(resp.Body).Decode(&result)

    for _, s := range result.Suggestions {
        fmt.Printf("%s (%d 次搜尋)\n", s.Text, s.SearchCount)
    }
}
```

### cURL

```bash
# 搜尋自動補全
curl "http://localhost:8080/api/v1/autocomplete?q=iph&limit=5"

# 新增詞條
curl -X POST http://localhost:8080/api/v1/words \
  -H "Content-Type: application/json" \
  -d '{"word": "iphone 16", "search_count": 1000}'

# 模糊搜尋
curl "http://localhost:8080/api/v1/fuzzy?q=ipone&max_distance=2"
```

## 設計決策

詳細的設計思路請參考 [DESIGN.md](./DESIGN.md)，包括：

1. **為什麼選擇 Trie 樹？** - 從 SQL LIKE 到 Trie 的演進
2. **Top K 優化** - 最小堆 vs 全量排序
3. **內存優化** - Radix Tree 壓縮
4. **模糊匹配** - 編輯距離算法
5. **擴展性** - Redis 分片、Elasticsearch

## 生產環境建議

本實現為教學用途，生產環境需要考慮：

### 1. 持久化

```go
// 定期保存快照
func (t *Trie) SaveSnapshot(path string) error {
    data, _ := t.Marshal()  // 使用 gob 或 protobuf
    return os.WriteFile(path, data, 0644)
}

// 啟動時加載
func LoadSnapshot(path string) (*Trie, error) {
    data, _ := os.ReadFile(path)
    trie := &Trie{}
    trie.Unmarshal(data)
    return trie, nil
}
```

### 2. 分布式擴展

```
架構選擇：
- < 1,000 萬詞：單機 Trie ✅（本實現）
- 1,000 萬 - 1 億詞：Redis + 分片
- > 1 億詞：Elasticsearch
```

### 3. 監控指標

```go
// Prometheus metrics
var (
    searchLatency = prometheus.NewHistogram(...)
    searchQPS = prometheus.NewCounter(...)
    trieSize = prometheus.NewGauge(...)
)
```

### 4. 安全性

```go
// 限流
if !rateLimiter.Allow(userID) {
    return errors.New("rate limit exceeded")
}

// 輸入驗證
if len(query) > 100 {
    return errors.New("query too long")
}
```

## 測試

```bash
# 單元測試
go test ./internal/...

# 性能測試
go test -bench=. ./internal/trie

# 壓力測試
ab -n 10000 -c 100 "http://localhost:8080/api/v1/autocomplete?q=iph"
```

## 相關閱讀

- [DESIGN.md](./DESIGN.md) - 完整的設計文檔
- [Trie 數據結構](https://en.wikipedia.org/wiki/Trie)
- [Levenshtein Distance](https://en.wikipedia.org/wiki/Levenshtein_distance)
- [Google Autocomplete](https://blog.google/products/search/how-google-autocomplete-predictions-work/)

## License

MIT License
