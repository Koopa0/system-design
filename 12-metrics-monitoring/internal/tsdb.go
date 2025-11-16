package internal

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Metric 指標數據
type Metric struct {
	Name      string            `json:"name"`
	Labels    map[string]string `json:"labels"`
	Timestamp int64             `json:"timestamp"` // 毫秒
	Value     float64           `json:"value"`
}

// Config 配置
type Config struct {
	Port              int
	RetentionRaw      time.Duration // 原始數據保留時間
	RetentionAgg5m    time.Duration // 5分鐘聚合數據保留時間
	RetentionAgg1h    time.Duration // 1小時聚合數據保留時間
	BlockDuration     time.Duration // Block 時長
	AlertEvalInterval time.Duration // 告警評估間隔
	MaxSeriesPerQuery int           // 單次查詢最大序列數
}

// TimeSeriesDB 時序數據庫
type TimeSeriesDB struct {
	config      *Config
	blocks      map[int64]*Block       // 按小時分塊
	series      map[string]*TimeSeries // 時間序列
	mu          sync.RWMutex
	compressor  *GorillaCompressor
	downsampler *Downsampler
}

// Block 數據塊
type Block struct {
	StartTime int64                  // 塊起始時間
	EndTime   int64                  // 塊結束時間
	Series    map[string]*TimeSeries // 序列數據
	mu        sync.RWMutex
}

// TimeSeries 時間序列
type TimeSeries struct {
	Name       string
	Labels     map[string]string
	Datapoints []*Datapoint
	mu         sync.RWMutex
}

// Datapoint 數據點
type Datapoint struct {
	Timestamp int64
	Value     float64
}

// NewTimeSeriesDB 創建時序數據庫
func NewTimeSeriesDB(config *Config) *TimeSeriesDB {
	db := &TimeSeriesDB{
		config:      config,
		blocks:      make(map[int64]*Block),
		series:      make(map[string]*TimeSeries),
		compressor:  NewGorillaCompressor(),
		downsampler: NewDownsampler(config),
	}

	// 啟動後台任務
	go db.compactionTask()
	go db.retentionTask()

	return db
}

// Write 寫入單個指標
func (db *TimeSeriesDB) Write(metric *Metric) error {
	seriesKey := db.getSeriesKey(metric.Name, metric.Labels)

	db.mu.Lock()
	defer db.mu.Unlock()

	// 獲取或創建時間序列
	series, exists := db.series[seriesKey]
	if !exists {
		series = &TimeSeries{
			Name:       metric.Name,
			Labels:     metric.Labels,
			Datapoints: make([]*Datapoint, 0),
		}
		db.series[seriesKey] = series
	}

	// 添加數據點
	series.mu.Lock()
	series.Datapoints = append(series.Datapoints, &Datapoint{
		Timestamp: metric.Timestamp,
		Value:     metric.Value,
	})
	series.mu.Unlock()

	// 添加到對應的 Block
	blockKey := db.getBlockKey(metric.Timestamp)
	block, exists := db.blocks[blockKey]
	if !exists {
		block = &Block{
			StartTime: blockKey,
			EndTime:   blockKey + int64(db.config.BlockDuration.Milliseconds()),
			Series:    make(map[string]*TimeSeries),
		}
		db.blocks[blockKey] = block
	}

	block.mu.Lock()
	blockSeries, exists := block.Series[seriesKey]
	if !exists {
		blockSeries = &TimeSeries{
			Name:       metric.Name,
			Labels:     metric.Labels,
			Datapoints: make([]*Datapoint, 0),
		}
		block.Series[seriesKey] = blockSeries
	}
	blockSeries.Datapoints = append(blockSeries.Datapoints, &Datapoint{
		Timestamp: metric.Timestamp,
		Value:     metric.Value,
	})
	block.mu.Unlock()

	return nil
}

// WriteBatch 批量寫入指標
func (db *TimeSeriesDB) WriteBatch(metrics []*Metric) error {
	for _, metric := range metrics {
		if err := db.Write(metric); err != nil {
			return err
		}
	}
	return nil
}

// QueryRange 查詢時間範圍
func (db *TimeSeriesDB) QueryRange(name string, start, end int64, labels map[string]string) []*Metric {
	db.mu.RLock()
	defer db.mu.RUnlock()

	results := make([]*Metric, 0)

	// 遍歷所有序列
	for _, series := range db.series {
		// 檢查名稱
		if series.Name != name {
			continue
		}

		// 檢查標籤
		if labels != nil && !db.labelsMatch(series.Labels, labels) {
			continue
		}

		// 查詢數據點
		series.mu.RLock()
		for _, dp := range series.Datapoints {
			timestamp := dp.Timestamp / 1000 // 轉換為秒
			if timestamp >= start && timestamp <= end {
				results = append(results, &Metric{
					Name:      series.Name,
					Labels:    series.Labels,
					Timestamp: dp.Timestamp,
					Value:     dp.Value,
				})
			}
		}
		series.mu.RUnlock()
	}

	return results
}

// Aggregate 聚合查詢
func (db *TimeSeriesDB) Aggregate(name string, aggType string, start, end int64) (float64, error) {
	metrics := db.QueryRange(name, start, end, nil)

	if len(metrics) == 0 {
		return 0, fmt.Errorf("no data found")
	}

	var sum, min, max float64
	min = metrics[0].Value
	max = metrics[0].Value

	for _, m := range metrics {
		sum += m.Value
		if m.Value < min {
			min = m.Value
		}
		if m.Value > max {
			max = m.Value
		}
	}

	switch aggType {
	case "sum":
		return sum, nil
	case "avg":
		return sum / float64(len(metrics)), nil
	case "min":
		return min, nil
	case "max":
		return max, nil
	default:
		return 0, fmt.Errorf("unknown aggregation type: %s", aggType)
	}
}

// getSeriesKey 生成序列鍵
func (db *TimeSeriesDB) getSeriesKey(name string, labels map[string]string) string {
	key := name

	// 排序標籤以確保一致性
	if labels != nil {
		keys := make([]string, 0, len(labels))
		for k := range labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			key += "{" + k + "=" + labels[k] + "}"
		}
	}

	return key
}

// getBlockKey 獲取 Block 鍵
func (db *TimeSeriesDB) getBlockKey(timestamp int64) int64 {
	blockDuration := db.config.BlockDuration.Milliseconds()
	return (timestamp / blockDuration) * blockDuration
}

// labelsMatch 檢查標籤是否匹配
func (db *TimeSeriesDB) labelsMatch(seriesLabels, queryLabels map[string]string) bool {
	for k, v := range queryLabels {
		if seriesLabels[k] != v {
			return false
		}
	}
	return true
}

// compactionTask 壓縮任務
func (db *TimeSeriesDB) compactionTask() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		db.compactOldBlocks()
	}
}

// compactOldBlocks 壓縮舊 Block
func (db *TimeSeriesDB) compactOldBlocks() {
	db.mu.Lock()
	defer db.mu.Unlock()

	now := time.Now().UnixMilli()
	compactionThreshold := now - int64(db.config.RetentionRaw.Milliseconds())

	for blockKey, block := range db.blocks {
		if block.StartTime < compactionThreshold {
			// 壓縮 Block
			db.compressBlock(block)
		}
	}
}

// compressBlock 壓縮 Block
func (db *TimeSeriesDB) compressBlock(block *Block) {
	block.mu.Lock()
	defer block.mu.Unlock()

	for _, series := range block.Series {
		series.mu.Lock()
		if len(series.Datapoints) > 0 {
			// 使用 Gorilla 壓縮
			timestamps := make([]int64, len(series.Datapoints))
			values := make([]float64, len(series.Datapoints))

			for i, dp := range series.Datapoints {
				timestamps[i] = dp.Timestamp
				values[i] = dp.Value
			}

			// 壓縮（這裡只是演示，實際壓縮數據會存儲到磁盤）
			_ = db.compressor.Compress(timestamps, values)
		}
		series.mu.Unlock()
	}
}

// retentionTask 數據保留任務
func (db *TimeSeriesDB) retentionTask() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		db.cleanupOldData()
	}
}

// cleanupOldData 清理過期數據
func (db *TimeSeriesDB) cleanupOldData() {
	db.mu.Lock()
	defer db.mu.Unlock()

	now := time.Now().UnixMilli()
	retentionThreshold := now - int64(db.config.RetentionRaw.Milliseconds())

	// 刪除過期的 Block
	for blockKey := range db.blocks {
		if blockKey < retentionThreshold {
			delete(db.blocks, blockKey)
		}
	}

	// 清理時間序列中的過期數據點
	for _, series := range db.series {
		series.mu.Lock()
		newDatapoints := make([]*Datapoint, 0)
		for _, dp := range series.Datapoints {
			if dp.Timestamp >= retentionThreshold {
				newDatapoints = append(newDatapoints, dp)
			}
		}
		series.Datapoints = newDatapoints
		series.mu.Unlock()
	}
}

// GetStats 獲取統計數據
func (db *TimeSeriesDB) GetStats() map[string]interface{} {
	db.mu.RLock()
	defer db.mu.RUnlock()

	totalDatapoints := 0
	for _, series := range db.series {
		series.mu.RLock()
		totalDatapoints += len(series.Datapoints)
		series.mu.RUnlock()
	}

	return map[string]interface{}{
		"total_series":     len(db.series),
		"total_blocks":     len(db.blocks),
		"total_datapoints": totalDatapoints,
	}
}
