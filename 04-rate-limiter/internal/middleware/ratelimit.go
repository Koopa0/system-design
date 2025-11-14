// Package middleware 提供 HTTP 限流中介軟體。
//
// 設計目標：
//   將限流邏輯整合到 HTTP 請求處理流程
//   支援多種限流策略和維度
package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/koopa0/system-design/04-rate-limiter/internal/limiter"
	"github.com/google/uuid"
)

// RateLimiterFunc 定義限流函數介面。
//
// 設計考量：
//   使用函數介面而非具體型別
//   提供彈性支援不同的限流器實作
type RateLimiterFunc func(ctx context.Context, key string) (bool, error)

// RateLimitConfig 限流中介軟體設定。
type RateLimitConfig struct {
	// KeyFunc 從請求提取限流 key
	// 範例：
	//   - IP 限流：func(r *http.Request) string { return r.RemoteAddr }
	//   - User 限流：func(r *http.Request) string { return getUserID(r) }
	//   - API 限流：func(r *http.Request) string { return r.URL.Path }
	KeyFunc func(r *http.Request) string

	// Limiter 限流器函數
	Limiter RateLimiterFunc

	// OnRateLimited 限流觸發時的處理
	// 預設：返回 429 Too Many Requests
	OnRateLimited http.HandlerFunc
}

// RateLimit 建立限流中介軟體。
//
// 使用範例：
//
//	// IP 限流
//	ipLimiter := limiter.NewDistributedTokenBucket(redisClient, 100, 100)
//	middleware := RateLimit(RateLimitConfig{
//	    KeyFunc: func(r *http.Request) string {
//	        return "ip:" + r.RemoteAddr
//	    },
//	    Limiter: ipLimiter.Allow,
//	})
//
//	http.Handle("/api/", middleware(apiHandler))
func RateLimit(config RateLimitConfig) func(http.Handler) http.Handler {
	// 設定預設值
	if config.OnRateLimited == nil {
		config.OnRateLimited = defaultRateLimitedHandler
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 提取限流 key
			key := config.KeyFunc(r)

			// 設定逾時上下文（避免 Redis 呼叫過久）
			ctx, cancel := context.WithTimeout(r.Context(), 100*time.Millisecond)
			defer cancel()

			// 檢查限流
			allowed, err := config.Limiter(ctx, key)
			if err != nil {
				// 錯誤處理：記錄日誌但允許請求通過
				// Trade-off: 可用性優先
				// TODO: 增加監控告警
				next.ServeHTTP(w, r)
				return
			}

			if !allowed {
				config.OnRateLimited(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// defaultRateLimitedHandler 預設的限流回應。
func defaultRateLimitedHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-RateLimit-Retry-After", "1")
	w.WriteHeader(http.StatusTooManyRequests)
	w.Write([]byte(`{"error":"rate limit exceeded"}`))
}

// MultiDimensionRateLimit 多維度限流中介軟體。
//
// 設計場景：
//   同時限制 IP、User、API 三個維度
//
// 使用範例：
//
//	middleware := MultiDimensionRateLimit(MultiDimensionConfig{
//	    Dimensions: []DimensionConfig{
//	        {
//	            Name: "ip",
//	            KeyFunc: func(r *http.Request) string { return r.RemoteAddr },
//	            Limiter: ipLimiter.Allow,
//	        },
//	        {
//	            Name: "user",
//	            KeyFunc: getUserID,
//	            Limiter: userLimiter.Allow,
//	        },
//	    },
//	})
type MultiDimensionConfig struct {
	Dimensions    []DimensionConfig
	OnRateLimited http.HandlerFunc
}

type DimensionConfig struct {
	Name    string
	KeyFunc func(r *http.Request) string
	Limiter RateLimiterFunc
}

func MultiDimensionRateLimit(config MultiDimensionConfig) func(http.Handler) http.Handler {
	if config.OnRateLimited == nil {
		config.OnRateLimited = defaultRateLimitedHandler
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), 100*time.Millisecond)
			defer cancel()

			// 依序檢查每個維度
			for _, dim := range config.Dimensions {
				key := dim.KeyFunc(r)
				allowed, err := dim.Limiter(ctx, key)

				if err != nil {
					// 降級：允許請求
					continue
				}

				if !allowed {
					// 任一維度超限則拒絕
					w.Header().Set("X-RateLimit-Dimension", dim.Name)
					config.OnRateLimited(w, r)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// SlidingWindowRateLimitAdapter 滑動視窗限流適配器。
//
// 問題：滑動視窗需要 requestID，但 RateLimiterFunc 只接受 key
// 解決：建立適配器自動產生 requestID
func SlidingWindowRateLimitAdapter(sw *limiter.DistributedSlidingWindow) RateLimiterFunc {
	return func(ctx context.Context, key string) (bool, error) {
		requestID := uuid.New().String()
		return sw.Allow(ctx, key, requestID)
	}
}
