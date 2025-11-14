// Package main 提供 HTTP API 服務
//
// 教學重點：
//   - RESTful API 設計
//   - 錯誤處理
//   - JSON 序列化
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/koopa0/06-unique-id-generator/internal/generator"
)

var (
	snowflake *generator.Snowflake
)

func main() {
	// 從環境變數讀取機器 ID
	machineID := int64(0)
	if env := os.Getenv("MACHINE_ID"); env != "" {
		if id, err := strconv.ParseInt(env, 10, 64); err == nil {
			machineID = id
		}
	}

	// 初始化 Snowflake 生成器
	var err error
	snowflake, err = generator.NewSnowflake(machineID)
	if err != nil {
		log.Fatalf("Failed to create Snowflake generator: %v", err)
	}

	log.Printf("Unique ID Generator started with machine ID: %d", machineID)

	// 註冊路由
	http.HandleFunc("/api/v1/snowflake", handleSnowflake)
	http.HandleFunc("/api/v1/snowflake/batch", handleSnowflakeBatch)
	http.HandleFunc("/api/v1/snowflake/parse/", handleSnowflakeParse)
	http.HandleFunc("/api/v1/capacity", handleCapacity)
	http.HandleFunc("/health", handleHealth)

	// 啟動服務
	port := "8080"
	if env := os.Getenv("PORT"); env != "" {
		port = env
	}

	log.Printf("Server listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// SnowflakeResponse 單個 ID 回應
type SnowflakeResponse struct {
	ID        int64  `json:"id"`
	IDString  string `json:"id_string"`
	MachineID int64  `json:"machine_id"`
}

// handleSnowflake 生成單個 Snowflake ID
//
// GET /api/v1/snowflake
//
// 回應範例：
//
//	{
//	  "id": 123456789,
//	  "id_string": "123456789",
//	  "machine_id": 1
//	}
func handleSnowflake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id, err := snowflake.Generate()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate ID: %v", err), http.StatusInternalServerError)
		return
	}

	info := generator.ParseSnowflakeID(id)

	resp := SnowflakeResponse{
		ID:        id,
		IDString:  strconv.FormatInt(id, 10),
		MachineID: info.MachineID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// BatchResponse 批量 ID 回應
type BatchResponse struct {
	Count int64   `json:"count"`
	IDs   []int64 `json:"ids"`
}

// handleSnowflakeBatch 批量生成 Snowflake ID
//
// GET /api/v1/snowflake/batch?count=100
//
// 教學重點：
//   - 批量操作減少網路往返
//   - 限制批量大小避免濫用
func handleSnowflakeBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 解析數量參數
	countStr := r.URL.Query().Get("count")
	if countStr == "" {
		http.Error(w, "Missing count parameter", http.StatusBadRequest)
		return
	}

	count, err := strconv.ParseInt(countStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid count parameter", http.StatusBadRequest)
		return
	}

	// 限制批量大小（避免濫用）
	const maxBatchSize = 1000
	if count <= 0 || count > maxBatchSize {
		http.Error(w, fmt.Sprintf("Count must be between 1 and %d", maxBatchSize), http.StatusBadRequest)
		return
	}

	// 生成 ID
	ids := make([]int64, count)
	for i := int64(0); i < count; i++ {
		id, err := snowflake.Generate()
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to generate ID: %v", err), http.StatusInternalServerError)
			return
		}
		ids[i] = id
	}

	resp := BatchResponse{
		Count: count,
		IDs:   ids,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleSnowflakeParse 解析 Snowflake ID
//
// GET /api/v1/snowflake/parse/:id
//
// 教學重點：
//   - ID 解析用於調試
//   - 提取時間、機器、序列號信息
func handleSnowflakeParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 提取 ID 參數（從路徑 /api/v1/snowflake/parse/:id）
	idStr := r.URL.Path[len("/api/v1/snowflake/parse/"):]
	if idStr == "" {
		http.Error(w, "Missing ID parameter", http.StatusBadRequest)
		return
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid ID parameter", http.StatusBadRequest)
		return
	}

	// 解析 ID
	info := generator.ParseSnowflakeID(id)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// handleCapacity 返回容量信息
//
// GET /api/v1/capacity
//
// 教學重點：
//   - 展示系統容量設計
func handleCapacity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cap := generator.GetCapacity()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cap)
}

// HealthResponse 健康檢查回應
type HealthResponse struct {
	Status         string `json:"status"`
	MachineID      int64  `json:"machine_id"`
	ClockBackCount int64  `json:"clock_back_count"`
}

// handleHealth 健康檢查
//
// GET /health
func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 測試生成 ID
	id, err := snowflake.Generate()
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}

	info := generator.ParseSnowflakeID(id)

	resp := HealthResponse{
		Status:         "healthy",
		MachineID:      info.MachineID,
		ClockBackCount: snowflake.GetClockBackCounter(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
