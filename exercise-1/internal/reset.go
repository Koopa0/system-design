package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// ResetScheduler 每日重置排程器
type ResetScheduler struct {
	counter *Counter
	logger  *slog.Logger
	stop    chan struct{}
}

// NewResetScheduler 創建重置排程器
func NewResetScheduler(counter *Counter, logger *slog.Logger) *ResetScheduler {
	return &ResetScheduler{
		counter: counter,
		logger:  logger,
		stop:    make(chan struct{}),
	}
}

// Start 啟動排程器
func (rs *ResetScheduler) Start() {
	go rs.run()
}

// Stop 停止排程器
func (rs *ResetScheduler) Stop() {
	close(rs.stop)
}

func (rs *ResetScheduler) run() {
	// 台北時區
	location, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		rs.logger.Error("failed to load timezone", "error", err)
		return
	}

	// 計算下次午夜時間
	nextMidnight := func() time.Time {
		now := time.Now().In(location)
		next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, location)
		return next
	}

	// 首次執行時間
	firstRun := nextMidnight()
	rs.logger.Info("reset scheduler started",
		"first_run", firstRun.Format("2006-01-02 15:04:05"))

	// 等待首次執行
	timer := time.NewTimer(time.Until(firstRun))
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			// 執行重置
			rs.resetDailyCounters()

			// 設定下次執行時間（24小時後）
			timer.Reset(24 * time.Hour)

		case <-rs.stop:
			rs.logger.Info("reset scheduler stopped")
			return
		}
	}
}

// resetDailyCounters 重置每日計數器
func (rs *ResetScheduler) resetDailyCounters() {
	ctx := context.Background()
	start := time.Now()

	rs.logger.Info("starting daily counter reset")

	// 獲取昨天的日期（用於歸檔）
	location, _ := time.LoadLocation("Asia/Taipei")
	yesterday := time.Now().In(location).AddDate(0, 0, -1)
	dateStr := yesterday.Format("20060102")

	// 需要重置的計數器列表
	dailyCounters := []string{
		"daily_active_users",
		"total_games_played",
	}

	for _, counter := range dailyCounters {
		// 歸檔當前值
		if err := rs.archiveCounter(ctx, counter, yesterday); err != nil {
			rs.logger.Error("failed to archive counter",
				"counter", counter,
				"date", dateStr,
				"error", err)
			continue
		}

		// 重置計數器
		if err := rs.counter.Reset(ctx, counter); err != nil {
			rs.logger.Error("failed to reset counter",
				"counter", counter,
				"error", err)
			continue
		}

		// 清理去重集合
		dauKey := fmt.Sprintf("counter:%s:users:%s", counter, dateStr)
		if err := rs.counter.redis.Del(ctx, dauKey).Err(); err != nil {
			rs.logger.Warn("failed to clean user set",
				"key", dauKey,
				"error", err)
		}

		rs.logger.Info("counter reset completed",
			"counter", counter,
			"date", dateStr)
	}

	// 清理舊的歷史記錄（保留7天）
	rs.cleanOldHistory(ctx)

	rs.logger.Info("daily counter reset completed",
		"duration", time.Since(start))
}

// archiveCounter 歸檔計數器
func (rs *ResetScheduler) archiveCounter(ctx context.Context, name string, date time.Time) error {
	// 獲取當前值
	value, err := rs.counter.GetValue(ctx, name)
	if err != nil {
		return fmt.Errorf("get value: %w", err)
	}

	// 獲取去重用戶列表（如果是 DAU 類型計數器）
	var uniqueUsers []string
	if name == "daily_active_users" {
		dateStr := date.Format("20060102")
		dauKey := fmt.Sprintf("counter:%s:users:%s", name, dateStr)

		uniqueUsers, err = rs.counter.redis.SMembers(ctx, dauKey).Result()
		if err != nil {
			rs.logger.Warn("failed to get unique users",
				"key", dauKey,
				"error", err)
		}
	}

	// 儲存到 PostgreSQL
	query := `
		INSERT INTO counter_history (counter_name, date, final_value, unique_users, metadata)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (counter_name, date) DO UPDATE
		SET final_value = EXCLUDED.final_value,
		    unique_users = EXCLUDED.unique_users,
		    metadata = EXCLUDED.metadata
	`

	usersJSON, _ := json.Marshal(uniqueUsers)
	metadata := map[string]any{
		"archived_at": time.Now(),
		"user_count":  len(uniqueUsers),
	}
	metadataJSON, _ := json.Marshal(metadata)

	_, err = rs.counter.pg.Exec(ctx, query,
		name,
		date.Format("2006-01-02"),
		value,
		usersJSON,
		metadataJSON,
	)

	return err
}

// cleanOldHistory 清理舊的歷史記錄
func (rs *ResetScheduler) cleanOldHistory(ctx context.Context) {
	query := `DELETE FROM counter_history WHERE date < CURRENT_DATE - INTERVAL '7 days'`

	result, err := rs.counter.pg.Exec(ctx, query)
	if err != nil {
		rs.logger.Error("failed to clean old history", "error", err)
		return
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected > 0 {
		rs.logger.Info("cleaned old history records", "rows", rowsAffected)
	}
}
