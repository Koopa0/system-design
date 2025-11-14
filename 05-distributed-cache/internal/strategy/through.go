package strategy

import "context"

// WriteThrough 實作 Write-Through 策略（寫穿透）。
//
// 策略說明：
//   寫入時同時更新快取和資料庫
//   快取層負責與資料庫同步
//
// 讀取流程：
//   1. 查詢快取
//   2. 快取命中：返回資料
//   3. 快取未命中：查詢資料庫 → 寫入快取 → 返回資料
//
// 寫入流程：
//   1. 更新資料庫
//   2. 更新快取
//   兩個操作要保證原子性或有重試機制
//
// 與 Cache-Aside 的差異：
//   Cache-Aside：應用程式負責同步
//   Write-Through：快取層負責同步
//
// 優點：
//   - 資料一致性好（同步更新）
//   - 讀取效能穩定（快取總是最新）
//   - 應用程式邏輯簡單（不需處理快取同步）
//
// 缺點：
//   - 寫入延遲高（需等待資料庫完成）
//   - 寫入頻繁時效能差
//   - 可能快取無效資料（寫入但不讀取）
//
// 適用場景：
//   - 讀多寫少
//   - 對一致性要求高
//   - 希望簡化應用程式邏輯
//
// 不適用場景：
//   - 寫入頻繁
//   - 對寫入延遲敏感
type WriteThrough struct {
	cache Cache
	store DataStore
}

// NewWriteThrough 建立 Write-Through 策略。
func NewWriteThrough(cache Cache, store DataStore) *WriteThrough {
	return &WriteThrough{
		cache: cache,
		store: store,
	}
}

// Get 讀取資料。
//
// 與 Cache-Aside 相同
func (wt *WriteThrough) Get(ctx context.Context, key string) (interface{}, error) {
	// 1. 查詢快取
	if value, ok := wt.cache.Get(key); ok {
		return value, nil
	}

	// 2. 快取未命中，查詢資料庫
	value, err := wt.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	// 3. 寫入快取
	wt.cache.Set(key, value)

	return value, nil
}

// Set 寫入資料。
//
// 執行流程：
//   1. 更新資料庫
//   2. 更新快取
//
// 一致性保證：
//   如果資料庫更新成功但快取更新失敗
//   下次讀取時會從資料庫重新載入
//
// 改進方案：
//   1. 使用事務保證原子性（如果快取支援）
//   2. 使用重試機制
//   3. 使用訊息佇列非同步更新快取
func (wt *WriteThrough) Set(ctx context.Context, key string, value interface{}) error {
	// 1. 更新資料庫
	if err := wt.store.Set(ctx, key, value); err != nil {
		return err
	}

	// 2. 更新快取
	wt.cache.Set(key, value)

	return nil
}

// Delete 刪除資料。
func (wt *WriteThrough) Delete(ctx context.Context, key string) error {
	// 1. 刪除資料庫
	if err := wt.store.Delete(ctx, key); err != nil {
		return err
	}

	// 2. 刪除快取
	wt.cache.Delete(key)

	return nil
}
