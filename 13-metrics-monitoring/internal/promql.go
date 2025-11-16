package internal

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PromQLExecutor PromQL 查詢執行器
type PromQLExecutor struct {
	db *TimeSeriesDB
}

// NewPromQLExecutor 創建 PromQL 執行器
func NewPromQLExecutor(db *TimeSeriesDB) *PromQLExecutor {
	return &PromQLExecutor{
		db: db,
	}
}

// Execute 執行 PromQL 查詢
func (pe *PromQLExecutor) Execute(query string) (float64, error) {
	query = strings.TrimSpace(query)

	// 解析聚合函數: sum(metric_name), avg(metric_name), etc.
	aggPattern := regexp.MustCompile(`^(sum|avg|max|min|count)\((.+)\)$`)
	if matches := aggPattern.FindStringSubmatch(query); len(matches) == 3 {
		aggFunc := matches[1]
		innerQuery := matches[2]

		// 檢查是否包含 rate() 函數
		if strings.Contains(innerQuery, "rate(") {
			return pe.executeRate(innerQuery, aggFunc)
		}

		// 簡單聚合查詢
		return pe.executeAggregation(innerQuery, aggFunc)
	}

	// 解析 rate() 函數: rate(metric_name[5m])
	ratePattern := regexp.MustCompile(`^rate\((.+)\[(\d+)([smhd])\]\)$`)
	if matches := ratePattern.FindStringSubmatch(query); len(matches) == 4 {
		metricName := matches[1]
		duration, _ := strconv.Atoi(matches[2])
		unit := matches[3]

		return pe.executeRateOnly(metricName, duration, unit)
	}

	// 簡單查詢: metric_name
	return pe.executeSimple(query)
}

// executeAggregation 執行聚合查詢
func (pe *PromQLExecutor) executeAggregation(metricName string, aggFunc string) (float64, error) {
	now := time.Now().Unix()
	start := now - 300 // 最近 5 分鐘

	return pe.db.Aggregate(metricName, aggFunc, start, now)
}

// executeRate 執行帶 rate 的聚合查詢
func (pe *PromQLExecutor) executeRate(innerQuery string, aggFunc string) (float64, error) {
	// 解析 rate(metric_name[5m])
	ratePattern := regexp.MustCompile(`^rate\((.+)\[(\d+)([smhd])\]\)$`)
	matches := ratePattern.FindStringSubmatch(innerQuery)
	if len(matches) != 4 {
		return 0, fmt.Errorf("invalid rate query: %s", innerQuery)
	}

	metricName := matches[1]
	duration, _ := strconv.Atoi(matches[2])
	unit := matches[3]

	// 計算時間窗口
	windowSeconds := pe.parseDuration(duration, unit)
	now := time.Now().Unix()
	start := now - windowSeconds

	// 查詢數據
	metrics := pe.db.QueryRange(metricName, start, now, nil)
	if len(metrics) < 2 {
		return 0, fmt.Errorf("insufficient data for rate calculation")
	}

	// 計算每個序列的 rate
	seriesRates := make(map[string]float64)
	seriesData := make(map[string][]*Metric)

	for _, m := range metrics {
		key := pe.getSeriesKey(m)
		seriesData[key] = append(seriesData[key], m)
	}

	for seriesKey, data := range seriesData {
		if len(data) < 2 {
			continue
		}

		// 計算 rate: (最後值 - 第一值) / 時間跨度
		firstValue := data[0].Value
		lastValue := data[len(data)-1].Value
		firstTime := data[0].Timestamp / 1000
		lastTime := data[len(data)-1].Timestamp / 1000

		if lastTime > firstTime {
			rate := (lastValue - firstValue) / float64(lastTime-firstTime)
			seriesRates[seriesKey] = rate
		}
	}

	// 應用聚合函數
	if len(seriesRates) == 0 {
		return 0, fmt.Errorf("no data for aggregation")
	}

	switch aggFunc {
	case "sum":
		var sum float64
		for _, rate := range seriesRates {
			sum += rate
		}
		return sum, nil

	case "avg":
		var sum float64
		for _, rate := range seriesRates {
			sum += rate
		}
		return sum / float64(len(seriesRates)), nil

	case "max":
		var max float64
		first := true
		for _, rate := range seriesRates {
			if first || rate > max {
				max = rate
				first = false
			}
		}
		return max, nil

	case "min":
		var min float64
		first := true
		for _, rate := range seriesRates {
			if first || rate < min {
				min = rate
				first = false
			}
		}
		return min, nil

	default:
		return 0, fmt.Errorf("unknown aggregation function: %s", aggFunc)
	}
}

// executeRateOnly 執行純 rate 查詢
func (pe *PromQLExecutor) executeRateOnly(metricName string, duration int, unit string) (float64, error) {
	windowSeconds := pe.parseDuration(duration, unit)
	now := time.Now().Unix()
	start := now - windowSeconds

	metrics := pe.db.QueryRange(metricName, start, now, nil)
	if len(metrics) < 2 {
		return 0, fmt.Errorf("insufficient data for rate calculation")
	}

	// 計算總的 rate
	firstValue := metrics[0].Value
	lastValue := metrics[len(metrics)-1].Value
	firstTime := metrics[0].Timestamp / 1000
	lastTime := metrics[len(metrics)-1].Timestamp / 1000

	if lastTime > firstTime {
		rate := (lastValue - firstValue) / float64(lastTime-firstTime)
		return rate, nil
	}

	return 0, nil
}

// executeSimple 執行簡單查詢（返回最新值）
func (pe *PromQLExecutor) executeSimple(metricName string) (float64, error) {
	now := time.Now().Unix()
	start := now - 60 // 最近 1 分鐘

	metrics := pe.db.QueryRange(metricName, start, now, nil)
	if len(metrics) == 0 {
		return 0, fmt.Errorf("no data found")
	}

	// 返回最新值
	return metrics[len(metrics)-1].Value, nil
}

// parseDuration 解析時間長度
func (pe *PromQLExecutor) parseDuration(value int, unit string) int64 {
	switch unit {
	case "s":
		return int64(value)
	case "m":
		return int64(value * 60)
	case "h":
		return int64(value * 3600)
	case "d":
		return int64(value * 86400)
	default:
		return int64(value * 60) // 默認分鐘
	}
}

// getSeriesKey 獲取序列鍵
func (pe *PromQLExecutor) getSeriesKey(metric *Metric) string {
	key := metric.Name
	if metric.Labels != nil {
		for k, v := range metric.Labels {
			key += "{" + k + "=" + v + "}"
		}
	}
	return key
}

// ExecuteRange 執行範圍查詢（返回時間序列）
func (pe *PromQLExecutor) ExecuteRange(query string, start, end int64) ([]*Metric, error) {
	// 提取指標名稱（簡化實現）
	metricName := query
	if strings.Contains(query, "(") {
		// 從函數中提取指標名稱
		re := regexp.MustCompile(`\(([^)]+)\)`)
		matches := re.FindStringSubmatch(query)
		if len(matches) > 1 {
			metricName = matches[1]
			// 移除時間範圍標記 [5m]
			if idx := strings.Index(metricName, "["); idx != -1 {
				metricName = metricName[:idx]
			}
		}
	}

	return pe.db.QueryRange(metricName, start, end, nil), nil
}

// ParseQuery 解析 PromQL 查詢（用於驗證）
func (pe *PromQLExecutor) ParseQuery(query string) (map[string]interface{}, error) {
	query = strings.TrimSpace(query)

	result := make(map[string]interface{})

	// 檢查聚合函數
	aggPattern := regexp.MustCompile(`^(sum|avg|max|min|count)\((.+)\)$`)
	if matches := aggPattern.FindStringSubmatch(query); len(matches) == 3 {
		result["type"] = "aggregation"
		result["function"] = matches[1]
		result["inner_query"] = matches[2]
		return result, nil
	}

	// 檢查 rate 函數
	ratePattern := regexp.MustCompile(`^rate\((.+)\[(\d+)([smhd])\]\)$`)
	if matches := ratePattern.FindStringSubmatch(query); len(matches) == 4 {
		result["type"] = "rate"
		result["metric_name"] = matches[1]
		result["duration"] = matches[2]
		result["unit"] = matches[3]
		return result, nil
	}

	// 簡單查詢
	result["type"] = "simple"
	result["metric_name"] = query

	return result, nil
}

// ValidateQuery 驗證 PromQL 查詢語法
func (pe *PromQLExecutor) ValidateQuery(query string) error {
	_, err := pe.ParseQuery(query)
	return err
}
