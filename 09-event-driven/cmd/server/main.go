// Package main Event-Driven Architecture 服務啟動入口
//
// 功能：
//  1. HTTP API Server（創建訂單、查詢訂單）
//  2. Event Store（NATS JetStream）
//  3. CQRS Read Side（訂單投影）
//  4. Saga 協調器（分布式事務）
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/koopa0/system-design/09-event-driven/internal"
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

	// 2. 創建 Event Store
	log.Printf("連接 NATS Server: %s", cfg.NATSUrl)
	eventStore, err := internal.NewEventStore(cfg.NATSUrl, cfg.EventStoreConfig)
	if err != nil {
		log.Fatalf("創建 Event Store 失敗: %v", err)
	}
	defer eventStore.Close()

	log.Println("✅ Event Store 已創建")
	log.Printf("⚙️  Stream: %s, Subject: %s.*",
		cfg.EventStoreConfig.StreamName, cfg.EventStoreConfig.SubjectPrefix)

	// 3. 創建訂單倉儲（連接 Aggregate 與 Event Store）
	repository := internal.NewOrderRepository(eventStore)

	// 4. 創建 CQRS Read Side（訂單投影）
	projection := internal.NewOrderProjection(eventStore)
	if err := projection.Start(); err != nil {
		log.Fatalf("啟動 Projection 失敗: %v", err)
	}
	log.Println("✅ CQRS Read Side 已啟動")

	// 5. 創建 Saga 協調器
	saga := internal.NewOrderSaga(eventStore, repository)
	if err := saga.Start(); err != nil {
		log.Fatalf("啟動 Saga 失敗: %v", err)
	}
	log.Println("✅ Saga 協調器已啟動")

	// 6. 創建 HTTP Handler
	handler := &Handler{
		repository: repository,
		projection: projection,
	}

	// 7. 註冊路由
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/orders", handler.HandleOrders)
	mux.HandleFunc("/api/v1/orders/", handler.HandleGetOrder)
	mux.HandleFunc("/api/v1/stats", handler.HandleStats)

	// 健康檢查
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// 8. 啟動 HTTP Server
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
		log.Println("   POST   /api/v1/orders       - 創建訂單（Command Side）")
		log.Println("   GET    /api/v1/orders/{id}  - 查詢訂單（Query Side）")
		log.Println("   GET    /api/v1/stats        - 統計信息")
		log.Println("   GET    /health              - 健康檢查")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP Server 啟動失敗: %v", err)
		}
	}()

	// 9. 演示：添加測試訂單（可選）
	if os.Getenv("DEMO_MODE") == "true" {
		go demoOrders(repository)
	}

	// 10. 等待中斷信號
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 收到關閉信號，正在優雅關閉...")

	// 11. 關閉 HTTP Server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP Server 關閉失敗: %v", err)
	}

	log.Println("👋 服務已關閉")
}

// Handler HTTP 處理器
type Handler struct {
	repository *internal.OrderRepository
	projection *internal.OrderProjection
}

// CreateOrderRequest 創建訂單請求
type CreateOrderRequest struct {
	UserID int                   `json:"user_id"`
	Amount float64               `json:"amount"`
	Items  []internal.OrderItem  `json:"items"`
}

// HandleOrders 處理訂單 API
//
// 系統設計：
//   - POST：創建訂單（Command Side - 寫端）
//   - GET：列出所有訂單（Query Side - 讀端）
func (h *Handler) HandleOrders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleCreateOrder(w, r)
	case http.MethodGet:
		h.handleListOrders(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCreateOrder 創建訂單（Command Side）
//
// 系統設計流程：
//   1. 解析請求
//   2. 創建 Aggregate
//   3. 執行命令（CreateOrder）
//   4. 保存事件到 Event Store
//   5. Saga 自動處理後續流程（預留庫存→支付→完成）
func (h *Handler) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var req CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// 生成訂單 ID
	orderID := fmt.Sprintf("order-%d", time.Now().UnixNano())

	// 創建訂單 Aggregate
	order := internal.NewOrderAggregate(orderID)

	// 執行命令：創建訂單
	if err := order.CreateOrder(req.UserID, req.Amount, req.Items); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 保存事件（寫入 Event Store）
	if err := h.repository.Save(order); err != nil {
		http.Error(w, "Failed to save order", http.StatusInternalServerError)
		return
	}

	log.Printf("📦 訂單已創建: %s (User: %d, Amount: %.2f)", orderID, req.UserID, req.Amount)
	log.Printf("   🎯 Saga 將自動處理: 預留庫存 → 支付 → 完成訂單")

	// 返回訂單 ID
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"order_id": orderID,
		"status":   "created",
		"message":  "訂單已創建，Saga 正在處理中",
	})
}

// handleListOrders 列出所有訂單（Query Side）
//
// 系統設計：從 Read Model 查詢（CQRS）
//   - 不需要重放事件
//   - 支持複雜查詢（可加入過濾、排序、分頁）
func (h *Handler) handleListOrders(w http.ResponseWriter, r *http.Request) {
	// 可選：按狀態過濾
	status := r.URL.Query().Get("status")

	var orders []*internal.OrderReadModel
	if status != "" {
		orders = h.projection.ListOrdersByStatus(status)
	} else {
		orders = h.projection.ListOrders()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(orders)
}

// HandleGetOrder 查詢單個訂單（Query Side）
//
// 系統設計：從 Read Model 查詢
//   - O(1) 查詢（內存 map，生產環境為資料庫索引）
//   - 包含完整事件歷史（審計）
func (h *Handler) HandleGetOrder(w http.ResponseWriter, r *http.Request) {
	// 從 URL 提取訂單 ID
	// /api/v1/orders/order-123 → order-123
	orderID := r.URL.Path[len("/api/v1/orders/"):]
	if orderID == "" {
		http.Error(w, "Order ID required", http.StatusBadRequest)
		return
	}

	order, err := h.projection.GetOrder(orderID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(order)
}

// HandleStats 統計信息（CQRS 聚合查詢）
func (h *Handler) HandleStats(w http.ResponseWriter, r *http.Request) {
	stats := h.projection.GetStatistics()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// demoOrders 演示：創建測試訂單
func demoOrders(repository *internal.OrderRepository) {
	time.Sleep(2 * time.Second) // 等待服務啟動

	log.Println("🎭 演示模式：創建測試訂單...")

	// 訂單 1
	order1 := internal.NewOrderAggregate("demo-order-1")
	order1.CreateOrder(123, 99.99, []internal.OrderItem{
		{ProductID: 1, Quantity: 2},
	})
	repository.Save(order1)
	log.Println("   ✅ 測試訂單 1 已創建: demo-order-1")

	// 訂單 2
	time.Sleep(1 * time.Second)
	order2 := internal.NewOrderAggregate("demo-order-2")
	order2.CreateOrder(456, 199.98, []internal.OrderItem{
		{ProductID: 2, Quantity: 1},
		{ProductID: 3, Quantity: 3},
	})
	repository.Save(order2)
	log.Println("   ✅ 測試訂單 2 已創建: demo-order-2")
}
