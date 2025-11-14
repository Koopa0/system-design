package strategy

import (
	"context"
	"sync"
	"time"

	"github.com/Koopa0/system-design/05-distributed-cache/internal/cache"
)

// WriteBack 實作 Write-Back 策略（寫回）。
//
// 策略說明：
//   寫入時只更新快取，非同步批量寫入資料庫
//   也稱為 Write-Behind 或 Lazy Write
//
// 讀取流程：
//   1. 查詢快取（資料總是在快取中）
//   2. 快取未命中：查詢資料庫 → 寫入快取
//
// 寫入流程：
//   1. 更新快取
//   2. 標記為髒資料（dirty）
//   3. 非同步批量寫入資料庫
//
// 優點：
//   - 寫入效能極高（只寫快取）
//   - 批量寫入減少資料庫壓力
//   - 適合寫入密集場景
//
// 缺點：
//   - 資料可能遺失（快取崩潰時）
//   - 一致性最弱（資料庫有延遲）
//   - 實作複雜（需要持久化機制）
//
// 適用場景：
//   - 寫入非常頻繁
//   - 可容忍資料遺失（如統計資料）
//   - 對寫入效能要求極高
//
// 不適用場景：
//   - 不能容忍資料遺失
//   - 需要強一致性
//
// 改進方案：
//   1. 使用 AOF（Append-Only File）防止資料遺失
//   2. 使用 WAL（Write-Ahead Log）保證持久化
//   3. 定期同步到資料庫
type WriteBack struct {
	cache       cache.Cache
	store       DataStore
	dirtyKeys   map[string]interface{} // 髒資料：需要同步到資料庫
	mu          sync.Mutex
	flushTicker *time.Ticker // 定期刷新計時器
	stopCh      chan struct{}
}

// NewWriteBack 建立 Write-Back 策略。
//
// 參數：
//   cache: 快取
//   store: 資料庫
//   flushInterval: 刷新間隔（如 5 秒）
func NewWriteBack(c cache.Cache, store DataStore, flushInterval time.Duration) *WriteBack {
	wb := &WriteBack{
		cache:       c,
		store:       store,
		dirtyKeys:   make(map[string]interface{}),
		flushTicker: time.NewTicker(flushInterval),
		stopCh:      make(chan struct{}),
	}

	// 啟動背景刷新 goroutine
	go wb.flushLoop()

	return wb
}

// Get 讀取資料。
func (wb *WriteBack) Get(ctx context.Context, key string) (interface{}, error) {
	// 1. 查詢快取
	if value, ok := wb.cache.Get(key); ok {
		return value, nil
	}

	// 2. 快取未命中，查詢資料庫
	value, err := wb.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	// 3. 寫入快取
	wb.cache.Set(key, value)

	return value, nil
}

// Set 寫入資料。
//
// 執行流程：
//   1. 更新快取
//   2. 標記為髒資料
//   3. 返回（非同步寫入資料庫）
func (wb *WriteBack) Set(ctx context.Context, key string, value interface{}) error {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	// 1. 更新快取
	wb.cache.Set(key, value)

	// 2. 標記為髒資料
	wb.dirtyKeys[key] = value

	return nil
}

// Delete 刪除資料。
func (wb *WriteBack) Delete(ctx context.Context, key string) error {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	// 1. 刪除快取
	wb.cache.Delete(key)

	// 2. 從髒資料中移除
	delete(wb.dirtyKeys, key)

	// 3. 刪除資料庫（立即執行，避免資料不一致）
	if err := wb.store.Delete(ctx, key); err != nil {
		return err
	}

	return nil
}

// flushLoop 背景刷新循環。
//
// 執行流程：
//   1. 定期觸發（如每 5 秒）
//   2. 批量將髒資料寫入資料庫
//   3. 清空髒資料標記
//
// 錯誤處理：
//   寫入失敗時，保留髒資料標記，下次繼續重試
func (wb *WriteBack) flushLoop() {
	for {
		select {
		case <-wb.flushTicker.C:
			wb.flush()
		case <-wb.stopCh:
			// 關閉前最後一次刷新
			wb.flush()
			return
		}
	}
}

// flush 將髒資料刷新到資料庫。
//
// 修復 map 迭代問題：
//   問題：在 range 迭代時刪除 map 元素可能導致未定義行為
//   方案：先收集成功的 key，迭代結束後統一刪除
//
// 已知限制（教學簡化）：
//   - 使用 context.Background()，無法取消/超時
//   - 生產環境應使用 context.WithTimeout(ctx, 5*time.Second)
func (wb *WriteBack) flush() {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	if len(wb.dirtyKeys) == 0 {
		return
	}

	// 批量寫入資料庫
	ctx := context.Background()
	var keysToDelete []string

	for key, value := range wb.dirtyKeys {
		if err := wb.store.Set(ctx, key, value); err != nil {
			// 寫入失敗，保留髒資料標記
			// TODO: 記錄日誌、告警
			continue
		}

		// 寫入成功，記錄待刪除的 key
		keysToDelete = append(keysToDelete, key)
	}

	// 統一刪除已成功同步的髒資料標記
	for _, key := range keysToDelete {
		delete(wb.dirtyKeys, key)
	}
}

// Flush 手動觸發刷新（用於關閉前）。
func (wb *WriteBack) Flush() {
	wb.flush()
}

// Stop 停止背景刷新。
func (wb *WriteBack) Stop() {
	close(wb.stopCh)
	wb.flushTicker.Stop()
}

// DirtyCount 返回髒資料數量（用於監控）。
func (wb *WriteBack) DirtyCount() int {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	return len(wb.dirtyKeys)
}
