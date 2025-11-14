package shortener

import (
	"context"
)

// Store 定義存儲接口
//
// 系統設計考量：
//
// 1. 存儲架構選擇：
//    - 主存儲：PostgreSQL（持久化、事務、SQL 查詢）
//    - 快取層：Redis（高性能讀取、TTL 支持）
//    - 設計模式：Cache-Aside（讀穿透、寫入時更新快取）
//
// 2. 讀寫模式：
//    - 讀多寫少（典型比例 100:1 或更高）
//    - 優化策略：快取熱點數據、讀寫分離
//
// 3. 數據一致性：
//    - 短網址創建：強一致性（必須立即可用）
//    - 點擊統計：最終一致性（允許延遲）
//
// 4. 擴展性考量：
//    - 水平擴展：資料庫分片（按 short_code 哈希）
//    - 快取擴展：Redis Cluster
//    - 讀擴展：主從複製（Replicas）
type Store interface {
	// Save 保存短網址
	//
	// 設計考量：
	//   - 冪等性：重複保存相同短碼應返回錯誤（ErrCodeExists）
	//   - 原子性：需要資料庫層面保證（UNIQUE 約束）
	//   - 性能：寫入頻率低，可接受稍高延遲（< 100ms）
	Save(ctx context.Context, url *URL) error

	// Load 加載短網址
	//
	// 設計考量：
	//   - 高頻操作：需要快取支持（目標：< 10ms）
	//   - 快取策略：Cache-Aside（先查快取，Miss 時查 DB）
	//   - 熱點數據：80/20 法則（20% 的短碼占 80% 的流量）
	//   - TTL 設置：快取 1 小時（平衡命中率與過期處理）
	Load(ctx context.Context, shortCode string) (*URL, error)

	// IncrementClicks 增加點擊計數
	//
	// 設計考量：
	//   - 性能優先：不能阻塞重定向（異步調用）
	//   - 一致性取捨：允許延遲、丟失（統計允許不精確）
	//   - 優化方案：
	//     → 短期：Redis INCR（快速原子操作）
	//     → 長期：消息隊列 + 批量更新（降低 DB 壓力）
	IncrementClicks(ctx context.Context, shortCode string) error
}
