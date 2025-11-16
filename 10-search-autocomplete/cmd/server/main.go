package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"10-search-autocomplete/internal"
)

var service *internal.AutocompleteService

func main() {
	// 初始化服務
	service = internal.NewAutocompleteService()

	// 加載測試數據
	loadTestData()

	// 路由
	http.HandleFunc("/api/v1/autocomplete", handleAutocomplete)
	http.HandleFunc("/api/v1/fuzzy", handleFuzzySearch)
	http.HandleFunc("/api/v1/words", handleAddWord)
	http.HandleFunc("/health", handleHealth)

	// 啟動服務
	addr := ":8080"
	log.Printf("🚀 Search Autocomplete Server starting on %s", addr)
	log.Printf("📖 Try: curl 'http://localhost:8080/api/v1/autocomplete?q=iph&limit=5'")
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// handleAutocomplete 處理自動補全請求
func handleAutocomplete(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// 解析參數
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Missing query parameter 'q'", http.StatusBadRequest)
		return
	}

	limit := 5
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// 搜尋
	results := service.Search(query, limit)

	// 響應
	latency := time.Since(start).Milliseconds()
	response := map[string]interface{}{
		"query":       query,
		"suggestions": results,
		"latency_ms":  latency,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("Autocomplete: query=%s, results=%d, latency=%dms", query, len(results), latency)
}

// handleFuzzySearch 處理模糊搜尋請求
func handleFuzzySearch(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// 解析參數
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Missing query parameter 'q'", http.StatusBadRequest)
		return
	}

	maxDistance := 2
	if distStr := r.URL.Query().Get("max_distance"); distStr != "" {
		if d, err := strconv.Atoi(distStr); err == nil && d > 0 {
			maxDistance = d
		}
	}

	limit := 5
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// 模糊搜尋
	results := service.FuzzySearch(query, maxDistance, limit)

	// 準備響應
	didYouMean := ""
	if len(results) > 0 && results[0].Distance > 0 {
		didYouMean = results[0].Word
	}

	// 響應
	latency := time.Since(start).Milliseconds()
	response := map[string]interface{}{
		"query":        query,
		"suggestions":  results,
		"did_you_mean": didYouMean,
		"latency_ms":   latency,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("FuzzySearch: query=%s, results=%d, latency=%dms", query, len(results), latency)
}

// handleAddWord 處理新增詞條請求
func handleAddWord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Word        string `json:"word"`
		SearchCount int    `json:"search_count"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Word == "" {
		http.Error(w, "Missing 'word' field", http.StatusBadRequest)
		return
	}

	if req.SearchCount <= 0 {
		req.SearchCount = 1
	}

	// 新增詞條
	service.AddWord(req.Word, req.SearchCount)

	// 響應
	response := map[string]interface{}{
		"success": true,
		"word":    req.Word,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)

	log.Printf("AddWord: word=%s, search_count=%d", req.Word, req.SearchCount)
}

// handleHealth 健康檢查
func handleHealth(w http.ResponseWriter, r *http.Request) {
	stats := service.GetStats()

	response := map[string]interface{}{
		"status": "healthy",
		"stats":  stats,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// loadTestData 加載測試數據
func loadTestData() {
	log.Println("Loading test data...")

	testData := []internal.Product{
		// iPhone 系列
		{"iphone 15 pro max", 2340000},
		{"iphone 15 pro", 1890000},
		{"iphone 15", 1560000},
		{"iphone 14 pro max", 1240000},
		{"iphone 14 pro", 980000},
		{"iphone 14", 890000},
		{"iphone 13", 670000},
		{"iphone 12", 520000},
		{"iphone 11", 380000},
		{"iphone se", 280000},
		{"iphone 充電線", 890000},
		{"iphone 手機殼", 670000},
		{"iphone 充電器", 520000},
		{"iphone 耳機", 380000},
		{"iphone 保護貼", 340000},

		// iPad 系列
		{"ipad pro", 520000},
		{"ipad air", 380000},
		{"ipad mini", 290000},
		{"ipad 保護套", 180000},

		// Samsung 系列
		{"samsung galaxy s24", 450000},
		{"samsung galaxy s23", 380000},
		{"samsung galaxy z fold", 320000},
		{"samsung 充電器", 150000},

		// 其他品牌
		{"xiaomi 14", 280000},
		{"huawei mate 60", 240000},
		{"oppo find x7", 180000},
		{"vivo x100", 160000},

		// 配件
		{"airpods pro", 670000},
		{"airpods", 520000},
		{"apple watch", 450000},
		{"macbook pro", 580000},
		{"macbook air", 490000},

		// 常見拼寫錯誤（用於測試模糊匹配）
		{"ipone", 50},  // iphone 的錯誤拼寫
		{"iphne", 30},  // iphone 的錯誤拼寫
		{"samsnug", 20}, // samsung 的錯誤拼寫
	}

	service.LoadWords(testData)

	log.Printf("✅ Loaded %d test words", len(testData))
	log.Printf("📊 Top 5 words:")
	topWords := service.GetTopWords(5)
	for i, word := range topWords {
		fmt.Printf("   %d. %s (%d searches)\n", i+1, word.Word, word.SearchCount)
	}
}
