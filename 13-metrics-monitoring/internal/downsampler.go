package internal

import (
	"sync"
	"time"
)

// Downsampler 降採樣器
type Downsampler struct {
	config       *Config
	agg5mData    map[string][]*Datapoint // 5分鐘聚合數據
	agg1hData    map[string][]*Datapoint // 1小時聚合數據
	mu           sync.RWMutex
	lastAgg5m    int64
	lastAgg1h    int64
}

// NewDownsampler 創建降採樣器
func NewDownsampler(config *Config) *Downsampler {
	ds := &Downsampler{
		config:    config,
		agg5mData: make(map[string][]*Datapoint),
		agg1hData: make(map[string][]*Datapoint),
	}

	// 啟動定期降採樣任務
	go ds.downsampleTask()

	return ds
}

// downsampleTask 降採樣任務
func (ds *Downsampler) downsampleTask() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		ds.performDownsampling()
	}
}

// performDownsampling 執行降採樣
func (ds *Downsampler) performDownsampling() {
	now := time.Now().Unix()

	// 每 5 分鐘執行一次 5 分鐘級別的降採樣
	if now-ds.lastAgg5m >= 300 {
		ds.downsample5m()
		ds.lastAgg5m = now
	}

	// 每 1 小時執行一次 1 小時級別的降採樣
	if now-ds.lastAgg1h >= 3600 {
		ds.downsample1h()
		ds.lastAgg1h = now
	}
}

// downsample5m 5 分鐘降採樣
func (ds *Downsampler) downsample5m() {
	// 這裡簡化實現，實際應該從主數據庫讀取並聚合
	// 降採樣邏輯：將過去 5 分鐘的數據聚合為一個點
}

// downsample1h 1 小時降採樣
func (ds *Downsampler) downsample1h() {
	// 這裡簡化實現，實際應該從 5 分鐘數據讀取並聚合
	// 降採樣邏輯：將過去 1 小時的數據聚合為一個點
}

// Downsample 對數據點進行降採樣
func (ds *Downsampler) Downsample(datapoints []*Datapoint, windowSize time.Duration, aggFunc string) []*Datapoint {
	if len(datapoints) == 0 {
		return []*Datapoint{}
	}

	windowMs := windowSize.Milliseconds()
	result := make([]*Datapoint, 0)

	// 按時間窗口分組
	var currentWindow []*Datapoint
	var windowStart int64

	for i, dp := range datapoints {
		if i == 0 {
			windowStart = (dp.Timestamp / windowMs) * windowMs
			currentWindow = []*Datapoint{dp}
			continue
		}

		dpWindowStart := (dp.Timestamp / windowMs) * windowMs
		if dpWindowStart == windowStart {
			// 同一窗口
			currentWindow = append(currentWindow, dp)
		} else {
			// 新窗口，先聚合當前窗口
			aggregated := ds.aggregateWindow(currentWindow, windowStart, aggFunc)
			if aggregated != nil {
				result = append(result, aggregated)
			}

			// 開始新窗口
			windowStart = dpWindowStart
			currentWindow = []*Datapoint{dp}
		}
	}

	// 處理最後一個窗口
	if len(currentWindow) > 0 {
		aggregated := ds.aggregateWindow(currentWindow, windowStart, aggFunc)
		if aggregated != nil {
			result = append(result, aggregated)
		}
	}

	return result
}

// aggregateWindow 聚合窗口內的數據點
func (ds *Downsampler) aggregateWindow(datapoints []*Datapoint, timestamp int64, aggFunc string) *Datapoint {
	if len(datapoints) == 0 {
		return nil
	}

	var value float64

	switch aggFunc {
	case "avg":
		var sum float64
		for _, dp := range datapoints {
			sum += dp.Value
		}
		value = sum / float64(len(datapoints))

	case "sum":
		for _, dp := range datapoints {
			value += dp.Value
		}

	case "min":
		value = datapoints[0].Value
		for _, dp := range datapoints {
			if dp.Value < value {
				value = dp.Value
			}
		}

	case "max":
		value = datapoints[0].Value
		for _, dp := range datapoints {
			if dp.Value > value {
				value = dp.Value
			}
		}

	case "first":
		value = datapoints[0].Value

	case "last":
		value = datapoints[len(datapoints)-1].Value

	default:
		// 默認使用平均值
		var sum float64
		for _, dp := range datapoints {
			sum += dp.Value
		}
		value = sum / float64(len(datapoints))
	}

	return &Datapoint{
		Timestamp: timestamp,
		Value:     value,
	}
}

// GetDownsampledData 獲取降採樣數據
func (ds *Downsampler) GetDownsampledData(seriesKey string, resolution string) []*Datapoint {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	switch resolution {
	case "5m":
		return ds.agg5mData[seriesKey]
	case "1h":
		return ds.agg1hData[seriesKey]
	default:
		return nil
	}
}

// CleanupOldData 清理過期的降採樣數據
func (ds *Downsampler) CleanupOldData() {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	now := time.Now().UnixMilli()

	// 清理 5 分鐘聚合數據（保留 30 天）
	threshold5m := now - int64(ds.config.RetentionAgg5m.Milliseconds())
	for seriesKey, datapoints := range ds.agg5mData {
		newDatapoints := make([]*Datapoint, 0)
		for _, dp := range datapoints {
			if dp.Timestamp >= threshold5m {
				newDatapoints = append(newDatapoints, dp)
			}
		}
		ds.agg5mData[seriesKey] = newDatapoints
	}

	// 清理 1 小時聚合數據（保留 1 年）
	threshold1h := now - int64(ds.config.RetentionAgg1h.Milliseconds())
	for seriesKey, datapoints := range ds.agg1hData {
		newDatapoints := make([]*Datapoint, 0)
		for _, dp := range datapoints {
			if dp.Timestamp >= threshold1h {
				newDatapoints = append(newDatapoints, dp)
			}
		}
		ds.agg1hData[seriesKey] = newDatapoints
	}
}
