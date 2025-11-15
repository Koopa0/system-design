// Package main Message Queue Server 啟動入口
//
// 功能：
//  1. HTTP API Server（發送消息、查詢狀態）
//  2. 演示 NATS JetStream 基本使用
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

	"07-message-queue/internal"
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

	// 2. 創建 MessageQueue 實例
	log.Printf("連接 NATS Server: %s", cfg.NATSUrl)
	mq, err := internal.NewMessageQueue(cfg)
	if err != nil {
		log.Fatalf("創建 MessageQueue 失敗: %v", err)
	}
	defer mq.Close()

	log.Printf("✅ 成功連接 NATS Server")
	log.Printf("✅ Stream '%s' 已初始化", cfg.StreamConfig.Name)

	// 3. 創建 HTTP Handler
	handler := internal.NewHandler(mq)

	// 4. 註冊路由
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/messages", handler.HandlePublish)
	mux.HandleFunc("/api/v1/streams/info", handler.HandleStreamInfo)
	mux.HandleFunc("/api/v1/consumers/info", handler.HandleConsumerInfo)

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

	// 6. 優雅關閉
	go func() {
		log.Printf("🚀 HTTP Server 啟動於 %s", addr)
		log.Printf("📝 API 文檔:")
		log.Printf("   POST   /api/v1/messages          - 發送消息")
		log.Printf("   GET    /api/v1/streams/info      - 查詢 Stream 狀態")
		log.Printf("   GET    /api/v1/consumers/info    - 查詢 Consumer 狀態")
		log.Printf("   GET    /health                   - 健康檢查")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP Server 啟動失敗: %v", err)
		}
	}()

	// 7. 演示：定時發送測試消息（可選）
	if os.Getenv("DEMO_MODE") == "true" {
		go demoPublisher(mq)
	}

	// 8. 等待中斷信號
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 收到關閉信號，正在優雅關閉...")

	// 9. 關閉 HTTP Server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("HTTP Server 關閉失敗: %v", err)
	}

	log.Println("👋 服務已關閉")
}

// demoPublisher 演示：定時發送測試消息
func demoPublisher(mq *internal.MessageQueue) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	orderID := 1

	for range ticker.C {
		msg := &internal.Message{
			Subject: "order.created",
			Data: map[string]interface{}{
				"order_id": fmt.Sprintf("ORD-%d", orderID),
				"user_id":  1000 + orderID,
				"amount":   99.99,
			},
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		pubAck, err := mq.Publish(ctx, msg)
		cancel()

		if err != nil {
			log.Printf("❌ 發送消息失敗: %v", err)
		} else {
			log.Printf("✅ 消息已發送 - Sequence: %d, Subject: %s",
				pubAck.Sequence, msg.Subject)
		}

		orderID++
	}
}
