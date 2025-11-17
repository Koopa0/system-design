package main

import (
	"12-metrics-monitoring/internal"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var (
	db            *internal.TimeSeriesDB
	alertEngine   *internal.AlertEngine
	promqlExecutor *internal.PromQLExecutor
)

func main() {
	port := flag.Int("port", 8080, "Server port")
	flag.Parse()

	// 初始化配置
	config := &internal.Config{
		Port:              *port,
		RetentionRaw:      7 * 24 * time.Hour,   // 7 天原始數據
		RetentionAgg5m:    30 * 24 * time.Hour,  // 30 天聚合數據
		RetentionAgg1h:    365 * 24 * time.Hour, // 1 年歷史數據
		BlockDuration:     2 * time.Hour,        // 2 小時一個 Block
		AlertEvalInterval: 30 * time.Second,     // 30 秒評估一次告警
		MaxSeriesPerQuery: 10000,                // 單次查詢最多 10000 個序列
	}

	// 初始化數據庫
	db = internal.NewTimeSeriesDB(config)
	log.Println("Time-series database initialized")

	// 初始化告警引擎
	alertEngine = internal.NewAlertEngine(config, db)
	alertEngine.SetCallback(func(alert *internal.Alert) {
		log.Printf("ALERT: %s - %s: %.2f (threshold: %.2f)\n",
			alert.Severity, alert.RuleName, alert.CurrentValue, alert.Threshold)
	})
	go alertEngine.Start()
	log.Println("Alert engine started")

	// 初始化 PromQL 執行器
	promqlExecutor = internal.NewPromQLExecutor(db)
	log.Println("PromQL executor initialized")

	// 註冊路由
	http.HandleFunc("/api/v1/write", handleWrite)
	http.HandleFunc("/api/v1/write/batch", handleWriteBatch)
	http.HandleFunc("/api/v1/query_range", handleQueryRange)
	http.HandleFunc("/api/v1/query", handleQuery)
	http.HandleFunc("/api/v1/aggregate", handleAggregate)
	http.HandleFunc("/api/v1/alerts/rules", handleAlertRules)
	http.HandleFunc("/api/v1/alerts/active", handleActiveAlerts)
	http.HandleFunc("/api/v1/stats", handleStats)
	http.HandleFunc("/health", handleHealth)

	// 啟動服務器
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Starting metrics monitoring server on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

// handleWrite 處理單個指標寫入
func handleWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var metric internal.Metric
	if err := json.NewDecoder(r.Body).Decode(&metric); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := db.Write(&metric); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Metric written successfully",
	})
}

// handleWriteBatch 處理批量寫入
func handleWriteBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var metrics []*internal.Metric
	if err := json.NewDecoder(r.Body).Decode(&metrics); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := db.WriteBatch(metrics); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"count":   len(metrics),
		"message": fmt.Sprintf("%d metrics written successfully", len(metrics)),
	})
}

// handleQueryRange 處理時間範圍查詢
func handleQueryRange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "name parameter is required", http.StatusBadRequest)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid start parameter", http.StatusBadRequest)
		return
	}

	end, err := strconv.ParseInt(endStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid end parameter", http.StatusBadRequest)
		return
	}

	// 解析標籤過濾
	var labels map[string]string
	labelsStr := r.URL.Query().Get("labels")
	if labelsStr != "" {
		labels = make(map[string]string)
		pairs := strings.Split(labelsStr, ",")
		for _, pair := range pairs {
			kv := strings.Split(pair, "=")
			if len(kv) == 2 {
				labels[kv[0]] = kv[1]
			}
		}
	}

	metrics := db.QueryRange(name, start, end, labels)

	// 組織返回數據
	result := map[string]interface{}{
		"name":       name,
		"labels":     labels,
		"datapoints": make([]map[string]interface{}, 0),
	}

	for _, m := range metrics {
		result["datapoints"] = append(result["datapoints"].([]map[string]interface{}), map[string]interface{}{
			"timestamp": m.Timestamp,
			"value":     m.Value,
		})
	}

	json.NewEncoder(w).Encode(result)
}

// handleQuery 處理 PromQL 查詢
func handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("query")
	if query == "" {
		http.Error(w, "query parameter is required", http.StatusBadRequest)
		return
	}

	result, err := promqlExecutor.Execute(query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"query":     query,
		"result":    result,
		"timestamp": time.Now().UnixMilli(),
	})
}

// handleAggregate 處理聚合查詢
func handleAggregate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.URL.Query().Get("name")
	aggregation := r.URL.Query().Get("aggregation")
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	if name == "" || aggregation == "" {
		http.Error(w, "name and aggregation parameters are required", http.StatusBadRequest)
		return
	}

	start, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid start parameter", http.StatusBadRequest)
		return
	}

	end, err := strconv.ParseInt(endStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid end parameter", http.StatusBadRequest)
		return
	}

	value, err := db.Aggregate(name, aggregation, start, end)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"name":        name,
		"aggregation": aggregation,
		"value":       value,
		"start":       start,
		"end":         end,
	})
}

// handleAlertRules 處理告警規則
func handleAlertRules(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// 獲取所有規則
		rules := alertEngine.GetRules()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"rules": rules,
		})

	case http.MethodPost:
		// 創建新規則
		var rule internal.AlertRule
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// 解析 duration 字符串
		if durationStr, ok := r.URL.Query()["duration"]; ok && len(durationStr) > 0 {
			duration, err := time.ParseDuration(durationStr[0])
			if err == nil {
				rule.Duration = duration
			}
		}

		rule.Enabled = true

		if err := alertEngine.AddRule(&rule); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"rule_id": rule.ID,
			"message": "Alert rule created successfully",
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleActiveAlerts 處理活動告警
func handleActiveAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	alerts := alertEngine.GetActiveAlerts()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"alerts": alerts,
	})
}

// handleStats 處理統計數據
func handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := db.GetStats()

	json.NewEncoder(w).Encode(stats)
}

// handleHealth 健康檢查
func handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"time":   time.Now().Format(time.RFC3339),
	})
}
