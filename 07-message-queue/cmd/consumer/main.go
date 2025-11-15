// Package main Message Queue Consumer 範例
//
// 演示：
//  1. 訂閱消息（Queue Groups 負載均衡）
//  2. 手動 ACK（At-least-once 語義）
//  3. 錯誤處理與重試
package main

import (
	"encoding/json"
	"flag"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"07-message-queue/internal"

	"github.com/nats-io/nats.go"
)

func main() {
	// 1. 解析命令行參數
	var (
		queueGroup   = flag.String("group", "order-processor", "Queue Group 名稱")
		consumerID   = flag.String("id", "1", "Consumer ID（用於日誌）")
		failRate     = flag.Float64("fail-rate", 0.0, "模擬失敗率（0.0-1.0）")
		natsURL      = flag.String("nats", "nats://localhost:4222", "NATS Server 地址")
	)
	flag.Parse()

	// 2. 創建 MessageQueue 實例
	cfg := internal.DefaultConfig()
	cfg.NATSUrl = *natsURL

	log.Printf("[Consumer-%s] 連接 NATS Server: %s", *consumerID, cfg.NATSUrl)
	mq, err := internal.NewMessageQueue(cfg)
	if err != nil {
		log.Fatalf("創建 MessageQueue 失敗: %v", err)
	}
	defer mq.Close()

	log.Printf("[Consumer-%s] ✅ 成功連接 NATS Server", *consumerID)

	// 3. 訂閱消息（Queue Groups 模式）
	//
	// 系統設計重點：
	//  - Queue Group：多個 Consumer 加入同一個 Group
	//  - 負載均衡：JetStream 自動分配消息（Round-Robin）
	//  - 每條消息只被一個 Consumer 處理
	//
	// 範例：
	//  Consumer 1 --┐
	//  Consumer 2 --┼--> Queue Group "order-processor"
	//  Consumer 3 --┘
	//  每個 Consumer 處理 1/3 的消息
	consumerName := "order-processor-" + *consumerID
	subject := "order.*" // 訂閱所有 order.* 主題

	log.Printf("[Consumer-%s] 訂閱 Subject: %s, Queue Group: %s", *consumerID, subject, *queueGroup)

	_, err = mq.QueueSubscribe(subject, *queueGroup, consumerName, func(msg *nats.Msg) {
		handleMessage(msg, *consumerID, *failRate)
	})
	if err != nil {
		log.Fatalf("訂閱失敗: %v", err)
	}

	log.Printf("[Consumer-%s] 🎧 開始監聽消息...", *consumerID)

	// 4. 等待中斷信號
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Printf("[Consumer-%s] 🛑 收到關閉信號，正在關閉...", *consumerID)
	log.Printf("[Consumer-%s] 👋 Consumer 已關閉", *consumerID)
}

// handleMessage 處理單條消息
//
// 系統設計重點：
//
//  1. 手動 ACK：
//     - msg.Ack()：處理成功，確認消息
//     - msg.Nak()：處理失敗，觸發重試
//     - 未 ACK：30 秒超時後自動重試
//
//  2. At-least-once 語義：
//     - 消息至少被處理一次
//     - 可能重複消費（網絡重試、Consumer 崩潰）
//     - 需要冪等性設計
//
//  3. 冪等性範例：
//     - 資料庫唯一約束：INSERT ... ON CONFLICT DO NOTHING
//     - 去重表：記錄已處理的消息 ID
//     - 業務層去重：檢查訂單狀態是否已變更
func handleMessage(msg *nats.Msg, consumerID string, failRate float64) {
	// 1. 解析消息
	var data map[string]interface{}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		log.Printf("[Consumer-%s] ❌ 解析消息失敗: %v", consumerID, err)
		msg.Nak() // NAK，觸發重試
		return
	}

	// 2. 獲取消息元數據
	metadata, _ := msg.Metadata()
	log.Printf("[Consumer-%s] 📨 收到消息 - Subject: %s, Sequence: %d, Delivered: %d",
		consumerID, msg.Subject, metadata.Sequence.Stream, metadata.NumDelivered)
	log.Printf("[Consumer-%s]    Data: %+v", consumerID, data)

	// 3. 模擬處理失敗（用於測試重試機制）
	if failRate > 0 && rand.Float64() < failRate {
		log.Printf("[Consumer-%s] 💥 模擬處理失敗（失敗率: %.1f%%）", consumerID, failRate*100)
		msg.Nak() // NAK，觸發重試
		return
	}

	// 4. 模擬業務處理
	//
	// 實際應用範例：
	//  - order.created：扣減庫存、發送通知
	//  - order.paid：更新訂單狀態、生成發票
	//  - order.shipped：發送物流通知
	time.Sleep(100 * time.Millisecond) // 模擬處理時間

	// 5. 冪等性檢查（範例）
	//
	// 生產環境建議：
	//  - 檢查去重表：SELECT EXISTS(SELECT 1 FROM processed_messages WHERE msg_id = ?)
	//  - 若已處理：直接 ACK（避免重複處理）
	//  - 若未處理：執行業務邏輯 + 插入去重表（事務）
	/*
	orderID := data["order_id"].(string)
	if isProcessed(orderID) {
		log.Printf("[Consumer-%s] ⏭️  消息已處理過，跳過: %s", consumerID, orderID)
		msg.Ack()
		return
	}
	*/

	// 6. 執行業務邏輯（範例）
	if err := processOrder(data); err != nil {
		log.Printf("[Consumer-%s] ❌ 處理失敗: %v", consumerID, err)
		msg.Nak() // NAK，觸發重試
		return
	}

	// 7. 處理成功，ACK
	if err := msg.Ack(); err != nil {
		log.Printf("[Consumer-%s] ⚠️  ACK 失敗: %v", consumerID, err)
		return
	}

	log.Printf("[Consumer-%s] ✅ 處理成功", consumerID)
}

// processOrder 處理訂單（業務邏輯範例）
func processOrder(data map[string]interface{}) error {
	// 實際應用範例：
	//  1. 驗證訂單數據
	//  2. 扣減庫存（調用 Inventory Service）
	//  3. 創建訂單記錄（寫入資料庫）
	//  4. 發送通知（調用 Notification Service）

	orderID := data["order_id"]
	log.Printf("   💼 處理訂單: %v", orderID)

	// 模擬可能的錯誤
	// return fmt.Errorf("庫存不足")

	return nil
}

// isProcessed 檢查消息是否已處理（冪等性）
//
// 生產環境實現範例：
//
//	func isProcessed(messageID string) bool {
//	    var exists bool
//	    db.QueryRow("SELECT EXISTS(SELECT 1 FROM processed_messages WHERE msg_id = $1)", messageID).Scan(&exists)
//	    return exists
//	}
func isProcessed(messageID string) bool {
	// 教學簡化：未實現
	return false
}
