package internal

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Handler HTTP 請求處理器
type Handler struct {
	counter *Counter
	logger  *slog.Logger
}

// NewHandler 創建 HTTP 處理器
func NewHandler(counter *Counter, logger *slog.Logger) *Handler {
	return &Handler{
		counter: counter,
		logger:  logger,
	}
}

// Routes 設定路由
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	// 中間件鏈：日誌 -> 恢復 -> 業務處理
	wrap := func(handler http.HandlerFunc) http.HandlerFunc {
		return h.recoverer(h.loggerMiddleware(handler))
	}

	// API 路由
	mux.HandleFunc("POST /api/v1/counter/{name}/increment", wrap(h.increment))
	mux.HandleFunc("POST /api/v1/counter/{name}/decrement", wrap(h.decrement))
	mux.HandleFunc("GET /api/v1/counter/{name}", wrap(h.get))
	mux.HandleFunc("GET /api/v1/counters", wrap(h.getMultiple))
	mux.HandleFunc("POST /api/v1/counter/{name}/reset", wrap(h.reset))

	// 健康檢查
	mux.HandleFunc("GET /health", wrap(h.health))
	mux.HandleFunc("GET /ready", wrap(h.ready))

	return mux
}

// 請求和響應結構
type incrementRequest struct {
	Value    int64          `json:"value,omitempty"`
	UserID   string         `json:"user_id,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type counterResponse struct {
	Success      bool   `json:"success"`
	CurrentValue int64  `json:"current_value,omitempty"`
	Error        string `json:"error,omitempty"`
}

type getResponse struct {
	Name        string    `json:"name"`
	Value       int64     `json:"value"`
	LastUpdated time.Time `json:"last_updated"`
}

type multipleResponse struct {
	Counters []struct {
		Name  string `json:"name"`
		Value int64  `json:"value"`
	} `json:"counters"`
}

// increment 處理增加計數請求
func (h *Handler) increment(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		h.respondError(w, "counter name required", http.StatusBadRequest)
		return
	}

	// 解析請求
	var req incrementRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.respondError(w, "invalid request body", http.StatusBadRequest)
			return
		}
	}

	// 預設值
	if req.Value == 0 {
		req.Value = 1
	}

	// 執行增加操作
	newValue, err := h.counter.Increment(r.Context(), name, req.Value, req.UserID)
	if err != nil {
		h.logger.Error("increment failed", "counter", name, "error", err)
		h.respondError(w, "increment failed", http.StatusInternalServerError)
		return
	}

	h.respondJSON(w, counterResponse{
		Success:      true,
		CurrentValue: newValue,
	})
}

// decrement 處理減少計數請求
func (h *Handler) decrement(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		h.respondError(w, "counter name required", http.StatusBadRequest)
		return
	}

	var req incrementRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			h.respondError(w, "invalid request body", http.StatusBadRequest)
			return
		}
	}

	if req.Value == 0 {
		req.Value = 1
	}

	newValue, err := h.counter.Decrement(r.Context(), name, req.Value)
	if err != nil {
		h.logger.Error("decrement failed", "counter", name, "error", err)
		h.respondError(w, "decrement failed", http.StatusInternalServerError)
		return
	}

	h.respondJSON(w, counterResponse{
		Success:      true,
		CurrentValue: newValue,
	})
}

// get 獲取單個計數器值
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		h.respondError(w, "counter name required", http.StatusBadRequest)
		return
	}

	value, err := h.counter.GetValue(r.Context(), name)
	if err != nil {
		h.logger.Error("get value failed", "counter", name, "error", err)
		h.respondError(w, "counter not found", http.StatusNotFound)
		return
	}

	h.respondJSON(w, getResponse{
		Name:        name,
		Value:       value,
		LastUpdated: time.Now(),
	})
}

// getMultiple 批量獲取計數器
func (h *Handler) getMultiple(w http.ResponseWriter, r *http.Request) {
	namesParam := r.URL.Query().Get("names")
	if namesParam == "" {
		h.respondError(w, "names parameter required", http.StatusBadRequest)
		return
	}

	// 解析名稱列表
	names := strings.Split(namesParam, ",")
	if len(names) > 10 {
		h.respondError(w, "maximum 10 counters allowed", http.StatusBadRequest)
		return
	}

	values, err := h.counter.GetMultiple(r.Context(), names)
	if err != nil {
		h.logger.Error("get multiple failed", "error", err)
		h.respondError(w, "failed to get counters", http.StatusInternalServerError)
		return
	}

	// 構建響應
	resp := multipleResponse{}
	for name, value := range values {
		resp.Counters = append(resp.Counters, struct {
			Name  string `json:"name"`
			Value int64  `json:"value"`
		}{
			Name:  name,
			Value: value,
		})
	}

	h.respondJSON(w, resp)
}

// reset 重置計數器
func (h *Handler) reset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	// 簡單的權限檢查
	var req struct {
		AdminToken string `json:"admin_token"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.respondError(w, "invalid request", http.StatusBadRequest)
		return
	}

	// 實際生產環境應該用更安全的方式
	if req.AdminToken != "secret_token" {
		h.respondError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if err := h.counter.Reset(r.Context(), name); err != nil {
		h.logger.Error("reset failed", "counter", name, "error", err)
		h.respondError(w, "reset failed", http.StatusInternalServerError)
		return
	}

	h.respondJSON(w, counterResponse{
		Success:      true,
		CurrentValue: 0,
	})
}

// health 健康檢查
func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
}

// ready 就緒檢查
func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	// 檢查 Redis 和 PostgreSQL 連線
	ctx := r.Context()

	// 簡單的連線測試
	if err := h.counter.redis.Ping(ctx).Err(); err != nil {
		h.respondError(w, "redis not ready", http.StatusServiceUnavailable)
		return
	}

	if err := h.counter.pg.Ping(ctx); err != nil {
		h.respondError(w, "postgres not ready", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "Ready")
}

// 中間件
// loggerMiddleware 記錄請求日誌
func (h *Handler) loggerMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// 包裝 ResponseWriter 以捕獲狀態碼
		ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next(ww, r)

		h.logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.statusCode,
			"duration", time.Since(start),
			"remote", r.RemoteAddr,
		)
	}
}

// recoverer 恢復 panic
func (h *Handler) recoverer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				h.logger.Error("panic recovered", "error", err)
				h.respondError(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next(w, r)
	}
}

func (h *Handler) respondJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

func (h *Handler) respondError(w http.ResponseWriter, message string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(counterResponse{
		Success: false,
		Error:   message,
	}); err != nil {
		h.logger.Error("failed to encode error response", "error", err, "message", message)
	}
}

// responseWriter 包裝以捕獲狀態碼
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (w *responseWriter) WriteHeader(code int) {
	if !w.written {
		w.statusCode = code
		w.written = true
		w.ResponseWriter.WriteHeader(code)
	}
}
