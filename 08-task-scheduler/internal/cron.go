package internal

import (
	"fmt"
	"time"
)

// CronParser 簡化版 Cron 解析器（教學用）
//
// 教學簡化：
//   - 當前實現：只支持簡單的秒級間隔
//   - 生產環境建議：使用 github.com/robfig/cron 庫
//
// 支持格式：
//   - "30s"  - 每 30 秒
//   - "5m"   - 每 5 分鐘
//   - "1h"   - 每 1 小時
//
// 不支持（生產環境需求）：
//   - Cron 表達式："0 2 * * *"（每天 2:00）
//   - 複雜規則："*/5 * * * *"（每 5 分鐘）
type CronParser struct{}

// NewCronParser 創建 Cron 解析器
func NewCronParser() *CronParser {
	return &CronParser{}
}

// ParseInterval 解析間隔表達式
//
// 簡化實現：只支持 duration 格式
//
// 範例：
//   "30s" → 30 秒
//   "5m"  → 5 分鐘
//   "1h"  → 1 小時
func (cp *CronParser) ParseInterval(expr string) (time.Duration, error) {
	duration, err := time.ParseDuration(expr)
	if err != nil {
		return 0, fmt.Errorf("無效的間隔表達式: %s", expr)
	}

	if duration <= 0 {
		return 0, fmt.Errorf("間隔必須大於 0")
	}

	return duration, nil
}

// NextExecuteTime 計算下次執行時間
//
// 簡化版：當前時間 + 間隔
//
// 生產環境（完整 Cron）：
//   - 需要解析 Cron 表達式："0 2 * * *"
//   - 計算下一個符合條件的時間點
//   - 考慮時區、夏令時等複雜情況
func (cp *CronParser) NextExecuteTime(interval time.Duration) time.Time {
	return time.Now().Add(interval)
}
