package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/koopa0/system-design/03-url-shortener/internal/shortener"
)

// RedisCache Redis 快取層實現（V3 架構）
//
// 快取策略：Cache-Aside（旁路快取）
//   1. 讀取：
//      → 先查 Redis
//      → Cache Miss：查資料庫 + 寫入 Redis
//      → Cache Hit：直接返回
//
//   2. 寫入：
//      → 寫資料庫
//      → 更新/刪除 Redis（保持一致性）
//
// 系統設計考量：
//
// 1. TTL 設置：
//    - 熱點數據：1 小時（平衡命中率與一致性）
//    - 過期數據：立即失效（避免返回過期 URL）
//
// 2. 一致性問題：
//    - 問題：資料庫更新後，快取可能過時
//    - 解法：寫入時主動更新快取（Write-Through）
//    - 備選：寫入時刪除快取（Cache-Invalidation）
//
// 3. 熱點數據（Hot Key）：
//    - 問題：某個短碼瞬間大量訪問（如病毒式傳播）
//    - 現象：單個 Redis 節點過載
//    - 解法：
//      → 短期：本地快取（進程內 LRU）
//      → 長期：一致性哈希 + 多副本
//
// 4. 快取穿透（Cache Penetration）：
//    - 問題：查詢不存在的短碼，每次都穿透到 DB
//    - 解法：快取空結果（TTL 較短，如 1 分鐘）
//
// 5. 快取雪崩（Cache Avalanche）：
//    - 問題：大量快取同時過期，DB 瞬間壓力劇增
//    - 解法：TTL 加隨機值（如 1h ± 5min）
//
// 6. 快取擊穿（Cache Breakdown）：
//    - 問題：熱點數據過期，大量請求同時查 DB
//    - 解法：互斥鎖（只有一個請求查 DB）或永不過期
type RedisCache struct {
	client    RedisClient       // Redis 客戶端接口（便於測試）
	backend   shortener.Store   // 後端存儲（PostgreSQL）
	ttl       time.Duration     // 快取 TTL（默認 1 小時）
	keyPrefix string            // 鍵前綴（避免衝突）
}

// RedisClient Redis 客戶端接口
//
// 為什麼定義接口？
//   - 便於測試（可用 Mock 替代真實 Redis）
//   - 解耦具體實現（支持不同 Redis 庫）
//   - 簡化 API（只暴露需要的方法）
type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
	Del(ctx context.Context, key string) error
	Incr(ctx context.Context, key string) (int64, error)
}

// NewRedisCache 創建 Redis 快取層
//
// 參數：
//   - client：Redis 客戶端
//   - backend：後端存儲（通常是 PostgreSQL）
//   - ttl：快取過期時間（0 表示使用默認 1 小時）
func NewRedisCache(client RedisClient, backend shortener.Store, ttl time.Duration) *RedisCache {
	if ttl == 0 {
		ttl = time.Hour // 默認 1 小時
	}

	return &RedisCache{
		client:    client,
		backend:   backend,
		ttl:       ttl,
		keyPrefix: "url:",
	}
}

// Save 保存短網址
//
// 流程：
//  1. 寫入後端資料庫（主存儲）
//  2. 寫入 Redis（保持一致性）
//
// 一致性考量：
//   - 先寫 DB，再寫 Cache
//   - 如果 Cache 寫入失敗，不影響主流程（容忍短暫不一致）
func (r *RedisCache) Save(ctx context.Context, url *shortener.URL) error {
	// 1. 寫入後端存儲（PostgreSQL）
	if err := r.backend.Save(ctx, url); err != nil {
		return err
	}

	// 2. 寫入 Redis（失敗不影響主流程）
	key := r.keyPrefix + url.ShortCode
	data, _ := json.Marshal(url)
	_ = r.client.Set(ctx, key, string(data), r.ttl)

	return nil
}

// Load 加載短網址（Cache-Aside 模式）
//
// 流程：
//  1. 查詢 Redis
//  2. Cache Hit：直接返回
//  3. Cache Miss：查資料庫 → 寫入 Redis → 返回
//
// 性能優化：
//   - 熱點數據：< 1ms（Redis 內存訪問）
//   - 冷數據：< 50ms（資料庫查詢 + Redis 寫入）
//   - 命中率目標：> 95%（80/20 法則）
func (r *RedisCache) Load(ctx context.Context, shortCode string) (*shortener.URL, error) {
	key := r.keyPrefix + shortCode

	// 1. 查詢 Redis
	data, err := r.client.Get(ctx, key)
	if err == nil {
		// Cache Hit：檢查是否為空結果（快取穿透防護）
		//
		// 系統設計考量：
		//   - 為什麼快取 "null"？
		//     → 防止不存在的短碼重複查詢 DB（快取穿透）
		//     → 攻擊場景：惡意請求大量不存在的短碼
		//   - 為什麼 TTL 較短（1 分鐘）？
		//     → 如果短碼後來被創建，可以快速生效
		if data == "null" {
			return nil, shortener.ErrNotFound
		}

		// 解析 JSON 並返回
		var url shortener.URL
		if err := json.Unmarshal([]byte(data), &url); err == nil {
			// 檢查過期（即使在快取中也要檢查）
			if url.IsExpired() {
				// 過期：刪除快取
				_ = r.client.Del(ctx, key)
				return nil, shortener.ErrExpired
			}
			return &url, nil
		}
	}

	// 2. Cache Miss：查詢後端資料庫
	url, err := r.backend.Load(ctx, shortCode)
	if err != nil {
		// 快取穿透防護：短碼不存在時，也快取空結果（TTL 較短）
		if err == shortener.ErrNotFound {
			_ = r.client.Set(ctx, key, "null", time.Minute)
		}
		return nil, err
	}

	// 3. 寫入 Redis（異步，不阻塞返回）
	go func() {
		data, _ := json.Marshal(url)
		_ = r.client.Set(context.Background(), key, string(data), r.ttl)
	}()

	return url, nil
}

// IncrementClicks 增加點擊計數
//
// 優化策略：
//  1. 直接在 Redis 中遞增（INCR）
//  2. 異步批量同步到資料庫（降低 DB 壓力）
//
// 設計問題：
//   Q: Redis 計數和 DB 計數不一致怎麼辦？
//   A: 允許最終一致性，統計數據不要求絕對精確
//
//   Q: Redis 數據丟失怎麼辦？
//   A: 1) Redis 持久化（AOF/RDB）
//      2) 定期從 DB 恢復計數
//
// 當前實作：簡化版（直接調用後端）
// 生產環境：應使用消息隊列批量更新
func (r *RedisCache) IncrementClicks(ctx context.Context, shortCode string) error {
	// 簡化實作：直接調用後端
	// 生產環境優化：
	//   1. Redis INCR（快速）
	//   2. 定期批量同步到 DB（如每 10 秒）
	//   3. 使用消息隊列（NATS/NSQ）解耦
	return r.backend.IncrementClicks(ctx, shortCode)
}
