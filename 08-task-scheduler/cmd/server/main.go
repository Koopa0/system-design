// Package main Task Scheduler Server 啟動入口
//
// 功能：
//  1. HTTP API Server（添加延遲任務、查詢統計）
//  2. 時間輪調度器（O(1) 性能）
//  3. NATS JetStream 持久化
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/koopa0/system-design/08-task-scheduler/internal"
)

func main() {
	// 1. 加載配置
	cfg := internal.DefaultConfig()

	// 從環境變量覆蓋配置
	if port := os.Getenv("HTTP_PORT"); port != "" {
		cfg.HTTPPort = port
	}
	if natsURL := os.Getenv("NATS_URL"); natsURL != "" {
		cfg.NATSUrl = natsURL
	}

	// 2. 創建任務調度器
	log.Printf("連接 NATS Server: %s", cfg.NATSUrl)
	scheduler, err := internal.NewTaskScheduler(cfg)
	if err != nil {
		log.Fatalf("創建任務調度器失敗: %v", err)
	}
	defer scheduler.Close()

	log.Println("✅ 任務調度器已創建")
	log.Printf("⚙️  時間輪配置: %d 槽位, 每 %s 轉動一次",
		cfg.WheelConfig.SlotCount, cfg.WheelConfig.TickDuration)

	// 3. 創建 HTTP Handler
	handler := internal.NewHandler(scheduler)

	// 4. 註冊路由
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/tasks/delay", handler.HandleAddDelayTask)
	mux.HandleFunc("/api/v1/stats", handler.HandleGetStats)

	// 健康檢查
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// 5. 啟動 HTTP Server
	addr := ":" + cfg.HTTPPort
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("🚀 HTTP Server 啟動於 %s", addr)
		log.Println("📝 API 文檔:")
		log.Println("   POST   /api/v1/tasks/delay    - 添加延遲任務")
		log.Println("   GET    /api/v1/stats          - 查詢統計信息")
		log.Println("   GET    /health                - 健康檢查")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP Server 啟動失敗: %v", err)
		}
	}()

	// 6. 啟動調度器
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := scheduler.Start(ctx); err != nil {
			log.Fatalf("調度器啟動失敗: %v", err)
		}
	}()

	// 7. 演示：添加測試任務（可選）
	if os.Getenv("DEMO_MODE") == "true" {
		go demoTasks(scheduler)
	}

	// 8. 等待中斷信號
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 收到關閉信號，正在優雅關閉...")

	// 9. 關閉 HTTP Server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP Server 關閉失敗: %v", err)
	}

	// 10. 取消調度器上下文
	cancel()

	log.Println("👋 服務已關閉")
}

// demoTasks 演示：添加測試任務
func demoTasks(scheduler *internal.TaskScheduler) {
	time.Sleep(2 * time.Second) // 等待服務啟動

	log.Println("🎭 演示模式：添加測試任務...")

	// 任務 1：10 秒後執行
	taskID1, err := scheduler.AddDelayTask(
		10*time.Second,
		"http://httpbin.org/post",
		map[string]interface{}{
			"task_name": "demo-task-1",
			"message":   "10 秒後執行",
		},
	)
	if err != nil {
		log.Printf("❌ 添加任務失敗: %v", err)
	} else {
		log.Printf("   ✅ 任務 1 已創建: ID=%s, 10秒後執行", taskID1)
	}

	// 任務 2：30 秒後執行
	taskID2, err := scheduler.AddDelayTask(
		30*time.Second,
		"http://httpbin.org/post",
		map[string]interface{}{
			"task_name": "demo-task-2",
			"message":   "30 秒後執行",
		},
	)
	if err != nil {
		log.Printf("❌ 添加任務失敗: %v", err)
	} else {
		log.Printf("   ✅ 任務 2 已創建: ID=%s, 30秒後執行", taskID2)
	}
}
