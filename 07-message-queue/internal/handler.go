package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/nats-io/nats.go"
)

// Handler HTTP API 處理器
type Handler struct {
	mq *MessageQueue
}

// NewHandler 創建 Handler 實例
func NewHandler(mq *MessageQueue) *Handler {
	return &Handler{mq: mq}
}

// PublishRequest 發送消息請求
type PublishRequest struct {
	Subject string                 `json:"subject"` // 消息主題
	Data    map[string]interface{} `json:"data"`    // 消息內容
}

// PublishResponse 發送消息響應
type PublishResponse struct {
	Success   bool      `json:"success"`   // 是否成功
	Sequence  uint64    `json:"sequence"`  // 消息序號
	Timestamp time.Time `json:"timestamp"` // 時間戳
	Error     string    `json:"error,omitempty"` // 錯誤信息（若有）
}

// StreamInfoResponse Stream 狀態響應
type StreamInfoResponse struct {
	Stream        string    `json:"stream"`
	Messages      uint64    `json:"messages"`
	Bytes         uint64    `json:"bytes"`
	FirstSeq      uint64    `json:"first_seq"`
	LastSeq       uint64    `json:"last_seq"`
	ConsumerCount int       `json:"consumer_count"`
	Timestamp     time.Time `json:"timestamp"`
}

// ConsumerInfoResponse Consumer 狀態響應
type ConsumerInfoResponse struct {
	Stream          string                  `json:"stream"`
	Consumer        string                  `json:"consumer"`
	NumPending      uint64                  `json:"num_pending"`
	NumAckPending   int                     `json:"num_ack_pending"`
	NumRedelivered  uint64                  `json:"num_redelivered"`
	Delivered       *nats.SequenceInfo      `json:"delivered"`
	Timestamp       time.Time               `json:"timestamp"`
}

// HandlePublish 處理發送消息請求
//
// POST /api/v1/messages
// {
//   "subject": "order.created",
//   "data": {"order_id": "ORD-123", "amount": 99.99}
// }
func (h *Handler) HandlePublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. 解析請求
	var req PublishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 2. 驗證參數
	if req.Subject == "" {
		writeError(w, "Subject is required", http.StatusBadRequest)
		return
	}

	// 3. 發送消息
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	msg := &Message{
		Subject: req.Subject,
		Data:    req.Data,
	}

	pubAck, err := h.mq.Publish(ctx, msg)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. 返回響應
	resp := PublishResponse{
		Success:   true,
		Sequence:  pubAck.Sequence,
		Timestamp: time.Now(),
	}

	writeJSON(w, resp, http.StatusOK)
}

// HandleStreamInfo 處理查詢 Stream 狀態請求
//
// GET /api/v1/streams/{streamName}
func (h *Handler) HandleStreamInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. 獲取 Stream 信息
	info, err := h.mq.GetStreamInfo()
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. 構建響應
	resp := StreamInfoResponse{
		Stream:        info.Config.Name,
		Messages:      info.State.Msgs,
		Bytes:         info.State.Bytes,
		FirstSeq:      info.State.FirstSeq,
		LastSeq:       info.State.LastSeq,
		ConsumerCount: info.State.Consumers,
		Timestamp:     time.Now(),
	}

	writeJSON(w, resp, http.StatusOK)
}

// HandleConsumerInfo 處理查詢 Consumer 狀態請求
//
// GET /api/v1/consumers/{streamName}/{consumerName}
func (h *Handler) HandleConsumerInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. 從 URL 參數獲取 consumer 名稱
	consumerName := r.URL.Query().Get("consumer")
	if consumerName == "" {
		writeError(w, "Consumer name is required", http.StatusBadRequest)
		return
	}

	// 2. 獲取 Consumer 信息
	info, err := h.mq.GetConsumerInfo(consumerName)
	if err != nil {
		writeError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 3. 構建響應
	resp := ConsumerInfoResponse{
		Stream:         info.Stream,
		Consumer:       info.Name,
		NumPending:     info.NumPending,
		NumAckPending:  info.NumAckPending,
		NumRedelivered: info.NumRedelivered,
		Delivered:      &info.Delivered,
		Timestamp:      time.Now(),
	}

	writeJSON(w, resp, http.StatusOK)
}

// writeJSON 寫入 JSON 響應
func writeJSON(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeError 寫入錯誤響應
func writeError(w http.ResponseWriter, message string, status int) {
	writeJSON(w, PublishResponse{
		Success: false,
		Error:   message,
	}, status)
}
