// Rate Limiter 示範服務
//
// 展示三種限流演算法和多維度限流的實際應用
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Koopa0/system-design/04-rate-limiter/internal/limiter"
	"github.com/Koopa0/system-design/04-rate-limiter/internal/middleware"
	"github.com/redis/go-redis/v9"
)

func main() {
	// 初始化 Redis 客戶端
	redisClient := redis.NewClient(&redis.Options{
		Addr:         getEnv("REDIS_ADDR", "localhost:6379"),
		Password:     getEnv("REDIS_PASSWORD", ""),
		DB:           0,
		PoolSize:     20,
		MinIdleConns: 5,
		ReadTimeout:  100 * time.Millisecond,
		WriteTimeout: 100 * time.Millisecond,
	})

	// 測試 Redis 連線
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Printf("警告：Redis 連線失敗，將使用單機限流器：%v", err)
		startWithLocalLimiter()
		return
	}

	log.Println("已連線到 Redis，使用分散式限流")
	startWithDistributedLimiter(redisClient)
}

// startWithLocalLimiter 使用本地限流器啟動服務
func startWithLocalLimiter() {
	mux := http.NewServeMux()

	// 範例 1：Token Bucket 限流
	tokenBucketLimiter := limiter.NewTokenBucket(10, 2) // 容量10，每秒2個
	mux.Handle("/api/token-bucket", tokenBucketMiddleware(tokenBucketLimiter)(
		http.HandlerFunc(handleAPI),
	))

	// 範例 2：Leaky Bucket 限流
	leakyBucketLimiter := limiter.NewLeakyBucket(10, 2) // 容量10，每秒2個
	mux.Handle("/api/leaky-bucket", leakyBucketMiddleware(leakyBucketLimiter)(
		http.HandlerFunc(handleAPI),
	))

	// 範例 3：Sliding Window 限流
	slidingWindowLimiter := limiter.NewSlidingWindow(10, time.Minute) // 1分鐘10個
	mux.Handle("/api/sliding-window", slidingWindowMiddleware(slidingWindowLimiter)(
		http.HandlerFunc(handleAPI),
	))

	// 範例 4：Sliding Window Counter 限流
	swcLimiter := limiter.NewSlidingWindowCounter(100, time.Minute, 60) // 1分鐘100個，60個桶
	mux.Handle("/api/sliding-window-counter", slidingWindowCounterMiddleware(swcLimiter)(
		http.HandlerFunc(handleAPI),
	))

	startServer(mux)
}

// startWithDistributedLimiter 使用分散式限流器啟動服務
func startWithDistributedLimiter(redisClient *redis.Client) {
	mux := http.NewServeMux()

	// 建立不同維度的限流器
	ipLimiter := limiter.NewDistributedTokenBucket(redisClient, 100, 100)       // IP: 100 req/s
	userLimiter := limiter.NewDistributedTokenBucket(redisClient, 50, 50)       // User: 50 req/s
	apiLimiter := limiter.NewDistributedTokenBucket(redisClient, 1000, 1000)    // API: 1000 req/s
	globalLimiter := limiter.NewDistributedTokenBucket(redisClient, 5000, 5000) // Global: 5000 req/s

	// 範例 1：單一維度限流（IP）
	ipRateLimit := middleware.RateLimit(middleware.RateLimitConfig{
		KeyFunc: func(r *http.Request) string {
			return "ip:" + r.RemoteAddr
		},
		Limiter: ipLimiter.Allow,
	})
	mux.Handle("/api/ip-limited", ipRateLimit(http.HandlerFunc(handleAPI)))

	// 範例 2：多維度限流（IP + User + API）
	multiDimRateLimit := middleware.MultiDimensionRateLimit(middleware.MultiDimensionConfig{
		Dimensions: []middleware.DimensionConfig{
			{
				Name: "ip",
				KeyFunc: func(r *http.Request) string {
					return "ip:" + r.RemoteAddr
				},
				Limiter: ipLimiter.Allow,
			},
			{
				Name: "user",
				KeyFunc: func(r *http.Request) string {
					// 實際應從認證 token 提取
					return "user:" + r.Header.Get("X-User-ID")
				},
				Limiter: userLimiter.Allow,
			},
			{
				Name: "api",
				KeyFunc: func(r *http.Request) string {
					return "api:" + r.URL.Path
				},
				Limiter: apiLimiter.Allow,
			},
		},
	})
	mux.Handle("/api/multi-dimension", multiDimRateLimit(http.HandlerFunc(handleAPI)))

	// 範例 3：滑動視窗限流
	swLimiter := limiter.NewDistributedSlidingWindow(redisClient, 100, time.Minute)
	swRateLimit := middleware.RateLimit(middleware.RateLimitConfig{
		KeyFunc: func(r *http.Request) string {
			return "sw:" + r.RemoteAddr
		},
		Limiter: middleware.SlidingWindowRateLimitAdapter(swLimiter),
	})
	mux.Handle("/api/sliding-window", swRateLimit(http.HandlerFunc(handleAPI)))

	// 範例 4：全域限流
	globalRateLimit := middleware.RateLimit(middleware.RateLimitConfig{
		KeyFunc: func(r *http.Request) string {
			return "global"
		},
		Limiter: globalLimiter.Allow,
	})
	mux.Handle("/api/global-limited", globalRateLimit(http.HandlerFunc(handleAPI)))

	startServer(mux)
}

// handleAPI 範例 API 處理器
func handleAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"message":"success","path":"%s","time":"%s"}`,
		r.URL.Path,
		time.Now().Format(time.RFC3339),
	)
}

// startServer 啟動 HTTP 服務
func startServer(handler http.Handler) {
	port := getEnv("PORT", "8080")
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 優雅關閉
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		log.Println("正在關閉服務...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("關閉服務錯誤：%v", err)
		}
	}()

	log.Printf("服務啟動於 http://localhost:%s", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("服務啟動失敗：%v", err)
	}
}

// 本地限流器的中介軟體適配器
func tokenBucketMiddleware(tb *limiter.TokenBucket) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !tb.Allow() {
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func leakyBucketMiddleware(lb *limiter.LeakyBucket) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !lb.Allow() {
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func slidingWindowMiddleware(sw *limiter.SlidingWindow) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !sw.Allow() {
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func slidingWindowCounterMiddleware(swc *limiter.SlidingWindowCounter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !swc.Allow() {
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
