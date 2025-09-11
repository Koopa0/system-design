package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/koopa0/system-design/exercise-1/internal"
	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v3"
)

func main() {
	// 載入配置
	config, err := loadConfig("config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// 設定日誌
	var logger *slog.Logger
	if config.Log.Format == "json" {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: parseLogLevel(config.Log.Level),
		}))
	} else {
		logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: parseLogLevel(config.Log.Level),
		}))
	}
	slog.SetDefault(logger)

	// 連接 Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr:         config.Redis.Addr,
		Password:     config.Redis.Password,
		DB:           config.Redis.DB,
		PoolSize:     config.Redis.PoolSize,
		MinIdleConns: config.Redis.MinIdleConns,
		MaxRetries:   config.Redis.MaxRetries,
		ReadTimeout:  config.Redis.ReadTimeout,
		WriteTimeout: config.Redis.WriteTimeout,
	})

	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	// 連接 PostgreSQL
	// 使用 pgxpool 而非單一連線
	pgConfig, err := pgxpool.ParseConfig(config.PostgresDSN())
	if err != nil {
		logger.Error("failed to parse postgres config", "error", err)
		os.Exit(1)
	}

	pgConfig.MaxConns = config.Postgres.MaxConns
	pgConfig.MinConns = config.Postgres.MinConns

	pgPool, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		logger.Error("failed to connect to postgres", "error", err)
		os.Exit(1)
	}
	defer pgPool.Close()

	// 執行資料庫遷移
	if err := runMigrations(pgPool); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// 創建計數器和處理器
	counter := internal.NewCounter(redisClient, pgPool, config, logger)
	handler := internal.NewHandler(counter, logger)

	// 設定 HTTP 伺服器
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", config.Server.Port),
		Handler:      handler.Routes(),
		ReadTimeout:  config.Server.ReadTimeout,
		WriteTimeout: config.Server.WriteTimeout,
		IdleTimeout:  120 * time.Second,
	}

	// 啟動伺服器
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("starting server", "port", config.Server.Port)
		serverErrors <- srv.ListenAndServe()
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}

	case sig := <-shutdown:
		logger.Info("shutdown signal received", "signal", sig)

		// 給予 30 秒時間完成當前請求
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// 關閉計數器
		counter.Shutdown()

		// 關閉 HTTP 伺服器
		if err := srv.Shutdown(ctx); err != nil {
			logger.Error("failed to shutdown server", "error", err)
			// 強制關閉伺服器
			if closeErr := srv.Close(); closeErr != nil {
				logger.Error("failed to force close server", "error", closeErr)
			}
		}
	}

	logger.Info("server stopped")
}

// loadConfig 載入配置檔案
func loadConfig(path string) (*internal.Config, error) {
	// #nosec G304 - path 是硬編碼的配置檔案路徑，非使用者輸入
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var config internal.Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &config, nil
}

// parseLogLevel 解析日誌級別
func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// runMigrations 執行資料庫遷移
func runMigrations(pool *pgxpool.Pool) error {
	ctx := context.Background()

	// 讀取 SQL 檔案
	schema, err := os.ReadFile("internal/migrations/migrations/000001_init_schema.up.sql")
	if err != nil {
		return fmt.Errorf("read schema file: %w", err)
	}

	// 執行遷移
	if _, err = pool.Exec(ctx, string(schema)); err != nil {
		return fmt.Errorf("execute migration: %w", err)
	}

	return nil
}
