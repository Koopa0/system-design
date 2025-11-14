package cache

// Cache 是快取介面，定義了基本的快取操作。
//
// 此介面由以下實作：
//   - LRU（Least Recently Used）
//   - LFU（Least Frequently Used）
//   - DistributedCache（分散式快取）
//
// 設計考量：
//   - 簡單介面：只包含核心操作（Get/Set/Delete）
//   - 無 context：本地快取不需要上下文（與遠端快取/資料庫區分）
//   - 無 error：記憶體操作通常不會失敗
//
// 與資料儲存介面的區別：
//   Cache     - 記憶體快取（快速、無錯誤）
//   DataStore - 持久化儲存（較慢、可能失敗）
type Cache interface {
	// Get 獲取快取值
	//
	// 返回：
	//   - value: 快取的值
	//   - ok: true 表示命中，false 表示未命中
	Get(key string) (value interface{}, ok bool)

	// Set 設定快取值
	//
	// 注意：如果快取已滿，根據驅逐策略移除舊資料
	Set(key string, value interface{})

	// Delete 刪除快取值
	//
	// 注意：刪除不存在的 key 不會報錯（冪等操作）
	Delete(key string)

	// Len 返回快取中的項目數量
	//
	// 注意：用於監控和統計，不保證強一致性
	Len() int
}
