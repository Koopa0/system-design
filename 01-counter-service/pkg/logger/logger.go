// Package logger 提供結構化日誌功能
package logger

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"
)

// contextKey 用於上下文的鍵類型
type contextKey string

const (
	// RequestIDKey 請求 ID 的上下文鍵
	RequestIDKey contextKey = "request_id"
	// UserIDKey 用戶 ID 的上下文鍵
	UserIDKey contextKey = "user_id"
)

// defaultLogger 預設日誌記錄器
var defaultLogger *slog.Logger

// Init 初始化日誌系統
func Init(level, format, outputPath string, addSource bool) error {
	// 解析日誌級別
	logLevel := parseLevel(level)

	// 設置輸出
	var output *os.File
	switch outputPath {
	case "stdout":
		output = os.Stdout
	case "stderr":
		output = os.Stderr
	default:
		// #nosec G304 - outputPath 是從配置來的，非使用者直接輸入
		// 使用更安全的檔案權限 0600
		file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		output = file
	}

	// 設置處理器選項
	opts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: addSource,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// 自定義時間格式
			if a.Key == slog.TimeKey {
				if t, ok := a.Value.Any().(time.Time); ok {
					// 使用台北時區
					loc, _ := time.LoadLocation("Asia/Taipei")
					if loc != nil {
						t = t.In(loc)
					}
					a.Value = slog.StringValue(t.Format("2006-01-02 15:04:05.000"))
				}
			}
			return a
		},
	}

	// 根據格式創建處理器
	var handler slog.Handler
	switch strings.ToLower(format) {
	case "json":
		handler = slog.NewJSONHandler(output, opts)
	default:
		handler = slog.NewTextHandler(output, opts)
	}

	// 包裝處理器以添加上下文資訊
	handler = &contextHandler{Handler: handler}

	// 設置預設日誌記錄器
	defaultLogger = slog.New(handler)
	slog.SetDefault(defaultLogger)

	return nil
}

// parseLevel 解析日誌級別
func parseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// contextHandler 從上下文中提取資訊的處理器
type contextHandler struct {
	slog.Handler
}

// Handle 處理日誌記錄
func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	// 從上下文中提取資訊
	if requestID, ok := ctx.Value(RequestIDKey).(string); ok && requestID != "" {
		r.AddAttrs(slog.String("request_id", requestID))
	}

	if userID, ok := ctx.Value(UserIDKey).(string); ok && userID != "" {
		r.AddAttrs(slog.String("user_id", userID))
	}

	return h.Handler.Handle(ctx, r)
}

// WithContext 返回帶有上下文的日誌記錄器
func WithContext(ctx context.Context) *slog.Logger {
	if defaultLogger == nil {
		return slog.Default()
	}
	return defaultLogger
}

// WithRequestID 添加請求 ID 到上下文
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

// WithUserID 添加用戶 ID 到上下文
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}

// Debug 記錄 Debug 級別日誌
func Debug(msg string, args ...any) {
	if defaultLogger != nil {
		defaultLogger.Debug(msg, args...)
	}
}

// Info 記錄 Info 級別日誌
func Info(msg string, args ...any) {
	if defaultLogger != nil {
		defaultLogger.Info(msg, args...)
	}
}

// Warn 記錄 Warn 級別日誌
func Warn(msg string, args ...any) {
	if defaultLogger != nil {
		defaultLogger.Warn(msg, args...)
	}
}

// Error 記錄 Error 級別日誌
func Error(msg string, args ...any) {
	if defaultLogger != nil {
		defaultLogger.Error(msg, args...)
	}
}

// LogError 記錄錯誤並包含堆疊資訊
func LogError(ctx context.Context, msg string, err error) {
	logger := WithContext(ctx)

	// 獲取呼叫者資訊
	pc, file, line, ok := runtime.Caller(1)
	if ok {
		fn := runtime.FuncForPC(pc)
		logger.Error(msg,
			slog.String("error", err.Error()),
			slog.String("file", file),
			slog.Int("line", line),
			slog.String("function", fn.Name()),
		)
	} else {
		logger.Error(msg, slog.String("error", err.Error()))
	}
}

// Metrics 記錄指標日誌
func Metrics(ctx context.Context, operation string, duration time.Duration, attrs ...slog.Attr) {
	logger := WithContext(ctx)

	baseAttrs := []any{
		slog.String("operation", operation),
		slog.Duration("duration", duration),
		slog.Float64("duration_ms", float64(duration.Milliseconds())),
	}

	// 添加額外屬性
	for _, attr := range attrs {
		baseAttrs = append(baseAttrs, attr)
	}

	logger.Info("metrics", baseAttrs...)
}
