# Metrics Monitoring 系統設計文檔

## 雙11凌晨的噩夢

2024 年 11 月 11 日凌晨 00:00，電商平台「快樂購」的雙11活動正式開始。

SRE 工程師 Sarah 坐在監控室，盯著儀表板。

**00:01** - 訂單量：127 筆/秒（正常）

**00:05** - 訂單量：1,894 筆/秒（流量激增！）

**00:08** - Sarah 的手機收到用戶投訴：「網站打不開！」

她立刻打開服務器查看：

```bash
$ ssh web-server-01
$ top

  PID USER      PR  NI    VIRT    RES    SHR S  %CPU %MEM
 1234 www       20   0   8.2g   6.8g   1.2g R  98.7 85.3

問題：服務器 CPU 98%，內存 85%！
```

Sarah 緊急重啟了服務器，網站恢復正常。

但 10 分鐘後，又收到投訴...

**她意識到一個嚴重問題：我們沒有監控系統，只能靠用戶投訴才知道出問題！**

---

**當晚的損失數據：**
```
00:08 - 00:18（10 分鐘宕機）：
- 損失訂單：估計 11,364 筆
- 損失金額：約 NT$ 3,400 萬
- 用戶流失：約 8,000 人

00:25 - 00:35（再次宕機）：
- 損失訂單：估計 9,827 筆
- 損失金額：約 NT$ 2,900 萬

總損失：NT$ 6,300 萬
```

第二天早上的覆盤會議上，CTO 大發雷霆：

「為什麼沒有監控？為什麼要等用戶投訴才發現問題？」

Sarah 低著頭：「我們... 沒有監控系統。」

「立刻建立！下次雙11之前必須上線！」

## 第一次嘗試：手動檢查（2024/11/12）

### 最簡單的方案

Sarah 的第一個想法：寫個腳本定期檢查。

```bash
#!/bin/bash
# check_server.sh

while true; do
    # 檢查 CPU
    cpu=$(top -bn1 | grep "Cpu(s)" | awk '{print $2}' | cut -d'%' -f1)

    # 檢查內存
    mem=$(free | grep Mem | awk '{print ($3/$2) * 100.0}')

    # 檢查磁盤
    disk=$(df -h / | tail -1 | awk '{print $5}' | cut -d'%' -f1)

    echo "[$(date)] CPU: ${cpu}%, MEM: ${mem}%, DISK: ${disk}%"

    # 如果 CPU > 80%，發郵件告警
    if (( $(echo "$cpu > 80" | bc -l) )); then
        echo "CPU 過高！" | mail -s "告警" sarah@example.com
    fi

    sleep 60  # 每分鐘檢查一次
done
```

「用 cron 每分鐘執行一次，應該夠了吧？」Sarah 想。

### 問題很快出現

**問題 1：歷史數據缺失**
```
2024-11-15 03:00 - CPU: 78%
2024-11-15 03:01 - CPU: 82%（告警！）
2024-11-15 03:02 - CPU: 45%

問題：
- 只有當前時刻的數據
- 無法查看趨勢（過去一小時、過去一天）
- 無法分析根因（CPU 突然升高是什麼導致的？）
```

**問題 2：多台服務器**
```
有 20 台 Web 服務器，需要：
- 登入每台服務器執行腳本
- 收集 20 台的數據
- 手動彙總分析 ❌

太麻煩了！
```

**問題 3：指標單一**
```
只監控 CPU、內存、磁盤，但還需要：
- HTTP 請求數（QPS）
- 響應時間（延遲）
- 錯誤率
- 數據庫連接數
- 快取命中率
- ...

每加一個指標，腳本變得更複雜
```

Sarah 嘆氣：「這樣下去不行，需要一個專門的監控系統。」

## 第二次嘗試：寫入數據庫（2024/11/16）

### 思路

「把指標數據存入數據庫，然後用 SQL 查詢！」

```sql
CREATE TABLE metrics (
    id SERIAL PRIMARY KEY,
    metric_name VARCHAR(100),
    metric_value FLOAT,
    host VARCHAR(50),
    timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 每秒插入一次
INSERT INTO metrics (metric_name, metric_value, host)
VALUES ('cpu_usage', 78.5, 'web-server-01');

INSERT INTO metrics (metric_name, metric_value, host)
VALUES ('memory_usage', 65.2, 'web-server-01');
```

### 問題：數據量爆炸

**計算：**
```
場景：
- 20 台服務器
- 每台 10 個指標（CPU、內存、磁盤、QPS、延遲...）
- 每秒採集一次

數據量：
- 每秒：20 × 10 = 200 條記錄
- 每分鐘：200 × 60 = 12,000 條
- 每小時：12,000 × 60 = 720,000 條
- 每天：720,000 × 24 = 17,280,000 條（1,728 萬）
- 每月：1,728 萬 × 30 = 5.184 億條 ❌

存儲：
- 每條記錄約 100 bytes
- 每月：5.184 億 × 100 bytes = 51.84 GB

一年：51.84 × 12 = 622 GB！
```

**性能問題：**
```sql
-- 查詢過去 1 小時的平均 CPU
SELECT AVG(metric_value) as avg_cpu
FROM metrics
WHERE metric_name = 'cpu_usage'
  AND host = 'web-server-01'
  AND timestamp >= NOW() - INTERVAL '1 hour';

執行時間：8.7 秒 ❌（掃描 72 萬條記錄）

-- 查詢過去 24 小時，每分鐘的平均 CPU（1440 個點）
SELECT
    DATE_TRUNC('minute', timestamp) as time,
    AVG(metric_value) as avg_cpu
FROM metrics
WHERE metric_name = 'cpu_usage'
  AND host = 'web-server-01'
  AND timestamp >= NOW() - INTERVAL '24 hours'
GROUP BY DATE_TRUNC('minute', timestamp)
ORDER BY time;

執行時間：35.2 秒 ❌（掃描 1,728 萬條記錄）
```

Sarah 崩潰了：「查詢太慢了！而且數據量還在不斷增長...」

### 為什麼傳統數據庫不適合？

資深 DBA Mike 解釋：

「時序數據（Time-Series Data）有特殊性質：」

```
1. 寫入頻繁（每秒數百次）
   - PostgreSQL 針對 OLTP 優化（少量寫入 + 複雜查詢）
   - 不適合高頻時序寫入

2. 只追加（Append-Only）
   - 指標數據只會新增，不會修改
   - PostgreSQL 的 MVCC（多版本並發控制）是浪費

3. 時間範圍查詢
   - 總是查詢某個時間範圍（如過去 1 小時）
   - PostgreSQL 的 B-Tree 索引不夠高效

4. 聚合計算
   - 需要大量 AVG、SUM、MAX、MIN、P99
   - PostgreSQL 的聚合需要掃描大量數據

5. 舊數據可刪除
   - 通常只保留最近 30 天的詳細數據
   - 更早的數據可以刪除或降採樣
   - PostgreSQL 的 DELETE 性能差
```

「你需要的是**時序數據庫**（Time-Series Database, TSDB）。」Mike 說。

## 靈感：Prometheus 的設計

Sarah 研究了業界最流行的監控系統 Prometheus，發現幾個關鍵設計：

### 1. 指標格式

```
# Prometheus 指標格式
<metric_name>{<label>=<value>, ...} <value> <timestamp>

範例：
http_requests_total{method="GET", status="200", path="/api/users"} 12456 1699776000
http_requests_total{method="POST", status="201", path="/api/orders"} 3421 1699776000
cpu_usage{host="web-server-01", core="0"} 78.5 1699776000
memory_usage{host="web-server-01", type="used"} 8589934592 1699776000
```

**關鍵特性：**
```
1. 指標名稱（metric_name）：描述測量什麼
   - http_requests_total：HTTP 請求總數
   - cpu_usage：CPU 使用率

2. 標籤（labels）：多維度篩選
   - method="GET"：按 HTTP 方法篩選
   - host="web-server-01"：按主機篩選
   - 可以任意組合查詢

3. 值（value）：測量值

4. 時間戳（timestamp）：Unix 時間戳（秒）
```

### 2. Pull 模型（拉取模型）

```
傳統（Push 模型）：
Agent → 推送 → 監控系統

問題：
- Agent 需要知道監控系統的地址
- 監控系統故障時，數據丟失
- 難以動態擴展

Prometheus（Pull 模型）：
Prometheus → 定期拉取 → Target（應用程序）

優勢：
- Target 不需要知道 Prometheus 地址
- Prometheus 可以主動發現 Target
- 易於檢測 Target 是否存活（拉取失敗 = 宕機）
```

### 3. 本地存儲 + 壓縮

```
Prometheus 的存儲優化：
1. 時間分塊（Time Blocks）
   - 每 2 小時的數據一個 block
   - Block 結構：
     ├── chunks/（壓縮的時序數據）
     ├── index（倒排索引）
     └── meta.json（元數據）

2. 高效壓縮（Gorilla 壓縮算法）
   - Facebook 開發
   - 針對時序數據優化
   - 壓縮率：10:1 到 20:1

3. 下採樣（Downsampling）
   - 保留原始數據（1 秒精度）：最近 7 天
   - 5 分鐘聚合數據：最近 30 天
   - 1 小時聚合數據：最近 1 年
```

Sarah 興奮地說：「這就是我需要的！」

## 改進方案：時序數據庫（2024/11/20）

### 核心數據結構

```go
// Metric 指標
type Metric struct {
    Name      string            // 指標名稱
    Labels    map[string]string // 標籤
    Timestamp int64             // Unix 時間戳（毫秒）
    Value     float64           // 值
}

// 範例
metric := Metric{
    Name: "http_requests_total",
    Labels: map[string]string{
        "method": "GET",
        "status": "200",
        "path":   "/api/users",
    },
    Timestamp: time.Now().UnixMilli(),
    Value:     12456,
}
```

### 存儲結構：按時間分片

```go
// TimeSeries 時間序列
type TimeSeries struct {
    MetricName string
    Labels     map[string]string
    Points     []DataPoint
}

type DataPoint struct {
    Timestamp int64   // 時間戳（毫秒）
    Value     float64 // 值
}

// 按時間分片存儲（每小時一個文件）
type TimeSeriesDB struct {
    blocks map[int64]*Block  // key: 小時的時間戳
}

type Block struct {
    StartTime int64
    EndTime   int64
    Series    map[string]*TimeSeries  // key: metric_name + labels 的組合
}
```

### 寫入流程

```go
func (db *TimeSeriesDB) Write(metric Metric) error {
    // 1. 計算屬於哪個 block（按小時）
    blockTime := metric.Timestamp / (3600 * 1000) * (3600 * 1000)

    // 2. 獲取或創建 block
    block := db.getOrCreateBlock(blockTime)

    // 3. 生成 series key（metric_name + labels）
    seriesKey := generateSeriesKey(metric.Name, metric.Labels)

    // 4. 獲取或創建 time series
    series := block.getSeries(seriesKey)
    if series == nil {
        series = &TimeSeries{
            MetricName: metric.Name,
            Labels:     metric.Labels,
            Points:     []DataPoint{},
        }
        block.putSeries(seriesKey, series)
    }

    // 5. 追加數據點
    series.Points = append(series.Points, DataPoint{
        Timestamp: metric.Timestamp,
        Value:     metric.Value,
    })

    return nil
}

// 生成 series key
func generateSeriesKey(name string, labels map[string]string) string {
    // 範例：http_requests_total{method="GET",status="200"}
    keys := make([]string, 0, len(labels))
    for k := range labels {
        keys = append(keys, k)
    }
    sort.Strings(keys)  // 排序保證一致性

    var buf bytes.Buffer
    buf.WriteString(name)
    buf.WriteString("{")
    for i, k := range keys {
        if i > 0 {
            buf.WriteString(",")
        }
        buf.WriteString(k)
        buf.WriteString("=\"")
        buf.WriteString(labels[k])
        buf.WriteString("\"")
    }
    buf.WriteString("}")

    return buf.String()
}
```

### 查詢流程

```go
// Query 查詢指定時間範圍的數據
func (db *TimeSeriesDB) Query(
    metricName string,
    labels map[string]string,
    startTime, endTime int64,
) ([]DataPoint, error) {

    results := []DataPoint{}

    // 1. 找到所有相關的 blocks
    blocks := db.getBlocksInRange(startTime, endTime)

    // 2. 遍歷 blocks
    for _, block := range blocks {
        seriesKey := generateSeriesKey(metricName, labels)
        series := block.getSeries(seriesKey)
        if series == nil {
            continue
        }

        // 3. 篩選時間範圍內的數據點
        for _, point := range series.Points {
            if point.Timestamp >= startTime && point.Timestamp <= endTime {
                results = append(results, point)
            }
        }
    }

    // 4. 排序（按時間）
    sort.Slice(results, func(i, j int) bool {
        return results[i].Timestamp < results[j].Timestamp
    })

    return results, nil
}
```

### 聚合查詢

```go
// Aggregate 聚合查詢（如 AVG、MAX、MIN）
func (db *TimeSeriesDB) Aggregate(
    metricName string,
    labels map[string]string,
    startTime, endTime int64,
    aggFunc string,  // "avg", "max", "min", "sum", "p99"
) (float64, error) {

    // 1. 查詢原始數據
    points, err := db.Query(metricName, labels, startTime, endTime)
    if err != nil {
        return 0, err
    }

    if len(points) == 0 {
        return 0, nil
    }

    // 2. 根據聚合函數計算
    switch aggFunc {
    case "avg":
        sum := 0.0
        for _, p := range points {
            sum += p.Value
        }
        return sum / float64(len(points)), nil

    case "max":
        max := points[0].Value
        for _, p := range points {
            if p.Value > max {
                max = p.Value
            }
        }
        return max, nil

    case "min":
        min := points[0].Value
        for _, p := range points {
            if p.Value < min {
                min = p.Value
            }
        }
        return min, nil

    case "sum":
        sum := 0.0
        for _, p := range points {
            sum += p.Value
        }
        return sum, nil

    case "p99":
        // P99 百分位數
        values := make([]float64, len(points))
        for i, p := range points {
            values[i] = p.Value
        }
        sort.Float64s(values)
        index := int(float64(len(values)) * 0.99)
        return values[index], nil

    default:
        return 0, fmt.Errorf("unknown aggregation function: %s", aggFunc)
    }
}
```

### 性能對比（2024/11/22 測試）

```
場景：查詢過去 1 小時的平均 CPU

方案 A：PostgreSQL
- 查詢時間：8.7 秒
- 掃描記錄：720,000 條
- 存儲大小：72 MB（未壓縮）

方案 B：時序數據庫
- 查詢時間：0.05 秒 ✅
- 掃描記錄：3,600 個數據點（已按 block 組織）
- 存儲大小：約 5 MB（壓縮）

提升：
- 查詢速度：174 倍
- 存儲：節省 93%
```

## 第三次災難：存儲成本爆炸（2024/11/25）

### 背景：監控範圍擴大

產品經理：「我們要監控所有服務！」

新增監控：
- Web 服務器：20 台 → 100 台
- 資料庫：5 台 → 20 台
- Redis：10 台 → 50 台
- 每台新增更多指標（50 個 → 200 個）

**數據量計算：**
```
之前：
- 20 台 × 10 指標 = 200 個時間序列
- 每秒 200 個數據點
- 每天：200 × 86,400 = 17,280,000 個數據點

現在：
- 170 台 × 200 指標 = 34,000 個時間序列
- 每秒 34,000 個數據點
- 每天：34,000 × 86,400 = 2,937,600,000 個數據點（29.4 億）

存儲（保留 30 天）：
- 每個數據點：16 bytes（8 bytes timestamp + 8 bytes value）
- 30 天：29.4 億 × 30 × 16 bytes = 1.41 TB ❌

成本（AWS EBS）：
- 1.41 TB × $0.1/GB/月 = $144/月
- 一年：$1,728
```

Sarah 擔心：「成本太高了，而且還在增長...」

### 解決方案 1：壓縮算法（Gorilla）

**Gorilla 壓縮算法**（Facebook 2015）：

```
原理：時序數據的特點
1. 時間戳規律：通常是固定間隔（如每秒一個）
2. 值變化小：相鄰數據點的值相近（如 CPU 78% → 79%）

壓縮技術：
1. Delta-of-Delta 編碼（時間戳）
   原始：1000, 1001, 1002, 1003
   Delta：   1,    1,    1,    1
   Delta-of-Delta: 0, 0, 0（全是 0！）
   → 用 1 bit 表示「與上次相同」

2. XOR 編碼（值）
   原始：78.5, 78.7, 78.3
   二進制 XOR：只記錄變化的位
   → 用可變長度編碼

壓縮率：
- 原始：16 bytes/點
- 壓縮後：約 1.37 bytes/點
- 壓縮率：11.7:1 ✅
```

**實現（簡化版）：**

```go
type GorillaSeries struct {
    baseTimestamp int64   // 基準時間戳
    baseValue     uint64  // 基準值（float64 轉為 uint64）

    timestamps []byte  // 壓縮的時間戳
    values     []byte  // 壓縮的值
}

// 追加數據點（Delta-of-Delta）
func (s *GorillaSeries) Append(timestamp int64, value float64) {
    // 時間戳：Delta-of-Delta 編碼
    if s.baseTimestamp == 0 {
        s.baseTimestamp = timestamp
    } else {
        // 計算 delta
        delta := timestamp - s.baseTimestamp
        // ... 寫入可變長度編碼
    }

    // 值：XOR 編碼
    valueUint := math.Float64bits(value)
    if s.baseValue == 0 {
        s.baseValue = valueUint
    } else {
        // XOR 與前一個值
        xor := valueUint ^ s.baseValue
        // ... 寫入可變長度編碼
    }

    s.baseValue = valueUint
}
```

**效果：**
```
壓縮前：1.41 TB
壓縮後：1.41 TB ÷ 11.7 = 120.5 GB ✅

成本：
- 120.5 GB × $0.1/GB/月 = $12/月
- 一年：$144

節省：$1,728 - $144 = $1,584/年
```

### 解決方案 2：下採樣（Downsampling）

「舊數據不需要那麼精確！」Sarah 想。

**策略：**
```
數據保留策略：
- 原始數據（1 秒精度）：保留 7 天
- 5 分鐘聚合數據：保留 30 天
- 1 小時聚合數據：保留 1 年

範例：
原始數據（1 秒）：
00:00:00 - CPU: 78%
00:00:01 - CPU: 79%
00:00:02 - CPU: 77%
...
00:04:59 - CPU: 80%

5 分鐘聚合：
00:00:00 - CPU: avg=78.5%, max=82%, min=75%, p99=81%

優勢：
- 1 個聚合數據點 = 300 個原始數據點
- 存儲減少 75 倍（保留 avg、max、min、p99）
```

**實現：**

```go
type Aggregation struct {
    Avg float64
    Max float64
    Min float64
    P99 float64
}

// Downsample 下採樣
func (db *TimeSeriesDB) Downsample(
    metricName string,
    labels map[string]string,
    startTime, endTime int64,
    interval int64,  // 聚合間隔（如 5 分鐘 = 300,000 毫秒）
) ([]Aggregation, error) {

    results := []Aggregation{}

    // 將時間範圍分割為多個 interval
    for t := startTime; t < endTime; t += interval {
        // 查詢該 interval 的原始數據
        points, _ := db.Query(metricName, labels, t, t+interval)

        if len(points) == 0 {
            continue
        }

        // 計算聚合
        agg := Aggregation{}

        // Avg
        sum := 0.0
        for _, p := range points {
            sum += p.Value
        }
        agg.Avg = sum / float64(len(points))

        // Max & Min
        agg.Max = points[0].Value
        agg.Min = points[0].Value
        for _, p := range points {
            if p.Value > agg.Max {
                agg.Max = p.Value
            }
            if p.Value < agg.Min {
                agg.Min = p.Value
            }
        }

        // P99
        values := make([]float64, len(points))
        for i, p := range points {
            values[i] = p.Value
        }
        sort.Float64s(values)
        p99Index := int(float64(len(values)) * 0.99)
        agg.P99 = values[p99Index]

        results = append(results, agg)
    }

    return results, nil
}

// 定期下採樣任務
func (db *TimeSeriesDB) StartDownsamplingTask() {
    ticker := time.NewTicker(1 * time.Hour)
    for range ticker.C {
        // 將 7 天前的原始數據下採樣為 5 分鐘聚合
        sevenDaysAgo := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()

        // ... 遍歷所有時間序列，下採樣並刪除原始數據
    }
}
```

**存儲對比：**
```
場景：保留 30 天數據

方案 A：全部原始數據（1 秒精度）
- 數據點：29.4 億 × 30 = 882 億
- 壓縮後：120.5 GB × 30 = 3.6 TB ❌

方案 B：分層存儲
- 最近 7 天（1 秒精度）：120.5 GB × 7 = 843 GB
- 7-30 天（5 分鐘聚合）：120.5 GB × 23 ÷ 300 = 9.2 GB
- 總計：852.2 GB ✅

節省：3.6 TB - 852 GB = 2.76 TB（76% 減少）
```

## 第四次挑戰：告警延遲（2024/12/01）

### 問題：被動監控

目前的系統：
```
1. 收集指標 → 存入時序數據庫
2. 用戶打開 Grafana 儀表板 → 查看指標
3. 用戶發現問題 → 手動處理

問題：
- 需要人盯著儀表板（不現實）
- 問題發生到發現：延遲數分鐘到數小時
- 無法及時響應
```

產品經理：「我們需要**告警系統**！CPU > 80% 自動發送通知！」

### 告警規則引擎

```go
// AlertRule 告警規則
type AlertRule struct {
    Name        string            // 規則名稱
    MetricName  string            // 監控的指標
    Labels      map[string]string // 標籤篩選
    Condition   string            // 條件（如 ">", "<", "=="）
    Threshold   float64           // 閾值
    Duration    time.Duration     // 持續時間（連續滿足多久才告警）
    Severity    string            // 嚴重級別（critical, warning, info）
    Message     string            // 告警消息模板
}

// 範例：CPU 使用率告警
rule := AlertRule{
    Name:       "HighCPUUsage",
    MetricName: "cpu_usage",
    Labels: map[string]string{
        "host": "web-server-*",  // 所有 web 服務器
    },
    Condition:  ">",
    Threshold:  80.0,
    Duration:   5 * time.Minute,  // 持續 5 分鐘
    Severity:   "critical",
    Message:    "主機 {{.Host}} CPU 使用率 {{.Value}}% 超過 80%",
}
```

### 告警評估器

```go
type AlertEvaluator struct {
    db    *TimeSeriesDB
    rules []*AlertRule

    // 記錄規則的觸發狀態
    ruleStates map[string]*RuleState
}

type RuleState struct {
    FirstTriggeredAt time.Time  // 首次觸發時間
    TriggeredCount   int         // 觸發次數
    Firing           bool        // 是否正在告警
}

// Evaluate 評估所有告警規則
func (ae *AlertEvaluator) Evaluate() {
    for _, rule := range ae.rules {
        ae.evaluateRule(rule)
    }
}

func (ae *AlertEvaluator) evaluateRule(rule *AlertRule) {
    // 1. 查詢最近的數據
    now := time.Now().UnixMilli()
    points, _ := ae.db.Query(
        rule.MetricName,
        rule.Labels,
        now - int64(rule.Duration.Milliseconds()),
        now,
    )

    if len(points) == 0 {
        return
    }

    // 2. 檢查所有數據點是否滿足條件
    allMatch := true
    for _, point := range points {
        if !ae.checkCondition(point.Value, rule.Condition, rule.Threshold) {
            allMatch = false
            break
        }
    }

    // 3. 更新規則狀態
    state := ae.getRuleState(rule.Name)

    if allMatch {
        // 滿足條件
        if state.FirstTriggeredAt.IsZero() {
            state.FirstTriggeredAt = time.Now()
        }
        state.TriggeredCount++

        // 檢查是否持續足夠久
        if time.Since(state.FirstTriggeredAt) >= rule.Duration {
            if !state.Firing {
                // 首次觸發告警
                ae.fireAlert(rule, points[len(points)-1].Value)
                state.Firing = true
            }
        }
    } else {
        // 不滿足條件，重置狀態
        if state.Firing {
            // 告警解除
            ae.resolveAlert(rule)
        }
        state.FirstTriggeredAt = time.Time{}
        state.TriggeredCount = 0
        state.Firing = false
    }
}

// checkCondition 檢查條件
func (ae *AlertEvaluator) checkCondition(value float64, condition string, threshold float64) bool {
    switch condition {
    case ">":
        return value > threshold
    case ">=":
        return value >= threshold
    case "<":
        return value < threshold
    case "<=":
        return value <= threshold
    case "==":
        return value == threshold
    case "!=":
        return value != threshold
    default:
        return false
    }
}

// fireAlert 觸發告警
func (ae *AlertEvaluator) fireAlert(rule *AlertRule, value float64) {
    // 渲染告警消息
    message := ae.renderMessage(rule.Message, map[string]interface{}{
        "Value": value,
        "Threshold": rule.Threshold,
    })

    // 發送通知（郵件、Slack、SMS 等）
    ae.sendNotification(rule.Severity, message)

    log.Printf("[ALERT] %s: %s", rule.Name, message)
}

// resolveAlert 解除告警
func (ae *AlertEvaluator) resolveAlert(rule *AlertRule) {
    message := fmt.Sprintf("告警 %s 已解除", rule.Name)
    ae.sendNotification("info", message)

    log.Printf("[RESOLVED] %s", rule.Name)
}

// sendNotification 發送通知
func (ae *AlertEvaluator) sendNotification(severity, message string) {
    // 根據嚴重級別選擇通知渠道
    switch severity {
    case "critical":
        // 電話 + SMS + Slack + 郵件
        sendSMS(message)
        sendSlack(message)
        sendEmail(message)
    case "warning":
        // Slack + 郵件
        sendSlack(message)
        sendEmail(message)
    case "info":
        // 郵件
        sendEmail(message)
    }
}
```

### 定期評估

```go
func (ae *AlertEvaluator) Start() {
    ticker := time.NewTicker(30 * time.Second)  // 每 30 秒評估一次
    for range ticker.C {
        ae.Evaluate()
    }
}
```

### 告警示例

**配置：**
```yaml
# alerts.yml
rules:
  - name: HighCPUUsage
    metric: cpu_usage
    condition: ">"
    threshold: 80
    duration: 5m
    severity: critical
    message: "主機 {{.Host}} CPU 使用率 {{.Value}}% 持續超過 80%"

  - name: HighMemoryUsage
    metric: memory_usage
    condition: ">"
    threshold: 85
    duration: 10m
    severity: warning
    message: "主機 {{.Host}} 內存使用率 {{.Value}}% 超過 85%"

  - name: HighErrorRate
    metric: http_errors_rate
    condition: ">"
    threshold: 5
    duration: 1m
    severity: critical
    message: "HTTP 錯誤率 {{.Value}}% 超過 5%"
```

**觸發流程：**
```
時間線：CPU 告警

14:00:00 - CPU: 82%（超過 80%，開始計時）
14:01:00 - CPU: 83%（持續 1 分鐘）
14:02:00 - CPU: 85%（持續 2 分鐘）
14:03:00 - CPU: 84%（持續 3 分鐘）
14:04:00 - CPU: 86%（持續 4 分鐘）
14:05:00 - CPU: 87%（持續 5 分鐘 → 觸發告警！📧）

14:10:00 - CPU: 75%（低於 80% → 告警解除 ✅）
```

## 第五次優化：查詢語言（PromQL）（2024/12/05）

### 問題：複雜查詢困難

產品經理的需求越來越複雜：

```
需求 1：過去 5 分鐘，所有 Web 服務器的平均 CPU
需求 2：HTTP 錯誤率（errors / total × 100%）
需求 3：P99 響應時間（按路徑分組）
需求 4：QPS 增長率（與 1 小時前對比）
```

用代碼實現太麻煩！

### 解決方案：查詢語言（參考 PromQL）

**基本查詢：**
```promql
# 查詢指標
cpu_usage{host="web-server-01"}

# 時間範圍
cpu_usage{host="web-server-01"}[5m]

# 聚合函數
avg(cpu_usage{host=~"web-server-.*"})
max(cpu_usage{host=~"web-server-.*"})
min(cpu_usage{host=~"web-server-.*"})

# 按標籤分組
avg by (host) (cpu_usage)
```

**複雜查詢：**
```promql
# HTTP 錯誤率
sum(http_requests_total{status=~"5.."}) / sum(http_requests_total) * 100

# QPS（每秒請求數）
rate(http_requests_total[1m])

# P99 響應時間（按路徑分組）
histogram_quantile(0.99, http_request_duration_bucket) by (path)

# CPU 增長率（與 1 小時前對比）
(cpu_usage - cpu_usage offset 1h) / cpu_usage offset 1h * 100
```

**實現（簡化版）：**

```go
// QueryEngine 查詢引擎
type QueryEngine struct {
    db *TimeSeriesDB
}

// Execute 執行查詢
func (qe *QueryEngine) Execute(query string) ([]DataPoint, error) {
    // 解析查詢語句
    ast, err := parseQuery(query)
    if err != nil {
        return nil, err
    }

    // 執行查詢
    return qe.executeAST(ast)
}

// 範例：avg(cpu_usage{host="web-server-01"}[5m])
func (qe *QueryEngine) executeAST(ast *AST) ([]DataPoint, error) {
    switch ast.Type {
    case "metric":
        // 查詢指標
        return qe.db.Query(ast.MetricName, ast.Labels, ast.StartTime, ast.EndTime)

    case "avg":
        // 聚合：平均值
        points, _ := qe.executeAST(ast.Children[0])
        avg, _ := calculateAvg(points)
        return []DataPoint{{Value: avg}}, nil

    case "rate":
        // 速率：(last - first) / time_range
        points, _ := qe.executeAST(ast.Children[0])
        if len(points) < 2 {
            return nil, nil
        }
        first := points[0]
        last := points[len(points)-1]
        timeRange := (last.Timestamp - first.Timestamp) / 1000.0  // 秒
        rate := (last.Value - first.Value) / timeRange
        return []DataPoint{{Value: rate}}, nil

    // ... 其他函數
    }

    return nil, fmt.Errorf("unknown AST type: %s", ast.Type)
}
```

## 新的挑戰：分布式擴展

### 當前架構瓶頸

```
單機時序數據庫容量：
- 時間序列數：約 100 萬
- 每秒寫入：約 10 萬個數據點
- 存儲：約 1 TB（壓縮後）
- 查詢 QPS：約 1,000

問題：
- 無法橫向擴展
- 單點故障
- 存儲有限
```

### 10x 擴展：分片 + 副本

```
架構變化：

當前（單機）：
Prometheus → TSDB (本地存儲)

優化後（分片）：
            ┌──────────────┐
            │  Prometheus  │
            └──────┬───────┘
                   ↓
            ┌──────────────┐
            │ Write Proxy  │（按 hash 分片）
            └──────┬───────┘
                   ↓
       ┌───────────┼───────────┐
       ↓           ↓           ↓
   ┌───────┐   ┌───────┐   ┌───────┐
   │TSDB 1 │   │TSDB 2 │   │TSDB 3 │
   │(主)   │   │(主)   │   │(主)   │
   └───┬───┘   └───┬───┘   └───┬───┘
       ↓           ↓           ↓
   ┌───────┐   ┌───────┐   ┌───────┐
   │TSDB 1'│   │TSDB 2'│   │TSDB 3'│
   │(副本) │   │(副本) │   │(副本) │
   └───────┘   └───────┘   └───────┘

分片策略：
- 按時間序列 hash 分片
- hash(metric_name + labels) % 3

查詢：
- 並行查詢所有分片
- 合併結果

容量：
- 3 個分片 × 100 萬序列 = 300 萬序列
- 3 個分片 × 10 萬 寫入/秒 = 30 萬 寫入/秒
```

### 100x 擴展：專業 TSDB（如 VictoriaMetrics、Thanos）

```
架構：

            ┌──────────────┐
            │  Prometheus  │（多個實例）
            └──────┬───────┘
                   ↓
            ┌──────────────┐
            │   Thanos    │
            │   (查詢層)   │
            └──────┬───────┘
                   ↓
       ┌───────────┼───────────────┐
       ↓           ↓               ↓
   ┌───────┐   ┌───────┐   ┌──────────┐
   │ TSDB  │   │ TSDB  │   │  S3      │
   │ (短期)│   │ (短期)│   │ (長期)   │
   └───────┘   └───────┘   └──────────┘

特性：
1. 長期存儲：將舊數據壓縮後存入 S3（便宜）
2. 全局查詢：跨多個 Prometheus 實例查詢
3. 下採樣：自動將舊數據降採樣
4. 去重：多個副本的數據自動去重

容量：
- 支持數千萬時間序列
- 每秒數百萬數據點
- PB 級存儲（S3）
```

## 真實案例：Uber 的監控系統演進

### Uber M3 的誕生

**2014 年（使用 Graphite）：**
```
問題：
- 寫入性能差（每秒 10 萬指標）
- 查詢慢（聚合需要 30 秒+）
- 存儲昂貴（未壓縮）
- 無法擴展
```

**2016 年（開發 M3）：**
```
M3 設計：
1. M3DB：分布式時序數據庫
   - 一致性哈希分片
   - 複製係數 3（高可用）
   - 自定義壓縮（20:1）

2. M3 Coordinator：查詢協調器
   - 並行查詢所有分片
   - 合併結果
   - 查詢緩存

3. M3 Aggregator：實時聚合
   - 在寫入時預聚合（如 1 分鐘平均）
   - 減少存儲和查詢壓力
```

**2020 年（M3 開源）：**
```
規模：
- 時間序列：6.5 億+
- 寫入：每秒 1,000 萬+ 數據點
- 存儲：60+ PB
- 查詢：每秒 20 萬+ 查詢

性能：
- 寫入延遲：P99 < 10ms
- 查詢延遲：P99 < 100ms
- 壓縮率：20:1
```

參考資料：
- [Uber M3: 開源分布式時序數據庫](https://eng.uber.com/m3/)
- [M3 GitHub](https://github.com/m3db/m3)

## 總結與對比

### 核心設計原則

```
1. 時序數據庫（TSDB）
   問題：PostgreSQL 查詢慢（8.7 秒）
   方案：專門的時序存儲（按時間分塊）
   效果：0.05 秒（提升 174 倍）

2. 壓縮算法（Gorilla）
   問題：存儲成本高（1.41 TB）
   方案：Delta-of-Delta + XOR 編碼
   效果：120 GB（壓縮率 11.7:1）

3. 下採樣（Downsampling）
   問題：長期存儲成本（3.6 TB/月）
   方案：分層存儲（7 天原始 + 聚合）
   效果：852 GB（節省 76%）

4. 告警規則引擎
   問題：被動監控（人工查看）
   方案：自動評估 + 通知
   效果：秒級發現問題

5. 查詢語言（PromQL）
   問題：複雜查詢困難
   方案：聲明式查詢語言
   效果：靈活強大
```

### 方案對比

| 方案 | 查詢速度 | 存儲效率 | 擴展性 | 適用規模 |
|------|---------|---------|--------|---------|
| **PostgreSQL** | 慢（8.7s） | 差（72 MB） | 差 | < 100 台 |
| **單機 TSDB** | 快（0.05s） | 優（5 MB） | 無法擴展 | < 1,000 台 |
| **分片 TSDB** | 快 | 優 | 橫向擴展 | < 10,000 台 |
| **M3/Thanos** | 極快 | 極優 | 無限擴展 | 數萬台+ |

### 適用場景

**適合使用監控系統的場景：**
- 服務器監控（CPU、內存、磁盤）
- 應用監控（QPS、延遲、錯誤率）
- 業務監控（訂單量、收入、用戶活躍）
- 基礎設施監控（數據庫、快取、消息隊列）

**不適合的場景：**
- 日誌存儲（用 ELK）
- 事件追蹤（用 Tracing）
- 全文檢索（用 Elasticsearch）

### 關鍵指標

```
最終性能（單機 TSDB + Gorilla + 下採樣）：
- 支持時間序列：100 萬
- 寫入吞吐：每秒 10 萬數據點
- 查詢延遲：P99 < 100ms
- 存儲效率：壓縮率 11.7:1
- 告警延遲：30 秒（評估間隔）

與 PostgreSQL 對比：
- 查詢速度：174 倍
- 存儲效率：14.4 倍
- 成本：$1,728/年 → $144/年
```

### 延伸閱讀

**時序數據庫：**
- Prometheus（最流行的開源監控系統）
- InfluxDB（Go 編寫的 TSDB）
- TimescaleDB（基於 PostgreSQL 的擴展）
- VictoriaMetrics（高性能 TSDB）
- M3（Uber 開源的分布式 TSDB）

**壓縮算法：**
- Gorilla（Facebook, 2015）
- Delta-of-Delta 編碼
- XOR 編碼

**查詢語言：**
- PromQL（Prometheus Query Language）
- Flux（InfluxDB 2.0）
- SQL（TimescaleDB）

---

從「雙11凌晨的噩夢」（損失 NT$ 6,300 萬）到「秒級發現問題的監控系統」，Metrics Monitoring 經歷了 5 次重大演進：

1. **沒有監控** → 手動檢查腳本
2. **數據庫存儲** → 時序數據庫（174 倍速度提升）
3. **存儲成本** → Gorilla 壓縮（11.7:1）+ 下採樣（節省 76%）
4. **被動監控** → 告警規則引擎（秒級響應）
5. **複雜查詢** → PromQL 查詢語言

**記住：** 監控是生產系統的眼睛。沒有監控，就像蒙著眼睛開車——早晚會出事。好的監控系統不僅能及時發現問題，更能幫助你理解系統行為、預測未來趨勢、持續優化性能。

**核心理念：** You can't improve what you can't measure.（無法測量就無法改進）
