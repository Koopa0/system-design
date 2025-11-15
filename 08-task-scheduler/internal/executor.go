package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// TaskExecutor 任務執行器
//
// 系統設計考量：
//
//  1. 為什麼需要獨立的執行器？
//     - 時間輪：負責調度（何時執行）
//     - 執行器：負責執行（如何執行）
//     - 關注點分離：時間輪不應該關心 HTTP、重試等細節
//
//  2. HTTP 回調 vs 直接調用？
//     HTTP 回調優勢：
//     - 解耦：任務調度器與業務服務分離
//     - 語言無關：業務服務可以用任何語言
//     - 擴展性：業務服務可以獨立擴展
//
//  3. 重試策略：指數退避
//     為什麼用指數退避而非固定間隔？
//     - 避免雪崩：給下游服務恢復時間
//     - 成功率提升：網絡抖動、臨時故障有時間恢復
//     - 範例：1s → 2s → 4s → 8s
type TaskExecutor struct {
	client *http.Client
	cfg    *ExecutorConfig
}

// NewTaskExecutor 創建任務執行器
func NewTaskExecutor(cfg *ExecutorConfig) *TaskExecutor {
	return &TaskExecutor{
		client: &http.Client{
			Timeout: cfg.CallbackTimeout,
		},
		cfg: cfg,
	}
}

// Execute 執行任務
//
// 執行流程：
//  1. 發送 HTTP POST 到 CallbackURL
//  2. 如果失敗：指數退避重試
//  3. 達到最大重試次數：記錄到死信隊列（教學簡化）
//
// 系統設計重點：
//
//  1. 冪等性要求：
//     問題：網絡重試可能導致任務重複執行
//     解決：業務服務必須保證冪等性
//     範例：
//     - 訂單取消：檢查訂單狀態，已取消則跳過
//     - 數據庫去重：唯一索引、INSERT ON CONFLICT
//
//  2. 超時設置：
//     CallbackTimeout = 30 秒
//     如果業務邏輯超過 30 秒：
//     - 應該設計為異步（任務立即返回，後台處理）
//     - 或增加超時時間（需權衡）
//
//  3. 錯誤分類：
//     - 臨時錯誤（可重試）：網絡超時、503 服務不可用
//     - 永久錯誤（不重試）：404、400 錯誤請求
func (te *TaskExecutor) Execute(task *ScheduledTask) {
	log.Printf("⏰ 執行任務: ID=%s, CallbackURL=%s", task.ID, task.CallbackURL)

	// 1. 準備 HTTP 請求
	payload := map[string]interface{}{
		"task_id":    task.ID,
		"execute_at": task.ExecuteAt,
		"data":       task.Data,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		log.Printf("❌ 任務序列化失敗: %s, Error: %v", task.ID, err)
		return
	}

	// 2. 執行（帶重試）
	var lastErr error
	for attempt := 0; attempt <= te.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			// 指數退避：2^(attempt-1) × BaseDelay
			// attempt=1 → 1s, attempt=2 → 2s, attempt=3 → 4s
			delay := time.Duration(1<<(attempt-1)) * te.cfg.RetryBaseDelay
			log.Printf("   ⏳ 重試 %d/%d，等待 %s...", attempt, te.cfg.MaxRetries, delay)
			time.Sleep(delay)
		}

		// 發送 HTTP POST
		ctx, cancel := context.WithTimeout(context.Background(), te.cfg.CallbackTimeout)
		req, err := http.NewRequestWithContext(ctx, "POST", task.CallbackURL, bytes.NewBuffer(payloadJSON))
		if err != nil {
			cancel()
			lastErr = err
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Task-ID", task.ID)

		resp, err := te.client.Do(req)
		cancel()

		if err != nil {
			lastErr = err
			log.Printf("   ⚠️  HTTP 請求失敗: %v", err)
			continue
		}

		// 檢查狀態碼
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			resp.Body.Close()
			log.Printf("   ✅ 任務執行成功: ID=%s, StatusCode=%d", task.ID, resp.StatusCode)
			return
		}

		// 4xx 錯誤：客戶端錯誤，不重試
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			resp.Body.Close()
			log.Printf("   ❌ 任務執行失敗（客戶端錯誤，不重試）: ID=%s, StatusCode=%d", task.ID, resp.StatusCode)
			return
		}

		// 5xx 錯誤：服務器錯誤，可重試
		resp.Body.Close()
		lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
		log.Printf("   ⚠️  服務器錯誤: %v", lastErr)
	}

	// 3. 所有重試都失敗
	log.Printf("   💀 任務最終失敗: ID=%s, Error: %v", task.ID, lastErr)

	// 教學簡化：實際生產環境應該：
	// 1. 發送到死信隊列（DLQ）
	// 2. 發送告警（郵件、Slack）
	// 3. 記錄到監控系統
	// te.sendToDeadLetterQueue(task, lastErr)
}
