package testutils

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/koopa0/system-design/exercise-1/internal"
	"github.com/stretchr/testify/require"
)

// DefaultTestConfig 返回測試用的預設配置
func DefaultTestConfig() *internal.Config {
	cfg := &internal.Config{}

	// Server 配置
	cfg.Server.Port = 8080
	cfg.Server.ReadTimeout = 5 * time.Second
	cfg.Server.WriteTimeout = 10 * time.Second

	// Redis 配置
	cfg.Redis.PoolSize = 10
	cfg.Redis.MinIdleConns = 5
	cfg.Redis.MaxRetries = 3
	cfg.Redis.ReadTimeout = 3 * time.Second
	cfg.Redis.WriteTimeout = 3 * time.Second

	// PostgreSQL 配置
	cfg.Postgres.MaxConns = 10
	cfg.Postgres.MinConns = 2

	// Counter 配置
	cfg.Counter.BatchSize = 100
	cfg.Counter.FlushInterval = 100 * time.Millisecond
	cfg.Counter.EnableFallback = true
	cfg.Counter.FallbackThreshold = 3

	// Log 配置
	cfg.Log.Level = "warn"
	cfg.Log.Format = "json"

	return cfg
}

// AssertEventuallyEqual 斷言最終一致性
//
// 在分散式系統測試中，有時需要等待資料同步。
// 這個函數會重試檢查直到條件滿足或超時。
func AssertEventuallyEqual(t testing.TB, expected interface{}, actualFunc func() interface{}, timeout time.Duration, message string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		if expected == actualFunc() {
			return
		}
		<-ticker.C
	}

	require.Equal(t, expected, actualFunc(), message)
}

// MakeHTTPRequest 執行 HTTP 請求的輔助函數
func MakeHTTPRequest(t testing.TB, handler http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		if str, ok := body.(string); ok {
			bodyReader = strings.NewReader(str)
		} else {
			jsonBytes, err := json.Marshal(body)
			require.NoError(t, err)
			bodyReader = strings.NewReader(string(jsonBytes))
		}
	}

	req := httptest.NewRequest(method, path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	return recorder
}

// ParseJSONResponse 解析 JSON 響應
func ParseJSONResponse(t testing.TB, recorder *httptest.ResponseRecorder, target interface{}) {
	t.Helper()

	err := json.NewDecoder(recorder.Body).Decode(target)
	require.NoError(t, err, "failed to parse JSON response")
}

// WaitForCondition 等待條件滿足
func WaitForCondition(t testing.TB, condition func() bool, timeout time.Duration, message string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for condition: %s", message)
		case <-ticker.C:
			if condition() {
				return
			}
		}
	}
}

// RunConcurrently 並發執行測試函數
func RunConcurrently(t testing.TB, concurrency int, iterations int, fn func(workerID, iteration int)) {
	t.Helper()

	done := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		workerID := i
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < iterations; j++ {
				fn(workerID, j)
			}
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < concurrency; i++ {
		<-done
	}
}

// GenerateTestData 生成測試資料
type TestData struct {
	CounterNames []string
	UserIDs      []string
}

// GenerateTestData 生成測試資料
func GenerateBasicTestData() *TestData {
	return &TestData{
		CounterNames: []string{
			"test_counter_1",
			"test_counter_2",
			"daily_active_users",
			"total_games_played",
			"api_requests",
		},
		UserIDs: []string{
			"user_001",
			"user_002",
			"user_003",
			"user_004",
			"user_005",
		},
	}
}

// AssertCounterValue 驗證計數器值
func AssertCounterValue(t testing.TB, counter *internal.Counter, name string, expected int64) {
	t.Helper()

	ctx := context.Background()
	actual, err := counter.GetValue(ctx, name)
	require.NoError(t, err)
	require.Equal(t, expected, actual, "counter %s should have value %d, got %d", name, expected, actual)
}

// AssertNoError 斷言沒有錯誤
func AssertNoError(t testing.TB, err error, msgAndArgs ...interface{}) {
	t.Helper()
	require.NoError(t, err, msgAndArgs...)
}

// AssertError 斷言有錯誤
func AssertError(t testing.TB, err error, msgAndArgs ...interface{}) {
	t.Helper()
	require.Error(t, err, msgAndArgs...)
}

// BenchmarkHelper 基準測試輔助結構
type BenchmarkHelper struct {
	Env     *TestEnvironment
	Counter *internal.Counter
	Config  *internal.Config
}

// SetupBenchmark 設置基準測試環境
func SetupBenchmark(b *testing.B) *BenchmarkHelper {
	b.Helper()

	env := SetupTestEnvironment(b)
	config := DefaultTestConfig()

	counter := internal.NewCounter(
		env.RedisClient,
		env.PostgresPool,
		config,
		env.Logger,
	)

	return &BenchmarkHelper{
		Env:     env,
		Counter: counter,
		Config:  config,
	}
}

// CleanupBenchmark 清理基準測試環境
func (bh *BenchmarkHelper) Cleanup() {
	bh.Counter.Shutdown()
	bh.Env.Cleanup()
}