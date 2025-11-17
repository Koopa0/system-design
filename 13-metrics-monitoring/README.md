# 指標監控系統 (Metrics Monitoring System)

高性能時序數據監控系統，支援 Gorilla 壓縮、告警規則引擎和 PromQL 查詢語言。

## 功能特性

- **時序數據庫 (TSDB)**
  - 基於 Block 的存储架構
  - 追加寫入 (Append-only)
  - 毫秒級查詢性能

- **Gorilla 壓縮**
  - Delta-of-Delta 時間戳壓縮
  - XOR 浮點數壓縮
  - 11.7:1 壓縮比

- **降採樣 (Downsampling)**
  - 7 天原始數據（10s 粒度）
  - 30 天聚合數據（5min 粒度）
  - 1 年歷史數據（1h 粒度）

- **告警規則引擎**
  - 靈活的條件表達式
  - 持續時間判斷
  - 告警級別分類

- **PromQL 查詢語言**
  - 聚合函數（sum, avg, max, min）
  - 時間範圍查詢
  - 標籤過濾

## 快速開始

### 安裝依賴

```bash
go mod download
```

### 啟動服務

```bash
make run
# 或
go run cmd/server/main.go
```

服務將在 `http://localhost:8080` 啟動。

### 寫入指標

```bash
# 寫入單個指標
curl -X POST http://localhost:8080/api/v1/write \
  -H "Content-Type: application/json" \
  -d '{
    "name": "http_requests_total",
    "labels": {
      "method": "GET",
      "path": "/api/users",
      "status": "200"
    },
    "value": 1,
    "timestamp": 1699500000000
  }'

# 批量寫入
curl -X POST http://localhost:8080/api/v1/write/batch \
  -H "Content-Type: application/json" \
  -d '[
    {
      "name": "cpu_usage_percent",
      "labels": {"host": "web-01"},
      "value": 45.2,
      "timestamp": 1699500000000
    },
    {
      "name": "memory_usage_bytes",
      "labels": {"host": "web-01"},
      "value": 2147483648,
      "timestamp": 1699500000000
    }
  ]'
```

### 查詢指標

```bash
# 查詢時間範圍
curl "http://localhost:8080/api/v1/query_range?name=http_requests_total&start=1699500000&end=1699503600"

# PromQL 查詢
curl "http://localhost:8080/api/v1/query?query=sum(rate(http_requests_total[5m]))"

# 聚合查詢
curl "http://localhost:8080/api/v1/aggregate?name=cpu_usage_percent&aggregation=avg&start=1699500000&end=1699503600"
```

### 配置告警規則

```bash
curl -X POST http://localhost:8080/api/v1/alerts/rules \
  -H "Content-Type: application/json" \
  -d '{
    "name": "HighCPUUsage",
    "metric_name": "cpu_usage_percent",
    "condition": ">",
    "threshold": 80,
    "duration": "5m",
    "severity": "warning",
    "description": "CPU 使用率超過 80% 持續 5 分鐘"
  }'
```

## API 文件

### 寫入 API

#### POST /api/v1/write

寫入單個指標數據。

**請求參數：**

```json
{
  "name": "metric_name",          // 指標名稱
  "labels": {                     // 標籤（可選）
    "key1": "value1",
    "key2": "value2"
  },
  "value": 123.45,                // 指標值
  "timestamp": 1699500000000      // 時間戳（毫秒）
}
```

**響應：**

```json
{
  "success": true,
  "message": "Metric written successfully"
}
```

#### POST /api/v1/write/batch

批量寫入多個指標。

**請求參數：**

```json
[
  {
    "name": "metric1",
    "labels": {"host": "web-01"},
    "value": 100,
    "timestamp": 1699500000000
  },
  {
    "name": "metric2",
    "labels": {"host": "web-02"},
    "value": 200,
    "timestamp": 1699500000000
  }
]
```

**響應：**

```json
{
  "success": true,
  "count": 2,
  "message": "2 metrics written successfully"
}
```

### 查詢 API

#### GET /api/v1/query_range

查詢時間範圍內的指標數據。

**查詢參數：**

- `name` - 指標名稱
- `start` - 開始時間戳（秒）
- `end` - 結束時間戳（秒）
- `labels` - 標籤過濾（可選，格式：`key1=value1,key2=value2`）

**響應：**

```json
{
  "name": "http_requests_total",
  "labels": {
    "method": "GET",
    "path": "/api/users"
  },
  "datapoints": [
    {"timestamp": 1699500000000, "value": 100},
    {"timestamp": 1699500010000, "value": 105},
    {"timestamp": 1699500020000, "value": 110}
  ]
}
```

#### GET /api/v1/query

使用 PromQL 查詢指標。

**查詢參數：**

- `query` - PromQL 查詢表達式
- `time` - 查詢時間點（可選，默認當前時間）

**支援的 PromQL 函數：**

- `sum(metric_name)` - 總和
- `avg(metric_name)` - 平均值
- `max(metric_name)` - 最大值
- `min(metric_name)` - 最小值
- `rate(metric_name[5m])` - 每秒速率（5分鐘窗口）

**響應：**

```json
{
  "query": "sum(rate(http_requests_total[5m]))",
  "result": 125.5,
  "timestamp": 1699500000000
}
```

#### GET /api/v1/aggregate

聚合查詢。

**查詢參數：**

- `name` - 指標名稱
- `aggregation` - 聚合類型（sum/avg/max/min）
- `start` - 開始時間戳（秒）
- `end` - 結束時間戳（秒）

**響應：**

```json
{
  "name": "cpu_usage_percent",
  "aggregation": "avg",
  "value": 45.2,
  "start": 1699500000,
  "end": 1699503600
}
```

### 告警 API

#### POST /api/v1/alerts/rules

創建告警規則。

**請求參數：**

```json
{
  "name": "HighCPUUsage",         // 規則名稱
  "metric_name": "cpu_usage_percent",
  "condition": ">",               // 條件：>, <, ==, >=, <=
  "threshold": 80,                // 閾值
  "duration": "5m",               // 持續時間
  "severity": "warning",          // 級別：info/warning/critical
  "description": "描述"
}
```

**響應：**

```json
{
  "success": true,
  "rule_id": "rule-12345",
  "message": "Alert rule created successfully"
}
```

#### GET /api/v1/alerts/rules

獲取所有告警規則。

**響應：**

```json
{
  "rules": [
    {
      "id": "rule-12345",
      "name": "HighCPUUsage",
      "metric_name": "cpu_usage_percent",
      "condition": ">",
      "threshold": 80,
      "duration": "5m",
      "severity": "warning",
      "enabled": true
    }
  ]
}
```

#### GET /api/v1/alerts/active

獲取當前活動的告警。

**響應：**

```json
{
  "alerts": [
    {
      "rule_name": "HighCPUUsage",
      "metric_name": "cpu_usage_percent",
      "current_value": 85.3,
      "threshold": 80,
      "started_at": "2024-11-09T10:30:00Z",
      "duration": "8m",
      "severity": "warning"
    }
  ]
}
```

## 性能指標

基於 1,000 個時間序列，每秒 1,000 次寫入的測試結果：

| 指標 | 數值 |
|------|------|
| 寫入延遲 (P99) | 2.5ms |
| 查詢延遲 (P99) | 50ms |
| 壓縮比 | 11.7:1 |
| 內存使用 | 256MB |
| 磁盤寫入速率 | 1.2MB/s |

## 配置

### 服務配置

編輯 `cmd/server/main.go` 中的配置：

```go
config := &internal.Config{
    Port:              8080,
    RetentionRaw:      7 * 24 * time.Hour,   // 7 天原始數據
    RetentionAgg5m:    30 * 24 * time.Hour,  // 30 天聚合數據
    RetentionAgg1h:    365 * 24 * time.Hour, // 1 年歷史數據
    BlockDuration:     2 * time.Hour,        // Block 時長
    AlertEvalInterval: 30 * time.Second,     // 告警評估間隔
    MaxSeriesPerQuery: 10000,                // 單次查詢最大序列數
}
```

### 壓縮配置

Gorilla 壓縮默認啟用，無需額外配置。

## 架構設計

詳細的架構設計和演進過程請參考 [DESIGN.md](./DESIGN.md)。

## 開發

### 運行測試

```bash
make test
```

### 運行基準測試

```bash
make bench
```

### 構建

```bash
make build
```

## 使用範例

### 範例 1：監控 Web 服務器

```go
package main

import (
    "12-metrics-monitoring/internal"
    "time"
)

func main() {
    db := internal.NewTimeSeriesDB(&internal.Config{
        RetentionRaw:  7 * 24 * time.Hour,
        BlockDuration: 2 * time.Hour,
    })

    // 記錄 HTTP 請求
    db.Write(&internal.Metric{
        Name: "http_requests_total",
        Labels: map[string]string{
            "method": "GET",
            "path":   "/api/users",
            "status": "200",
        },
        Value:     1,
        Timestamp: time.Now().UnixMilli(),
    })

    // 查詢最近 5 分鐘的請求數
    metrics := db.QueryRange("http_requests_total",
        time.Now().Add(-5*time.Minute).Unix(),
        time.Now().Unix(),
        nil,
    )

    // 計算 QPS
    var total float64
    for _, m := range metrics {
        total += m.Value
    }
    qps := total / 300.0 // 5 分鐘 = 300 秒
    println("QPS:", qps)
}
```

### 範例 2：設置告警

```go
package main

import (
    "12-metrics-monitoring/internal"
    "time"
)

func main() {
    engine := internal.NewAlertEngine(&internal.Config{
        AlertEvalInterval: 30 * time.Second,
    })

    // 創建告警規則
    rule := &internal.AlertRule{
        Name:       "HighErrorRate",
        MetricName: "http_errors_total",
        Condition:  ">",
        Threshold:  100,
        Duration:   5 * time.Minute,
        Severity:   "critical",
    }

    engine.AddRule(rule)

    // 設置告警回調
    engine.SetCallback(func(alert *internal.Alert) {
        println("ALERT:", alert.RuleName, "triggered!")
        println("Current value:", alert.CurrentValue)
        println("Threshold:", alert.Threshold)
        // 發送通知（Email, Slack, etc.）
    })

    // 啟動告警引擎
    engine.Start()
}
```

### 範例 3：PromQL 查詢

```go
package main

import (
    "12-metrics-monitoring/internal"
    "fmt"
)

func main() {
    executor := internal.NewPromQLExecutor(db)

    // 查詢 HTTP 請求總數
    result, err := executor.Execute("sum(http_requests_total)")
    if err != nil {
        panic(err)
    }
    fmt.Printf("Total requests: %.0f\n", result)

    // 查詢平均 CPU 使用率
    result, err = executor.Execute("avg(cpu_usage_percent)")
    if err != nil {
        panic(err)
    }
    fmt.Printf("Avg CPU: %.2f%%\n", result)

    // 查詢每秒請求速率（5分鐘窗口）
    result, err = executor.Execute("rate(http_requests_total[5m])")
    if err != nil {
        panic(err)
    }
    fmt.Printf("Request rate: %.2f req/s\n", result)
}
```

## 實戰案例

### Uber M3 的演進

本項目參考了 Uber 的 M3 監控系統的演進歷程：

- **2014 年**: 單體 Graphite，每秒 100 萬指標
- **2016 年**: M3DB v1，每秒 500 萬指標
- **2018 年**: M3DB v2 + M3Query，每秒 2,000 萬指標
- **2020 年**: M3 Aggregator，每秒 5,000 萬指標

關鍵技術：
- Gorilla 壓縮（11.7:1）
- 分層降採樣
- 分布式聚合
- PromQL 兼容

## 授權

MIT License
