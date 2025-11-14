// Package strategy 實作多種快取策略。
//
// 常見快取策略：
//   1. Cache-Aside（旁路快取）- 最常用
//   2. Read-Through（讀穿透）
//   3. Write-Through（寫穿透）
//   4. Write-Back（寫回）
//
// 選擇策略的考量：
//   - 一致性要求
//   - 效能要求
//   - 實作複雜度
package strategy

import (
	"context"
	"errors"
)

// DataStore 是資料儲存介面（如資料庫）。
type DataStore interface {
	Get(ctx context.Context, key string) (interface{}, error)
	Set(ctx context.Context, key string, value interface{}) error
	Delete(ctx context.Context, key string) error
}

// Cache 是快取介面。
type Cache interface {
	Get(key string) (interface{}, bool)
	Set(key string, value interface{})
	Delete(key string)
}

var (
	ErrKeyNotFound = errors.New("key not found")
)

// CacheAside 實作 Cache-Aside 策略（旁路快取）。
//
// 策略說明：
//   應用程式直接操作快取，資料庫由應用程式負責同步
//
// 讀取流程：
//   1. 查詢快取
//   2. 快取命中：返回資料
//   3. 快取未命中：查詢資料庫 → 寫入快取 → 返回資料
//
// 寫入流程（兩種方案）：
//   方案 A：先刪除快取，再更新資料庫（推薦）
//   方案 B：先更新資料庫，再刪除快取
//
// 為何是「刪除」而非「更新」快取？
//   1. 避免併發問題：
//      更新：多個執行緒同時更新可能導致資料不一致
//      刪除：下次讀取時重新載入，保證資料一致
//   2. 延遲載入：
//      可能更新的資料不會被讀取，避免無效快取
//
// 優點：
//   - 簡單易懂，容易實作
//   - 應用程式完全控制快取邏輯
//   - 最常用的策略（Redis 預設用法）
//
// 缺點：
//   - 需要應用程式處理快取邏輯
//   - 快取未命中時延遲較高
//   - 可能出現短暫的資料不一致
//
// 一致性問題：
//   方案 A（先刪除快取，再更新資料庫）：
//     問題：更新資料庫失敗時，快取已被刪除
//     影響：下次讀取會從資料庫載入舊資料
//     嚴重程度：低（下次更新會修正）
//
//   方案 B（先更新資料庫，再刪除快取）：
//     問題：刪除快取失敗時，快取中是舊資料
//     影響：持續讀取到舊資料，直到快取過期
//     嚴重程度：高（需要重試機制）
//
// 推薦：方案 A + 重試機制
type CacheAside struct {
	cache Cache
	store DataStore
}

// NewCacheAside 建立 Cache-Aside 策略。
func NewCacheAside(cache Cache, store DataStore) *CacheAside {
	return &CacheAside{
		cache: cache,
		store: store,
	}
}

// Get 讀取資料。
//
// 執行流程：
//   1. 查詢快取
//   2. 快取命中：直接返回
//   3. 快取未命中：
//      a. 查詢資料庫
//      b. 寫入快取
//      c. 返回資料
//
// 併發問題：
//   多個執行緒同時快取未命中時，可能多次查詢資料庫
//   解決：使用 singleflight 防止快取擊穿
func (ca *CacheAside) Get(ctx context.Context, key string) (interface{}, error) {
	// 1. 查詢快取
	if value, ok := ca.cache.Get(key); ok {
		return value, nil
	}

	// 2. 快取未命中，查詢資料庫
	value, err := ca.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	// 3. 寫入快取
	ca.cache.Set(key, value)

	return value, nil
}

// Set 寫入資料。
//
// 執行流程（方案 A）：
//   1. 刪除快取
//   2. 更新資料庫
//
// 為何先刪除快取？
//   避免：更新資料庫失敗但快取已更新的情況
//   結果：資料庫失敗時，快取已刪除，下次讀取會重新載入
//
// 改進：
//   可加入重試機制，確保資料庫更新成功
func (ca *CacheAside) Set(ctx context.Context, key string, value interface{}) error {
	// 1. 刪除快取
	ca.cache.Delete(key)

	// 2. 更新資料庫
	if err := ca.store.Set(ctx, key, value); err != nil {
		return err
	}

	return nil
}

// Delete 刪除資料。
//
// 執行流程：
//   1. 刪除快取
//   2. 刪除資料庫
func (ca *CacheAside) Delete(ctx context.Context, key string) error {
	// 1. 刪除快取
	ca.cache.Delete(key)

	// 2. 刪除資料庫
	if err := ca.store.Delete(ctx, key); err != nil {
		return err
	}

	return nil
}

// SetWithCache 寫入資料並更新快取（方案 B）。
//
// 執行流程：
//   1. 更新資料庫
//   2. 刪除快取
//
// 風險：
//   刪除快取失敗時，快取中是舊資料
//   建議：加入重試機制或使用訊息佇列保證刪除
func (ca *CacheAside) SetWithCache(ctx context.Context, key string, value interface{}) error {
	// 1. 更新資料庫
	if err := ca.store.Set(ctx, key, value); err != nil {
		return err
	}

	// 2. 刪除快取（允許失敗，下次讀取時會重新載入）
	ca.cache.Delete(key)

	return nil
}
