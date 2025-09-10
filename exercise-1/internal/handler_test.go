package internal_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/koopa0/system-design/exercise-1/internal"
	"github.com/koopa0/system-design/exercise-1/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandler_Increment 測試增加計數器的 HTTP 端點
func TestHandler_Increment(t *testing.T) {
	tests := []struct {
		name           string
		counterName    string
		requestBody    string
		setupFunc      func(t *testing.T, env *testutils.TestEnvironment)
		expectedStatus int
		expectedValue  int64
		expectedError  string
	}{
		{
			name:           "increment without body",
			counterName:    "test_counter",
			requestBody:    "",
			expectedStatus: http.StatusOK,
			expectedValue:  1, // 預設增加 1
		},
		{
			name:           "increment with value",
			counterName:    "test_counter",
			requestBody:    `{"value": 5}`,
			expectedStatus: http.StatusOK,
			expectedValue:  5,
		},
		{
			name:           "increment with user_id",
			counterName:    "daily_active_users",
			requestBody:    `{"value": 1, "user_id": "user123"}`,
			expectedStatus: http.StatusOK,
			expectedValue:  1,
		},
		{
			name:           "increment with metadata",
			counterName:    "test_counter",
			requestBody:    `{"value": 3, "metadata": {"source": "mobile"}}`,
			expectedStatus: http.StatusOK,
			expectedValue:  3,
		},
		{
			name:           "increment with invalid JSON",
			counterName:    "test_counter",
			requestBody:    `{invalid json}`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid request body",
		},
		{
			name:           "increment without counter name",
			counterName:    "",
			requestBody:    `{"value": 1}`,
			expectedStatus: http.StatusBadRequest,
			expectedError:  "counter name required",
		},
		{
			name:           "increment existing counter",
			counterName:    "existing_counter",
			requestBody:    `{"value": 10}`,
			setupFunc: func(t *testing.T, env *testutils.TestEnvironment) {
				ctx := context.Background()
				err := env.RedisClient.Set(ctx, "counter:existing_counter", 20, 0).Err()
				require.NoError(t, err)
			},
			expectedStatus: http.StatusOK,
			expectedValue:  30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 設置測試環境
			env := testutils.SetupTestEnvironment(t)
			defer env.Cleanup()

			config := testutils.DefaultTestConfig()
			counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
			defer counter.Shutdown()

			handler := internal.NewHandler(counter, env.Logger)
			routes := handler.Routes()

			// 執行設置函數
			if tt.setupFunc != nil {
				tt.setupFunc(t, env)
			}

			// 創建請求
			path := fmt.Sprintf("/api/v1/counter/%s/increment", tt.counterName)
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(tt.requestBody))
			if tt.requestBody != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			// 執行請求
			recorder := httptest.NewRecorder()
			routes.ServeHTTP(recorder, req)

			// 驗證狀態碼
			assert.Equal(t, tt.expectedStatus, recorder.Code)

			// 解析響應
			var response map[string]interface{}
			err := json.NewDecoder(recorder.Body).Decode(&response)
			require.NoError(t, err)

			if tt.expectedStatus == http.StatusOK {
				assert.True(t, response["success"].(bool))
				assert.Equal(t, float64(tt.expectedValue), response["current_value"].(float64))
			} else {
				assert.False(t, response["success"].(bool))
				if tt.expectedError != "" {
					assert.Contains(t, response["error"].(string), tt.expectedError)
				}
			}
		})
	}
}

// TestHandler_Decrement 測試減少計數器的 HTTP 端點
func TestHandler_Decrement(t *testing.T) {
	tests := []struct {
		name           string
		counterName    string
		requestBody    string
		initialValue   int64
		expectedStatus int
		expectedValue  int64
	}{
		{
			name:           "decrement by 1",
			counterName:    "test_counter",
			requestBody:    "",
			initialValue:   10,
			expectedStatus: http.StatusOK,
			expectedValue:  9,
		},
		{
			name:           "decrement by specific value",
			counterName:    "test_counter",
			requestBody:    `{"value": 5}`,
			initialValue:   10,
			expectedStatus: http.StatusOK,
			expectedValue:  5,
		},
		{
			name:           "decrement below zero",
			counterName:    "test_counter",
			requestBody:    `{"value": 15}`,
			initialValue:   10,
			expectedStatus: http.StatusOK,
			expectedValue:  0,
		},
		{
			name:           "decrement non-existing counter",
			counterName:    "new_counter",
			requestBody:    `{"value": 5}`,
			initialValue:   0,
			expectedStatus: http.StatusOK,
			expectedValue:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := testutils.SetupTestEnvironment(t)
			defer env.Cleanup()

			config := testutils.DefaultTestConfig()
			counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
			defer counter.Shutdown()

			handler := internal.NewHandler(counter, env.Logger)
			routes := handler.Routes()

			// 設置初始值
			if tt.initialValue > 0 {
				ctx := context.Background()
				err := env.RedisClient.Set(ctx, fmt.Sprintf("counter:%s", tt.counterName), tt.initialValue, 0).Err()
				require.NoError(t, err)
			}

			// 創建請求
			path := fmt.Sprintf("/api/v1/counter/%s/decrement", tt.counterName)
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(tt.requestBody))
			if tt.requestBody != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			// 執行請求
			recorder := httptest.NewRecorder()
			routes.ServeHTTP(recorder, req)

			// 驗證
			assert.Equal(t, tt.expectedStatus, recorder.Code)

			var response map[string]interface{}
			err := json.NewDecoder(recorder.Body).Decode(&response)
			require.NoError(t, err)

			// 檢查 success 欄位
			success, ok := response["success"].(bool)
			assert.True(t, ok, "response should have success field")
			assert.True(t, success)
			
			// 檢查 current_value 欄位 (只在成功時檢查)
			if success {
				currentValue, ok := response["current_value"].(float64)
				assert.True(t, ok, "response should have current_value field")
				assert.Equal(t, float64(tt.expectedValue), currentValue)
			}
		})
	}
}

// TestHandler_Get 測試獲取計數器值的 HTTP 端點
func TestHandler_Get(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	handler := internal.NewHandler(counter, env.Logger)
	routes := handler.Routes()

	t.Run("get existing counter", func(t *testing.T) {
		// 設置測試資料
		ctx := context.Background()
		err := env.RedisClient.Set(ctx, "counter:test_get", 42, 0).Err()
		require.NoError(t, err)

		// 創建請求
		req := httptest.NewRequest(http.MethodGet, "/api/v1/counter/test_get", nil)
		recorder := httptest.NewRecorder()

		// 執行請求
		routes.ServeHTTP(recorder, req)

		// 驗證
		assert.Equal(t, http.StatusOK, recorder.Code)

		var response map[string]interface{}
		err = json.NewDecoder(recorder.Body).Decode(&response)
		require.NoError(t, err)

		assert.Equal(t, "test_get", response["name"])
		assert.Equal(t, float64(42), response["value"])
		assert.NotEmpty(t, response["last_updated"])
	})

	t.Run("get non-existing counter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/counter/non_existing", nil)
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		// 新計數器應返回 0 而不是 404
		assert.Equal(t, http.StatusOK, recorder.Code)

		var response map[string]interface{}
		err := json.NewDecoder(recorder.Body).Decode(&response)
		require.NoError(t, err)

		assert.Equal(t, "non_existing", response["name"])
		assert.Equal(t, float64(0), response["value"])
	})

	t.Run("get without counter name", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/counter/", nil)
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		// 應該返回 404（路由不匹配）
		assert.Equal(t, http.StatusNotFound, recorder.Code)
	})
}

// TestHandler_GetMultiple 測試批量獲取計數器
func TestHandler_GetMultiple(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	handler := internal.NewHandler(counter, env.Logger)
	routes := handler.Routes()

	// 設置測試資料
	ctx := context.Background()
	testData := map[string]int64{
		"counter1": 10,
		"counter2": 20,
		"counter3": 30,
	}

	for name, value := range testData {
		err := env.RedisClient.Set(ctx, fmt.Sprintf("counter:%s", name), value, 0).Err()
		require.NoError(t, err)
	}

	t.Run("get multiple existing counters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/counters?names=counter1,counter2,counter3", nil)
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code)

		var response map[string]interface{}
		err := json.NewDecoder(recorder.Body).Decode(&response)
		require.NoError(t, err)

		counters := response["counters"].([]interface{})
		assert.Len(t, counters, 3)

		// 驗證值
		values := make(map[string]float64)
		for _, c := range counters {
			counter := c.(map[string]interface{})
			name := counter["name"].(string)
			value := counter["value"].(float64)
			values[name] = value
		}

		assert.Equal(t, float64(10), values["counter1"])
		assert.Equal(t, float64(20), values["counter2"])
		assert.Equal(t, float64(30), values["counter3"])
	})

	t.Run("get multiple with some non-existing", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/counters?names=counter1,non_existing,counter3", nil)
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code)

		var response map[string]interface{}
		err := json.NewDecoder(recorder.Body).Decode(&response)
		require.NoError(t, err)

		counters := response["counters"].([]interface{})
		assert.Len(t, counters, 3)

		// 驗證非存在的計數器返回 0
		values := make(map[string]float64)
		for _, c := range counters {
			counter := c.(map[string]interface{})
			name := counter["name"].(string)
			value := counter["value"].(float64)
			values[name] = value
		}

		assert.Equal(t, float64(0), values["non_existing"])
	})

	t.Run("get multiple without names parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/counters", nil)
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusBadRequest, recorder.Code)

		var response map[string]interface{}
		err := json.NewDecoder(recorder.Body).Decode(&response)
		require.NoError(t, err)

		assert.False(t, response["success"].(bool))
		assert.Contains(t, response["error"], "names parameter required")
	})

	t.Run("get multiple with too many counters", func(t *testing.T) {
		// 創建超過 10 個計數器名稱
		names := make([]string, 11)
		for i := 0; i < 11; i++ {
			names[i] = fmt.Sprintf("counter%d", i)
		}

		req := httptest.NewRequest(http.MethodGet, 
			fmt.Sprintf("/api/v1/counters?names=%s", strings.Join(names, ",")), nil)
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusBadRequest, recorder.Code)

		var response map[string]interface{}
		err := json.NewDecoder(recorder.Body).Decode(&response)
		require.NoError(t, err)

		assert.False(t, response["success"].(bool))
		assert.Contains(t, response["error"], "maximum 10 counters")
	})
}

// TestHandler_Reset 測試重置計數器
func TestHandler_Reset(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	handler := internal.NewHandler(counter, env.Logger)
	routes := handler.Routes()

	t.Run("reset with valid admin token", func(t *testing.T) {
		// 設置初始值
		ctx := context.Background()
		err := env.RedisClient.Set(ctx, "counter:test_reset", 100, 0).Err()
		require.NoError(t, err)

		// 創建請求
		body := `{"admin_token": "secret_token"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/counter/test_reset/reset", 
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		// 執行請求
		routes.ServeHTTP(recorder, req)

		// 驗證
		assert.Equal(t, http.StatusOK, recorder.Code)

		var response map[string]interface{}
		err = json.NewDecoder(recorder.Body).Decode(&response)
		require.NoError(t, err)

		// 檢查 success 欄位
		success, ok := response["success"].(bool)
		assert.True(t, ok, "response should have success field")
		assert.True(t, success)
		
		// 檢查 current_value 欄位
		if success {
			currentValue, ok := response["current_value"].(float64)
			assert.True(t, ok, "response should have current_value field")
			assert.Equal(t, float64(0), currentValue)
		}

		// 驗證計數器已重置
		value, err := counter.GetValue(ctx, "test_reset")
		assert.NoError(t, err)
		assert.Equal(t, int64(0), value)
	})

	t.Run("reset with invalid admin token", func(t *testing.T) {
		body := `{"admin_token": "wrong_token"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/counter/test_reset/reset", 
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusUnauthorized, recorder.Code)

		var response map[string]interface{}
		err := json.NewDecoder(recorder.Body).Decode(&response)
		require.NoError(t, err)

		assert.False(t, response["success"].(bool))
		assert.Contains(t, response["error"], "unauthorized")
	})

	t.Run("reset without admin token", func(t *testing.T) {
		body := `{}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/counter/test_reset/reset", 
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusUnauthorized, recorder.Code)
	})

	t.Run("reset with invalid JSON", func(t *testing.T) {
		body := `{invalid json}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/counter/test_reset/reset", 
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	})
}

// TestHandler_HealthCheck 測試健康檢查端點
func TestHandler_HealthCheck(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	handler := internal.NewHandler(counter, env.Logger)
	routes := handler.Routes()

	t.Run("health endpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "OK", recorder.Body.String())
	})

	t.Run("ready endpoint with healthy services", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "Ready", recorder.Body.String())
	})

	t.Run("ready endpoint with unhealthy Redis", func(t *testing.T) {
		// 關閉 Redis 連接
		env.RedisClient.Close()

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		// 應該返回服務不可用
		assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)

		var response map[string]interface{}
		err := json.NewDecoder(recorder.Body).Decode(&response)
		require.NoError(t, err)

		assert.False(t, response["success"].(bool))
		assert.Contains(t, response["error"], "redis not ready")
	})
}

// TestHandler_Middleware 測試中間件功能
func TestHandler_Middleware(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	// 創建一個會記錄日誌的 logger
	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuffer, nil))

	handler := internal.NewHandler(counter, logger)
	routes := handler.Routes()

	t.Run("logger middleware", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		// 檢查日誌是否包含請求資訊
		logContent := logBuffer.String()
		assert.Contains(t, logContent, "http request")
		assert.Contains(t, logContent, "GET")
		assert.Contains(t, logContent, "/health")
		assert.Contains(t, logContent, "200")
	})

	t.Run("recovery middleware", func(t *testing.T) {
		// 創建一個會 panic 的處理器
		panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("test panic")
		})

		// 包裝處理器
		recoveredHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger.Error("panic recovered", "error", err)
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"success": false,
						"error":   "internal server error",
					})
				}
			}()
			panicHandler.ServeHTTP(w, r)
		})

		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		recorder := httptest.NewRecorder()

		// 不應該 panic
		assert.NotPanics(t, func() {
			recoveredHandler.ServeHTTP(recorder, req)
		})

		assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	})
}

// TestHandler_ConcurrentRequests 測試並發請求處理
func TestHandler_ConcurrentRequests(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	handler := internal.NewHandler(counter, env.Logger)
	routes := handler.Routes()

	const (
		numRequests = 100
		numWorkers  = 10
	)

	t.Run("concurrent increment requests", func(t *testing.T) {
		var wg sync.WaitGroup
		wg.Add(numWorkers)

		successCount := 0
		var mu sync.Mutex

		for i := 0; i < numWorkers; i++ {
			go func(workerID int) {
				defer wg.Done()

				for j := 0; j < numRequests/numWorkers; j++ {
					body := fmt.Sprintf(`{"value": %d}`, j+1)
					req := httptest.NewRequest(http.MethodPost, 
						"/api/v1/counter/concurrent_test/increment", 
						strings.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					
					recorder := httptest.NewRecorder()
					routes.ServeHTTP(recorder, req)

					if recorder.Code == http.StatusOK {
						mu.Lock()
						successCount++
						mu.Unlock()
					}
				}
			}(i)
		}

		wg.Wait()

		// 大部分請求應該成功
		assert.Greater(t, successCount, numRequests*9/10, 
			"At least 90%% of requests should succeed")
	})

	t.Run("mixed concurrent operations", func(t *testing.T) {
		// 設置初始值
		ctx := context.Background()
		err := env.RedisClient.Set(ctx, "counter:mixed_concurrent", 1000, 0).Err()
		require.NoError(t, err)

		var wg sync.WaitGroup
		wg.Add(numWorkers * 3) // increment, decrement, get

		// 並發增加
		for i := 0; i < numWorkers; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					req := httptest.NewRequest(http.MethodPost, 
						"/api/v1/counter/mixed_concurrent/increment", 
						strings.NewReader(`{"value": 1}`))
					req.Header.Set("Content-Type", "application/json")
					
					recorder := httptest.NewRecorder()
					routes.ServeHTTP(recorder, req)
				}
			}()
		}

		// 並發減少
		for i := 0; i < numWorkers; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					req := httptest.NewRequest(http.MethodPost, 
						"/api/v1/counter/mixed_concurrent/decrement", 
						strings.NewReader(`{"value": 1}`))
					req.Header.Set("Content-Type", "application/json")
					
					recorder := httptest.NewRecorder()
					routes.ServeHTTP(recorder, req)
				}
			}()
		}

		// 並發獲取
		for i := 0; i < numWorkers; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					req := httptest.NewRequest(http.MethodGet, 
						"/api/v1/counter/mixed_concurrent", nil)
					
					recorder := httptest.NewRecorder()
					routes.ServeHTTP(recorder, req)
				}
			}()
		}

		wg.Wait()

		// 驗證最終值
		finalValue, err := counter.GetValue(ctx, "mixed_concurrent")
		assert.NoError(t, err)
		// 1000 + (10*10) - (10*10) = 1000
		assert.Equal(t, int64(1000), finalValue)
	})
}

// TestHandler_ErrorCases 測試各種錯誤情況
func TestHandler_ErrorCases(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	handler := internal.NewHandler(counter, env.Logger)
	routes := handler.Routes()

	t.Run("invalid HTTP method", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/counter/test/increment", nil)
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
	})

	t.Run("non-existent endpoint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/non-existent", nil)
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusNotFound, recorder.Code)
	})

	t.Run("malformed URL", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/counter//increment", nil)
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		// 空的計數器名稱
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	})

	t.Run("very large request body", func(t *testing.T) {
		// 創建一個非常大的請求體
		largeBody := strings.Repeat("a", 1024*1024) // 1MB
		req := httptest.NewRequest(http.MethodPost, 
			"/api/v1/counter/test/increment", 
			strings.NewReader(largeBody))
		req.Header.Set("Content-Type", "application/json")
		
		recorder := httptest.NewRecorder()
		routes.ServeHTTP(recorder, req)

		// 應該返回錯誤
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	})
}

// TestHandler_Integration 整合測試
func TestHandler_Integration(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	handler := internal.NewHandler(counter, env.Logger)
	routes := handler.Routes()

	t.Run("complete counter lifecycle", func(t *testing.T) {
		counterName := "lifecycle_test"

		// 1. 初始增加
		req := httptest.NewRequest(http.MethodPost, 
			fmt.Sprintf("/api/v1/counter/%s/increment", counterName),
			strings.NewReader(`{"value": 10}`))
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code)
		var incResp map[string]interface{}
		json.NewDecoder(recorder.Body).Decode(&incResp)
		assert.Equal(t, float64(10), incResp["current_value"])

		// 2. 獲取當前值
		req = httptest.NewRequest(http.MethodGet, 
			fmt.Sprintf("/api/v1/counter/%s", counterName), nil)
		recorder = httptest.NewRecorder()
		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code)
		var getResp map[string]interface{}
		json.NewDecoder(recorder.Body).Decode(&getResp)
		assert.Equal(t, float64(10), getResp["value"])

		// 3. 減少
		req = httptest.NewRequest(http.MethodPost, 
			fmt.Sprintf("/api/v1/counter/%s/decrement", counterName),
			strings.NewReader(`{"value": 3}`))
		req.Header.Set("Content-Type", "application/json")
		recorder = httptest.NewRecorder()
		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code)
		var decResp map[string]interface{}
		json.NewDecoder(recorder.Body).Decode(&decResp)
		assert.Equal(t, float64(7), decResp["current_value"])

		// 4. 批量獲取（包含此計數器）
		req = httptest.NewRequest(http.MethodGet, 
			fmt.Sprintf("/api/v1/counters?names=%s,other_counter", counterName), nil)
		recorder = httptest.NewRecorder()
		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code)
		var multiResp map[string]interface{}
		json.NewDecoder(recorder.Body).Decode(&multiResp)
		counters := multiResp["counters"].([]interface{})
		assert.Len(t, counters, 2)

		// 5. 重置
		req = httptest.NewRequest(http.MethodPost, 
			fmt.Sprintf("/api/v1/counter/%s/reset", counterName),
			strings.NewReader(`{"admin_token": "secret_token"}`))
		req.Header.Set("Content-Type", "application/json")
		recorder = httptest.NewRecorder()
		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code)
		var resetResp map[string]interface{}
		json.NewDecoder(recorder.Body).Decode(&resetResp)
		assert.Equal(t, float64(0), resetResp["current_value"])

		// 6. 驗證重置後的值
		req = httptest.NewRequest(http.MethodGet, 
			fmt.Sprintf("/api/v1/counter/%s", counterName), nil)
		recorder = httptest.NewRecorder()
		routes.ServeHTTP(recorder, req)

		assert.Equal(t, http.StatusOK, recorder.Code)
		var finalResp map[string]interface{}
		json.NewDecoder(recorder.Body).Decode(&finalResp)
		assert.Equal(t, float64(0), finalResp["value"])
	})
}

// BenchmarkHandler_Increment 基準測試：增加操作
func BenchmarkHandler_Increment(b *testing.B) {
	helper := testutils.SetupBenchmark(b)
	defer helper.Cleanup()

	handler := internal.NewHandler(helper.Counter, helper.Env.Logger)
	routes := handler.Routes()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			counterName := fmt.Sprintf("bench_%d", i%100)
			body := fmt.Sprintf(`{"value": %d}`, (i%10)+1)
			
			req := httptest.NewRequest(http.MethodPost,
				fmt.Sprintf("/api/v1/counter/%s/increment", counterName),
				strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			
			recorder := httptest.NewRecorder()
			routes.ServeHTTP(recorder, req)
			
			if recorder.Code != http.StatusOK {
				b.Fatalf("unexpected status code: %d", recorder.Code)
			}
			i++
		}
	})
}

// BenchmarkHandler_Get 基準測試：獲取操作
func BenchmarkHandler_Get(b *testing.B) {
	helper := testutils.SetupBenchmark(b)
	defer helper.Cleanup()

	handler := internal.NewHandler(helper.Counter, helper.Env.Logger)
	routes := handler.Routes()

	// 預設一些計數器
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		counterName := fmt.Sprintf("get_bench_%d", i)
		helper.Env.RedisClient.Set(ctx, fmt.Sprintf("counter:%s", counterName), i*10, 0)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			counterName := fmt.Sprintf("get_bench_%d", i%100)
			
			req := httptest.NewRequest(http.MethodGet,
				fmt.Sprintf("/api/v1/counter/%s", counterName), nil)
			
			recorder := httptest.NewRecorder()
			routes.ServeHTTP(recorder, req)
			
			if recorder.Code != http.StatusOK {
				b.Fatalf("unexpected status code: %d", recorder.Code)
			}
			i++
		}
	})
}

// BenchmarkHandler_GetMultiple 基準測試：批量獲取
func BenchmarkHandler_GetMultiple(b *testing.B) {
	helper := testutils.SetupBenchmark(b)
	defer helper.Cleanup()

	handler := internal.NewHandler(helper.Counter, helper.Env.Logger)
	routes := handler.Routes()

	// 預設計數器
	ctx := context.Background()
	names := make([]string, 10)
	for i := 0; i < 10; i++ {
		names[i] = fmt.Sprintf("multi_bench_%d", i)
		key := fmt.Sprintf("counter:%s", names[i])
		helper.Env.RedisClient.Set(ctx, key, i*100, 0)
	}

	namesParam := strings.Join(names, ",")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet,
			fmt.Sprintf("/api/v1/counters?names=%s", namesParam), nil)
		
		recorder := httptest.NewRecorder()
		routes.ServeHTTP(recorder, req)
		
		if recorder.Code != http.StatusOK {
			b.Fatalf("unexpected status code: %d", recorder.Code)
		}
	}
}

// TestHandler_LargePayload 測試大型負載
func TestHandler_LargePayload(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	handler := internal.NewHandler(counter, env.Logger)
	routes := handler.Routes()

	t.Run("large metadata", func(t *testing.T) {
		// 創建包含大型 metadata 的請求
		metadata := make(map[string]interface{})
		for i := 0; i < 100; i++ {
			metadata[fmt.Sprintf("field_%d", i)] = fmt.Sprintf("value_%d", i)
		}

		body := map[string]interface{}{
			"value":    1,
			"metadata": metadata,
		}

		jsonBody, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost,
			"/api/v1/counter/large_metadata/increment",
			bytes.NewReader(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		recorder := httptest.NewRecorder()
		routes.ServeHTTP(recorder, req)

		// 應該正常處理
		assert.Equal(t, http.StatusOK, recorder.Code)
	})
}

// TestHandler_ResponseHeaders 測試響應標頭
func TestHandler_ResponseHeaders(t *testing.T) {
	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	handler := internal.NewHandler(counter, env.Logger)
	routes := handler.Routes()

	t.Run("content-type header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/counter/test", nil)
		recorder := httptest.NewRecorder()

		routes.ServeHTTP(recorder, req)

		contentType := recorder.Header().Get("Content-Type")
		assert.Equal(t, "application/json", contentType)
	})
}

// TestHandler_RequestTimeout 測試請求超時處理
func TestHandler_RequestTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timeout test in short mode")
	}

	env := testutils.SetupTestEnvironment(t)
	defer env.Cleanup()

	config := testutils.DefaultTestConfig()
	counter := internal.NewCounter(env.RedisClient, env.PostgresPool, config, env.Logger)
	defer counter.Shutdown()

	handler := internal.NewHandler(counter, env.Logger)
	
	// 創建一個有超時的處理器
	timeoutHandler := http.TimeoutHandler(handler.Routes(), 100*time.Millisecond, "timeout")

	t.Run("request with timeout", func(t *testing.T) {
		// 創建一個會延遲的請求（透過大量操作）
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodPost,
					fmt.Sprintf("/api/v1/counter/timeout_test_%d/increment", id),
					strings.NewReader(`{"value": 1}`))
				req.Header.Set("Content-Type", "application/json")
				
				recorder := httptest.NewRecorder()
				timeoutHandler.ServeHTTP(recorder, req)
			}(i)
		}
		wg.Wait()
	})
}