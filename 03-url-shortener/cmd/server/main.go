package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/koopa0/system-design/03-url-shortener/internal/handler"
	"github.com/koopa0/system-design/03-url-shortener/internal/storage"
	"github.com/koopa0/system-design/03-url-shortener/pkg/snowflake"
)

// main 函數：應用程序入口
//
// 系統設計重點：
//   1. 依賴初始化順序（資料庫 → ID生成器 → 存儲層 → HTTP處理）
//   2. 優雅關閉（Graceful Shutdown）
//   3. 配置管理（環境變量 vs 配置文件）
//   4. 錯誤處理與日誌
func main() {
	// 1. 初始化日誌
	//
	// 系統設計考量：
	//   - 結構化日誌：便於解析和查詢
	//   - 日誌級別：開發用 DEBUG，生產用 INFO
	//   - 日誌輸出：開發用 stdout，生產用文件/日誌系統
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// 2. 讀取配置
	//
	// 系統設計考量：
	//   - 配置來源：環境變量（12-Factor App）
	//   - 敏感信息：不應硬編碼（使用 Secret Manager）
	//   - 默認值：便於本地開發
	cfg := loadConfig()
	logger.Info("configuration loaded", "addr", cfg.ServerAddr)

	// 3. 連接 PostgreSQL
	//
	// 系統設計考量：
	//   - 連接池：複用連接，減少開銷
	//   - 超時設置：防止無限等待
	//   - 健康檢查：啟動時驗證連接
	db, err := connectPostgres(cfg.DatabaseURL, logger)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// 4. 創建 Snowflake ID 生成器
	//
	// 系統設計考量：
	//   - 機器 ID：需要唯一（多實例部署時）
	//   - 獲取方式：環境變量 / 配置中心 / 自動分配
	//   - 範圍：0-1023（10 bit）
	idgen, err := snowflake.NewGenerator(cfg.MachineID)
	if err != nil {
		logger.Error("failed to create snowflake generator", "error", err)
		os.Exit(1)
	}
	logger.Info("snowflake generator initialized", "machine_id", cfg.MachineID)

	// 5. 創建存儲層
	//
	// 系統設計演進：
	//   V1：Memory（開發）
	//   V2：PostgreSQL（生產）
	//   V3：PostgreSQL + Redis（快取加速）
	//
	// 當前：使用 PostgreSQL
	// TODO：加入 Redis 快取層
	store := storage.NewPostgres(db)
	logger.Info("storage initialized", "type", "postgres")

	// 6. 創建 HTTP Handler
	h := handler.New(store, idgen, logger)

	// 7. 設置 HTTP Server
	//
	// 系統設計考量：
	//   - 超時設置：防止慢請求佔用資源
	//   - 讀超時：客戶端發送請求的時間
	//   - 寫超時：服務器返回響應的時間
	//   - 空閒超時：Keep-Alive 連接的最大空閒時間
	srv := &http.Server{
		Addr:         cfg.ServerAddr,
		Handler:      h.Routes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 8. 啟動服務器（非阻塞）
	//
	// 系統設計：
	//   - 使用 goroutine 啟動服務器
	//   - 主 goroutine 等待終止信號
	//   - 收到信號後優雅關閉
	go func() {
		logger.Info("starting server", "addr", cfg.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// 9. 等待終止信號（優雅關閉）
	//
	// 系統設計考量：
	//   - 信號處理：SIGINT（Ctrl+C）、SIGTERM（kill）
	//   - 優雅關閉：
	//     1. 停止接受新請求
	//     2. 等待現有請求完成（帶超時）
	//     3. 關閉資源（資料庫連接、文件句柄）
	//   - 超時設置：30 秒（根據業務調整）
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	logger.Info("received shutdown signal", "signal", sig.String())

	// 10. 優雅關閉服務器
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	logger.Info("server stopped gracefully")
}

// Config 應用配置
//
// 系統設計考量：
//   - 12-Factor App：配置與代碼分離
//   - 環境變量：適合容器化部署（Docker/Kubernetes）
//   - 敏感信息：不應出現在代碼庫中
type Config struct {
	ServerAddr  string // HTTP 服務器地址（如 ":8080"）
	DatabaseURL string // PostgreSQL 連接字符串
	MachineID   int64  // Snowflake 機器 ID（0-1023）
	// TODO: 加入更多配置
	// RedisAddr   string // Redis 地址
	// LogLevel    string // 日誌級別
}

// loadConfig 從環境變量加載配置
//
// 系統設計考量：
//   - 默認值：便於本地開發
//   - 環境變量：生產環境覆蓋默認值
//   - 驗證：確保配置合法（如端口範圍）
func loadConfig() *Config {
	return &Config{
		ServerAddr:  getEnv("SERVER_ADDR", ":8080"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/urlshortener?sslmode=disable"),
		MachineID:   getEnvInt64("MACHINE_ID", 1),
	}
}

// getEnv 獲取環境變量（帶默認值）
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt64 獲取 int64 環境變量（帶默認值）
func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		var result int64
		if _, err := fmt.Sscanf(value, "%d", &result); err == nil {
			return result
		}
	}
	return defaultValue
}

// connectPostgres 連接 PostgreSQL
//
// 系統設計考量：
//   1. 連接池設置：
//      - MaxOpenConns：最大打開連接數（避免耗盡資料庫連接）
//      - MaxIdleConns：最大空閒連接數（複用連接）
//      - ConnMaxLifetime：連接最大生命週期（避免長連接問題）
//
//   2. 超時設置：
//      - connect_timeout：連接超時（秒）
//      - statement_timeout：SQL 執行超時（毫秒）
//
//   3. 健康檢查：
//      - 啟動時 Ping 驗證連接
//      - 運行時定期健康檢查
func connectPostgres(databaseURL string, logger *slog.Logger) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}

	// 連接池配置
	//
	// 容量規劃：
	//   假設 QPS = 10,000
	//   每個請求耗時 = 10ms
	//   並發請求數 = 10,000 × 0.01 = 100
	//   連接池大小 ≥ 100
	//
	//   實際設置：預留余量
	//   MaxOpenConns = 150
	db.SetMaxOpenConns(150)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	// 驗證連接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, err
	}

	logger.Info("database connected", "max_open_conns", 150, "max_idle_conns", 25)
	return db, nil
}
