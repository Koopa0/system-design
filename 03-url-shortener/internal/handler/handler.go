// Package handler 實現 HTTP 請求處理
//
// 教學重點：
//
//  1. 使用 net/http 標準庫（不依賴框架）
//
//  2. Go 1.22+ 的增強路由功能：
//     - 支持方法路由：GET、POST
//     - 支持路徑參數：/{shortCode}
//     - 無需第三方庫！
//
//  3. 標準的 HTTP 處理模式：
//     - Handler 函數簽名：func(w http.ResponseWriter, r *http.Request)
//     - 中間件模式（logger、recovery）
//     - 錯誤處理和狀態碼
//
// 系統設計考量：
//   - API 設計：RESTful 風格
//   - 錯誤響應：統一的 JSON 格式
//   - 日誌記錄：結構化日誌
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/koopa0/system-design/03-url-shortener/internal/shortener"
	"github.com/koopa0/system-design/03-url-shortener/pkg/snowflake"
)

// Handler HTTP 處理器
//
// Go 慣用法：
//   - 簡單的結構體
//   - 依賴注入（store、idgen、logger）
//   - 方法接收器提供 HTTP handler 函數
type Handler struct {
	store  shortener.Store
	idgen  *snowflake.Generator
	logger *slog.Logger
}

// New 創建 Handler 實例
func New(store shortener.Store, idgen *snowflake.Generator, logger *slog.Logger) *Handler {
	return &Handler{
		store:  store,
		idgen:  idgen,
		logger: logger,
	}
}

// Routes 設置路由
//
// Go 1.22+ 路由功能：
//   - "POST /api/v1/urls" → 方法 + 路徑匹配
//   - "GET /{shortCode}" → 路徑參數
//   - r.PathValue("shortCode") → 獲取參數
//
// 中間件鏈：
//   - logger → recovery → 業務處理
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	// 創建短網址
	mux.HandleFunc("POST /api/v1/urls", h.withMiddleware(h.create))

	// 重定向（核心功能）
	// 注意：這裡不用 /api/v1 前綴，短網址應該儘量短
	mux.HandleFunc("GET /{shortCode}", h.withMiddleware(h.redirect))

	// 獲取統計信息（可選）
	mux.HandleFunc("GET /api/v1/urls/{shortCode}/stats", h.withMiddleware(h.stats))

	// 健康檢查
	mux.HandleFunc("GET /health", h.health)

	return mux
}

// withMiddleware 應用中間件鏈
//
// 中間件模式：
//   - 記錄日誌（請求開始/結束）
//   - 恢復 panic
//   - 最後執行業務處理
func (h *Handler) withMiddleware(next http.HandlerFunc) http.HandlerFunc {
	// 先應用 recovery（最外層，捕獲所有 panic）
	// 再應用 logger
	// 最後執行業務邏輯
	return h.recovery(h.logRequest(next))
}

// create 創建短網址
//
// API: POST /api/v1/urls
// Body: {"long_url": "https://...", "custom_code": "optional", "expires_at": "2024-12-31T..."}
// Response: {"short_url": "https://short.url/abc123", "short_code": "abc123", ...}
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	// 1. 解析請求
	var req struct {
		LongURL    string  `json:"long_url"`
		CustomCode string  `json:"custom_code,omitempty"`
		ExpiresAt  *string `json:"expires_at,omitempty"` // RFC3339 格式
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorJSON(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// 2. 驗證必填字段
	if req.LongURL == "" {
		h.errorJSON(w, "long_url is required", http.StatusBadRequest)
		return
	}

	// 3. 解析過期時間（如果有）
	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			h.errorJSON(w, "invalid expires_at format (use RFC3339)", http.StatusBadRequest)
			return
		}
		expiresAt = &t
	}

	// 4. 調用 shortener 包創建短網址
	//
	// 注意：直接調用包級別函數，傳入依賴（store、idgen）
	ctx := r.Context()
	url, err := shortener.Shorten(ctx, h.store, h.idgen, req.LongURL, req.CustomCode, expiresAt)
	if err != nil {
		// 錯誤處理：根據錯誤類型返回不同狀態碼
		switch {
		case errors.Is(err, shortener.ErrInvalidURL):
			h.errorJSON(w, "invalid url format", http.StatusBadRequest)
		case errors.Is(err, shortener.ErrCodeExists):
			h.errorJSON(w, "custom code already exists", http.StatusConflict)
		default:
			h.logger.Error("create short url failed", "error", err)
			h.errorJSON(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	// 5. 構建響應
	//
	// 系統設計考量：
	//   - 返回完整的短網址（方便客戶端直接使用）
	//   - 同時返回短碼（方便後續查詢統計）
	resp := map[string]any{
		"short_url":  buildShortURL(r, url.ShortCode),
		"short_code": url.ShortCode,
		"long_url":   url.LongURL,
		"created_at": url.CreatedAt.Format(time.RFC3339),
	}
	if url.ExpiresAt != nil {
		resp["expires_at"] = url.ExpiresAt.Format(time.RFC3339)
	}

	h.writeJSON(w, resp, http.StatusCreated)
}

// redirect 重定向到長網址
//
// API: GET /{shortCode}
// Response: 302 Found, Location: https://...
//
// 系統設計要點：
//   - 使用 302（臨時重定向）而非 301（永久重定向）
//   - 為什麼？302 每次都經過服務器，可以統計點擊
//   - 301 會被瀏覽器快取，後續訪問不經過服務器
func (h *Handler) redirect(w http.ResponseWriter, r *http.Request) {
	// 1. 獲取路徑參數（Go 1.22+ 功能）
	shortCode := r.PathValue("shortCode")
	if shortCode == "" {
		h.errorJSON(w, "short code required", http.StatusBadRequest)
		return
	}

	// 2. 調用 shortener 包解析短碼
	ctx := r.Context()
	longURL, err := shortener.Resolve(ctx, h.store, shortCode)
	if err != nil {
		switch {
		case errors.Is(err, shortener.ErrNotFound):
			// 404 Not Found（短碼不存在）
			h.errorJSON(w, "short code not found", http.StatusNotFound)
		case errors.Is(err, shortener.ErrExpired):
			// 410 Gone（URL 已過期）
			// 使用 410 而非 404，語義更準確
			h.errorJSON(w, "url expired", http.StatusGone)
		default:
			h.logger.Error("resolve short code failed", "short_code", shortCode, "error", err)
			h.errorJSON(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}

	// 3. 執行重定向
	//
	// HTTP 狀態碼說明：
	//   - 301 Moved Permanently：永久重定向（瀏覽器快取）
	//   - 302 Found：臨時重定向（每次都經過服務器）✅
	//   - 307 Temporary Redirect：臨時（保持 HTTP 方法）
	//
	// 我們選擇 302，因為需要統計每次點擊
	http.Redirect(w, r, longURL, http.StatusFound) // 302
}

// stats 獲取統計信息
//
// API: GET /api/v1/urls/{shortCode}/stats
// Response: {"short_code": "abc123", "long_url": "...", "clicks": 123, ...}
func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	// 獲取路徑參數
	shortCode := r.PathValue("shortCode")
	if shortCode == "" {
		h.errorJSON(w, "short code required", http.StatusBadRequest)
		return
	}

	// 調用 shortener 包查詢統計信息
	ctx := r.Context()
	url, err := shortener.Stats(ctx, h.store, shortCode)
	if err != nil {
		if errors.Is(err, shortener.ErrNotFound) {
			h.errorJSON(w, "short code not found", http.StatusNotFound)
			return
		}
		h.logger.Error("get stats failed", "short_code", shortCode, "error", err)
		h.errorJSON(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// 構建響應
	resp := map[string]any{
		"short_code": url.ShortCode,
		"long_url":   url.LongURL,
		"clicks":     url.Clicks,
		"created_at": url.CreatedAt.Format(time.RFC3339),
	}
	if url.ExpiresAt != nil {
		resp["expires_at"] = url.ExpiresAt.Format(time.RFC3339)
	}

	h.writeJSON(w, resp, http.StatusOK)
}

// health 健康檢查
func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	h.writeJSON(w, map[string]string{"status": "ok"}, http.StatusOK)
}

// === 工具函數 ===

// writeJSON 寫入 JSON 響應
func (h *Handler) writeJSON(w http.ResponseWriter, data any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("encode json failed", "error", err)
	}
}

// errorJSON 寫入錯誤響應（統一格式）
func (h *Handler) errorJSON(w http.ResponseWriter, message string, status int) {
	h.writeJSON(w, map[string]string{"error": message}, status)
}

// buildShortURL 構建完整的短網址
//
// 系統設計考量：
//   - 生產環境應使用配置的域名（如 short.url）
//   - 這裡簡化處理：使用請求的 Host
//
// buildShortURL 構建完整的短網址
//
// 系統設計考量：
//   - Scheme 檢測：支持反向代理場景
//     → 優先檢查 X-Forwarded-Proto（代理轉發的原始協議）
//     → 回退到 r.TLS（直連場景）
//   - 部署場景：
//     → 開發環境：直連（http://localhost:8080）
//     → 生產環境：代理（nginx → https → 服務）
func buildShortURL(r *http.Request, shortCode string) string {
	// 檢查代理轉發的原始協議（常見於生產環境）
	//
	// 為什麼優先檢查 X-Forwarded-Proto？
	//   - 反向代理（nginx/Traefik）會設置此 header
	//   - 表示客戶端到代理的原始協議（https）
	//   - r.TLS 為 nil（代理到服務是 http）
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme == "" {
		// 直連場景：根據 TLS 判斷
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	return scheme + "://" + r.Host + "/" + shortCode
}

// === 中間件 ===

// logRequest 記錄請求日誌
//
// 記錄內容：
//   - 請求方法、路徑
//   - 響應狀態碼、耗時
//   - 客戶端 IP
func (h *Handler) logRequest(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// 包裝 ResponseWriter 以捕獲狀態碼
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// 執行業務邏輯
		next(wrapped, r)

		// 記錄日誌
		duration := time.Since(start)
		h.logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration", duration,
			"ip", r.RemoteAddr,
		)
	}
}

// recovery 恢復 panic
//
// 防止單個請求的 panic 導致整個服務崩潰
func (h *Handler) recovery(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				h.logger.Error("panic recovered",
					"error", err,
					"path", r.URL.Path,
				)
				h.errorJSON(w, "internal server error", http.StatusInternalServerError)
			}
		}()

		next(w, r)
	}
}

// responseWriter 包裝 http.ResponseWriter 以捕獲狀態碼
//
// Go 慣用法：
//   - 組合（embedded field）
//   - 攔截 WriteHeader 方法
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

// WriteHeader 攔截狀態碼
func (w *responseWriter) WriteHeader(code int) {
	if !w.written {
		w.statusCode = code
		w.written = true
		w.ResponseWriter.WriteHeader(code)
	}
}

// Write 確保 WriteHeader 被調用
func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// === Context 工具函數（可選）===

// withTimeout 為請求添加超時
//
// 用途：
//   - 防止慢查詢阻塞
//   - 保護資料庫
//
// 使用示例：
//
//	ctx, cancel := withTimeout(r.Context(), 5*time.Second)
//	defer cancel()
//	url, err := h.service.Resolve(ctx, shortCode)
func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, timeout)
}
