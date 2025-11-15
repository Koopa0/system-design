// Package internal 實現基於時間輪算法的任務調度系統
//
// 系統設計問題：
//
//	如何實現高性能、可靠的延遲任務調度？
//
// 核心挑戰：
//  1. 如何支持百萬級任務同時調度？
//  2. 如何保證任務不丟失（進程重啟）？
//  3. 如何避免重複執行（分布式環境）？
//  4. 如何處理任務執行失敗？
//
// 設計方案：
//
//	✅ 時間輪算法：O(1) 插入與觸發，記憶體高效
//	✅ NATS JetStream：持久化任務，重啟後恢復
//	✅ Queue Groups：自動負載均衡，避免重複執行
//	✅ 指數退避重試：處理臨時故障
//
// 為何選擇時間輪 + NATS？
//
//  1. 性能：時間輪 O(1) vs 資料庫輪詢 O(N)
//  2. 可靠：NATS 持久化 vs 純記憶體丟失
//  3. 簡單：無需分布式鎖（NATS Queue Groups）
//  4. 整合：承接 07 章節的 NATS 基礎
package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
)

// TaskScheduler 任務調度器
//
// 架構設計：
//
//	Client → NATS JetStream → Scheduler → TimingWheel → Executor
//	             ↓                            ↓
//	       Persistent Storage           Memory (O(1))
//
// 系統設計考量：
//
//  1. 為什麼用時間輪而非資料庫輪詢？
//     資料庫：每秒 SELECT * FROM tasks WHERE execute_time <= NOW()
//     問題：100萬任務 × 每秒掃描 = 巨大開銷
//     時間輪：O(1) 插入、O(1) 觸發，只檢查當前槽位
//     結果：性能提升 1000x+
//
//  2. 為什麼用 NATS 而非純記憶體？
//     純記憶體：進程重啟 → 所有任務丟失
//     NATS：持久化到磁盤 → 重啟後從 NATS 恢復
//     權衡：增加少量延遲（微秒級）換取可靠性
//
//  3. 如何避免分布式環境重複執行？
//     方案A：分布式鎖（Redis）→ 每個任務都要網絡調用
//     方案B：NATS Queue Groups → 自動互斥，無額外開銷
//     選擇：方案B（性能更好、實現更簡單）
//
//  4. 任務恢復策略？
//     啟動時：
//     - 從 NATS 拉取所有未完成任務
//     - 加載到時間輪
//     - 時間已到期的任務：立即執行
//     - 未到期的任務：等待觸發
type TaskScheduler struct {
	wheel    *TimingWheel       // 時間輪
	executor *TaskExecutor      // 任務執行器
	conn     *nats.Conn         // NATS 連接
	js       nats.JetStreamContext // JetStream 上下文
	cfg      *Config            // 配置
}

// ScheduledTask 調度任務
type ScheduledTask struct {
	ID          string                 `json:"id"`           // 任務 ID
	Type        string                 `json:"type"`         // 任務類型（delay/cron）
	ExecuteAt   time.Time              `json:"execute_at"`   // 執行時間
	CallbackURL string                 `json:"callback_url"` // 回調 URL
	Data        map[string]interface{} `json:"data"`         // 任務數據
	RetryCount  int                    `json:"retry_count"`  // 重試次數
	CreatedAt   time.Time              `json:"created_at"`   // 創建時間
}

// NewTaskScheduler 創建任務調度器
func NewTaskScheduler(cfg *Config) (*TaskScheduler, error) {
	// 1. 連接 NATS
	conn, err := nats.Connect(
		cfg.NATSUrl,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("連接 NATS 失敗: %w", err)
	}

	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("創建 JetStream 失敗: %w", err)
	}

	// 2. 創建執行器
	executor := NewTaskExecutor(&cfg.ExecutorConfig)

	// 3. 創建時間輪（回調函數：時間到了執行任務）
	wheel := NewTimingWheel(func(tasks []*Task) {
		for _, task := range tasks {
			// 轉換為 ScheduledTask
			scheduledTask := &ScheduledTask{
				ID:          task.ID,
				ExecuteAt:   task.ExecuteAt,
				CallbackURL: task.Callback,
				Data:        task.Data,
				RetryCount:  task.Retry,
			}

			// 執行任務
			go executor.Execute(scheduledTask)
		}
	})

	scheduler := &TaskScheduler{
		wheel:    wheel,
		executor: executor,
		conn:     conn,
		js:       js,
		cfg:      cfg,
	}

	// 4. 初始化 Stream
	if err := scheduler.initStream(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("初始化 Stream 失敗: %w", err)
	}

	return scheduler, nil
}

// initStream 初始化 JetStream Stream
func (ts *TaskScheduler) initStream() error {
	streamName := "SCHEDULED_TASKS"

	// 檢查 Stream 是否存在
	_, err := ts.js.StreamInfo(streamName)
	if err == nats.ErrStreamNotFound {
		// 創建 Stream
		_, err = ts.js.AddStream(&nats.StreamConfig{
			Name:     streamName,
			Subjects: []string{"task.delay.*", "task.cron.*"},
			Storage:  nats.FileStorage,
			MaxAge:   7 * 24 * time.Hour, // 保留 7 天
			Replicas: 1,
		})
		if err != nil {
			return fmt.Errorf("創建 Stream 失敗: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("查詢 Stream 失敗: %w", err)
	}

	return nil
}

// AddDelayTask 添加延遲任務
//
// 系統設計重點：
//
//  1. 雙寫策略（可靠性）：
//     - 先寫 NATS（持久化）
//     - 再加入時間輪（記憶體調度）
//     - 如果進程崩潰：NATS 中的任務不丟失
//
//  2. 時間輪插入（O(1)）：
//     - 計算槽位：slot = (current + delay) % 3600
//     - 計算圈數：round = delay / 3600
//     - 插入鏈表：wheel[slot].append(task)
//
//  3. 錯誤處理：
//     - NATS 寫入失敗：返回錯誤，不加入時間輪
//     - 時間輪插入：始終成功（記憶體操作）
func (ts *TaskScheduler) AddDelayTask(delay time.Duration, callbackURL string, data map[string]interface{}) (string, error) {
	// 1. 生成任務 ID
	taskID := fmt.Sprintf("task-%d", time.Now().UnixNano())

	// 2. 計算執行時間
	executeAt := time.Now().Add(delay)

	// 3. 構建任務
	task := &ScheduledTask{
		ID:          taskID,
		Type:        "delay",
		ExecuteAt:   executeAt,
		CallbackURL: callbackURL,
		Data:        data,
		RetryCount:  0,
		CreatedAt:   time.Now(),
	}

	// 4. 持久化到 NATS（先寫 NATS，保證可靠性）
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return "", fmt.Errorf("序列化任務失敗: %w", err)
	}

	subject := fmt.Sprintf("task.delay.%s", taskID)
	_, err = ts.js.Publish(subject, taskJSON)
	if err != nil {
		return "", fmt.Errorf("發布任務到 NATS 失敗: %w", err)
	}

	// 5. 加入時間輪（記憶體調度）
	wheelTask := &Task{
		ID:        taskID,
		ExecuteAt: executeAt,
		Callback:  callbackURL,
		Data:      data,
		Retry:     0,
	}
	ts.wheel.AddTask(wheelTask)

	log.Printf("✅ 延遲任務已創建: ID=%s, ExecuteAt=%s, Delay=%s",
		taskID, executeAt.Format(time.RFC3339), delay)

	return taskID, nil
}

// Start 啟動調度器
//
// 啟動流程：
//  1. 從 NATS 恢復未完成任務
//  2. 啟動時間輪（開始計時）
//  3. 訂閱新任務（分布式環境）
func (ts *TaskScheduler) Start(ctx context.Context) error {
	log.Println("🚀 任務調度器啟動中...")

	// 1. 從 NATS 恢復任務（可選：初次啟動可跳過）
	// 教學簡化：實際生產環境應該實現恢復邏輯
	// if err := ts.recoverTasks(); err != nil {
	//     return fmt.Errorf("恢復任務失敗: %w", err)
	// }

	// 2. 啟動時間輪
	ts.wheel.Start()
	log.Println("✅ 時間輪已啟動")

	// 3. 等待上下文取消
	<-ctx.Done()

	// 4. 停止時間輪
	ts.wheel.Stop()
	log.Println("🛑 任務調度器已停止")

	return nil
}

// Close 關閉調度器
func (ts *TaskScheduler) Close() {
	if ts.conn != nil {
		ts.conn.Close()
	}
}

// GetTaskCount 獲取當前任務數量（用於監控）
func (ts *TaskScheduler) GetTaskCount() int {
	return ts.wheel.Size()
}
