package main

import (
	"13-distributed-kv-store/internal"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

var (
	kvStore *internal.DistributedKVStore
)

func main() {
	// 命令行參數
	nodeID := flag.String("id", "node-1", "Node ID")
	port := flag.Int("port", 8080, "HTTP port")
	seeds := flag.String("seeds", "", "Seed nodes (comma-separated, e.g., node-2:8081,node-3:8082)")
	n := flag.Int("n", 3, "Number of replicas")
	w := flag.Int("w", 2, "Write quorum")
	r := flag.Int("r", 2, "Read quorum")
	flag.Parse()

	nodeAddr := fmt.Sprintf("localhost:%d", *port)

	// 創建 Quorum 配置
	config := &internal.QuorumConfig{
		N: *n,
		W: *w,
		R: *r,
	}

	// 創建 KV Store
	kvStore = internal.NewDistributedKVStore(*nodeID, nodeAddr, config)

	// 添加種子節點
	if *seeds != "" {
		// 簡化處理，實際應該解析 comma-separated 字符串
		// seedList := strings.Split(*seeds, ",")
		// for _, seed := range seedList {
		//     kvStore.AddSeedNode(...)
		// }
	}

	// 啟動 KV Store
	kvStore.Start()

	// 註冊 HTTP 處理器
	http.HandleFunc("/set", handleSet)
	http.HandleFunc("/get", handleGet)
	http.HandleFunc("/delete", handleDelete)
	http.HandleFunc("/stats", handleStats)
	http.HandleFunc("/data", handleData)
	http.HandleFunc("/health", handleHealth)

	// 啟動 HTTP 服務器
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting KV Store node %s on %s (N=%d, W=%d, R=%d)", *nodeID, addr, *n, *w, *r)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

// handleSet 處理 Set 請求
func handleSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 解析請求
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key parameter is required", http.StatusBadRequest)
		return
	}

	value, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 寫入
	start := time.Now()
	err = kvStore.Set(key, value)
	duration := time.Since(start)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 返回響應
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"key":      key,
		"duration": duration.Milliseconds(),
	})
}

// handleGet 處理 Get 請求
func handleGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key parameter is required", http.StatusBadRequest)
		return
	}

	// 讀取
	start := time.Now()
	value, err := kvStore.Get(key)
	duration := time.Since(start)

	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// 返回響應
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"key":      key,
		"value":    string(value),
		"duration": duration.Milliseconds(),
	})
}

// handleDelete 處理 Delete 請求
func handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "key parameter is required", http.StatusBadRequest)
		return
	}

	// 刪除
	err := kvStore.Delete(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 返回響應
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"key":     key,
	})
}

// handleStats 處理統計請求
func handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := kvStore.GetStats()

	json.NewEncoder(w).Encode(stats)
}

// handleData 處理數據導出請求
func handleData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data := kvStore.ExportData()

	json.NewEncoder(w).Encode(data)
}

// handleHealth 健康檢查
func handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"time":   time.Now().Format(time.RFC3339),
	})
}
