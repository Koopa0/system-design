// Distributed Cache 示範服務
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Koopa0/system-design/05-distributed-cache/internal/cache"
	"github.com/Koopa0/system-design/05-distributed-cache/internal/strategy"
)

// MockDataStore 模擬資料庫（用於展示快取策略）。
// MockDataStore 是一個簡單的記憶體資料儲存（用於示範）。
//
// 並發安全：使用 RWMutex 保護 map 訪問
type MockDataStore struct {
	data map[string]interface{}
	mu   sync.RWMutex
}

func NewMockDataStore() *MockDataStore {
	return &MockDataStore{
		data: make(map[string]interface{}),
	}
}

func (m *MockDataStore) Get(ctx context.Context, key string) (interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if value, ok := m.data[key]; ok {
		return value, nil
	}
	return nil, strategy.ErrKeyNotFound
}

func (m *MockDataStore) Set(ctx context.Context, key string, value interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data[key] = value
	return nil
}

func (m *MockDataStore) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.data, key)
	return nil
}

func main() {
	log.Println("啟動 Distributed Cache 示範服務...")

	// 示範 1：LRU 快取
	demonstrateLRU()

	// 示範 2：LFU 快取
	demonstrateLFU()

	// 示範 3：分散式快取
	demonstrateDistributed()

	// 示範 4：快取策略
	demonstrateStrategies()

	// 啟動 HTTP 服務
	startHTTPServer()
}

// demonstrateLRU 展示 LRU 快取。
func demonstrateLRU() {
	log.Println("\n=== LRU 快取示範 ===")

	lru := cache.NewLRU(3)

	// 寫入資料
	lru.Set("a", "value_a")
	lru.Set("b", "value_b")
	lru.Set("c", "value_c")
	log.Printf("已寫入 3 筆資料，當前快取：%v", lru.Keys())

	// 存取 a（移到最前面）
	lru.Get("a")
	log.Printf("存取 'a' 後，當前快取：%v", lru.Keys())

	// 寫入 d（淘汰最久未使用的 b）
	lru.Set("d", "value_d")
	log.Printf("寫入 'd' 後，當前快取：%v (淘汰了 'b')", lru.Keys())
}

// demonstrateLFU 展示 LFU 快取。
func demonstrateLFU() {
	log.Println("\n=== LFU 快取示範 ===")

	lfu := cache.NewLFU(3)

	// 寫入資料
	lfu.Set("a", "value_a")
	lfu.Set("b", "value_b")
	lfu.Set("c", "value_c")

	// 多次存取 a（增加頻率）
	lfu.Get("a")
	lfu.Get("a")
	lfu.Get("a")

	// 存取 b 一次
	lfu.Get("b")

	stats := lfu.GetStats()
	log.Printf("當前快取統計：大小=%d, 最小頻率=%d, 頻率分布=%v",
		stats.Size, stats.MinFreq, stats.FreqDist)

	// 寫入 d（淘汰頻率最低的 c）
	lfu.Set("d", "value_d")
	stats = lfu.GetStats()
	log.Printf("寫入 'd' 後，頻率分布=%v (淘汰了頻率最低的 'c')", stats.FreqDist)
}

// demonstrateDistributed 展示分散式快取。
func demonstrateDistributed() {
	log.Println("\n=== 分散式快取示範 ===")

	// 建立 3 個節點的分散式快取
	nodes := []string{"node1", "node2", "node3"}
	dc := cache.NewDistributedCache(nodes, func() cache.Cache {
		return cache.NewLRU(100)
	})

	// 寫入資料
	keys := []string{"user:1001", "user:1002", "user:1003", "user:1004", "user:1005"}
	for _, key := range keys {
		dc.Set(key, fmt.Sprintf("data_%s", key))
		log.Printf("寫入 %s", key)
	}

	// 查看資料分布
	stats := dc.GetStats()
	log.Println("\n資料分布：")
	for _, stat := range stats {
		log.Printf("  %s: %d 筆資料", stat.Node, stat.Size)
	}

	// 新增節點
	log.Println("\n新增節點 node4...")
	dc.AddNode("node4", func() cache.Cache {
		return cache.NewLRU(100)
	})

	// 查看節點列表
	log.Printf("當前節點：%v", dc.Nodes())
}

// demonstrateStrategies 展示快取策略。
func demonstrateStrategies() {
	log.Println("\n=== 快取策略示範 ===")

	lru := cache.NewLRU(100)
	store := NewMockDataStore()

	// Cache-Aside 策略
	aside := strategy.NewCacheAside(lru, store)

	ctx := context.Background()

	// 寫入資料
	aside.Set(ctx, "user:1001", map[string]string{"name": "Alice"})
	log.Println("使用 Cache-Aside 寫入 user:1001")

	// 讀取資料（快取未命中，從資料庫載入）
	value, err := aside.Get(ctx, "user:1001")
	if err != nil {
		log.Printf("讀取失敗：%v", err)
	} else {
		log.Printf("讀取 user:1001：%v（從資料庫載入並寫入快取）", value)
	}

	// 再次讀取（快取命中）
	value, _ = aside.Get(ctx, "user:1001")
	log.Printf("再次讀取 user:1001：%v（從快取讀取）", value)
}

// startHTTPServer 啟動 HTTP 服務。
func startHTTPServer() {
	port := getEnv("PORT", "8080")

	mux := http.NewServeMux()

	// 健康檢查
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"status":"ok"}`)
	})

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 優雅關閉
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		log.Println("正在關閉服務...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("關閉服務錯誤：%v", err)
		}
	}()

	log.Printf("服務啟動於 http://localhost:%s", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("服務啟動失敗：%v", err)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
