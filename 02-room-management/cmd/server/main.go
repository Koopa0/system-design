package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/koopa0/system-design/exercise-2/internal"
)

func main() {
	// 解析命令行參數
	var (
		port     = flag.Int("port", 8080, "服務器端口")
		logLevel = flag.String("log-level", "info", "日誌級別 (debug, info, warn, error)")
		logFormat = flag.String("log-format", "text", "日誌格式 (text, json)")
	)
	flag.Parse()

	// 設置日誌
	logger := setupLogger(*logLevel, *logFormat)

	// 創建房間管理器
	manager := internal.NewManager(logger)

	// 創建 HTTP 處理器
	handler := internal.NewHandler(manager, logger)

	// 創建 WebSocket Hub
	wsHub := internal.NewWebSocketHub(manager, logger)

	// 設置路由
	mux := http.NewServeMux()
	
	// HTTP API 路由
	mux.Handle("/", handler.Routes())
	
	// WebSocket 路由
	mux.HandleFunc("/ws/rooms/{room_id}", wsHub.ServeWS)

	// 創建 HTTP 服務器
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", *port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 啟動服務器
	go func() {
		logger.Info("遊戲房間服務器啟動", 
			"port", *port,
			"log_level", *logLevel,
			"log_format", *logFormat)
		
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("服務器啟動失敗", "error", err)
			os.Exit(1)
		}
	}()

	// 等待中斷信號
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("收到關閉信號，開始優雅關閉...")

	// 優雅關閉
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 停止接受新連接
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("服務器關閉失敗", "error", err)
	}

	// 停止房間管理器
	manager.Stop()

	// 停止 WebSocket Hub
	wsHub.Stop()

	logger.Info("服務器已關閉")
}

// setupLogger 設置日誌
func setupLogger(level, format string) *slog.Logger {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: logLevel,
		AddSource: level == "debug", // debug 模式顯示源碼位置
	}

	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}