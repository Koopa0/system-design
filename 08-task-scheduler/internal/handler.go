package internal

import (
	"encoding/json"
	"net/http"
	"time"
)

// Handler HTTP API 處理器
type Handler struct {
	scheduler *TaskScheduler
}

// NewHandler 創建 Handler
func NewHandler(scheduler *TaskScheduler) *Handler {
	return &Handler{scheduler: scheduler}
}

// AddDelayTaskRequest 添加延遲任務請求
type AddDelayTaskRequest struct {
	DelaySeconds int                    `json:"delay_seconds"` // 延遲秒數
	CallbackURL  string                 `json:"callback_url"`  // 回調 URL
	Data         map[string]interface{} `json:"data"`          // 任務數據
}

// AddDelayTaskResponse 添加延遲任務響應
type AddDelayTaskResponse struct {
	Success   bool      `json:"success"`
	TaskID    string    `json:"task_id,omitempty"`
	ExecuteAt time.Time `json:"execute_at,omitempty"`
	Status    string    `json:"status,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// HandleAddDelayTask 處理添加延遲任務請求
//
// POST /api/v1/tasks/delay
// {
//   "delay_seconds": 1800,
//   "callback_url": "http://order-service/api/timeout",
//   "data": {"order_id": "ORD-123"}
// }
func (h *Handler) HandleAddDelayTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. 解析請求
	var req AddDelayTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 2. 驗證參數
	if req.DelaySeconds <= 0 {
		writeJSONError(w, "delay_seconds must be positive", http.StatusBadRequest)
		return
	}
	if req.CallbackURL == "" {
		writeJSONError(w, "callback_url is required", http.StatusBadRequest)
		return
	}

	// 3. 添加任務
	delay := time.Duration(req.DelaySeconds) * time.Second
	taskID, err := h.scheduler.AddDelayTask(delay, req.CallbackURL, req.Data)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. 返回響應
	executeAt := time.Now().Add(delay)
	resp := AddDelayTaskResponse{
		Success:   true,
		TaskID:    taskID,
		ExecuteAt: executeAt,
		Status:    "scheduled",
	}

	writeJSON(w, resp, http.StatusOK)
}

// HandleGetStats 獲取調度器統計信息
//
// GET /api/v1/stats
func (h *Handler) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := map[string]interface{}{
		"pending_tasks": h.scheduler.GetTaskCount(),
		"timestamp":     time.Now(),
	}

	writeJSON(w, stats, http.StatusOK)
}

// writeJSON 寫入 JSON 響應
func writeJSON(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeJSONError 寫入錯誤響應
func writeJSONError(w http.ResponseWriter, message string, status int) {
	writeJSON(w, AddDelayTaskResponse{
		Success: false,
		Error:   message,
	}, status)
}
