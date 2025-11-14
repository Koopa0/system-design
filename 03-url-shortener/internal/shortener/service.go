// Package shortener 實現 URL 短網址服務的核心業務邏輯
//
// 系統設計要點：
//
//	1. ID 生成策略：Snowflake ID + Base62 編碼
//	   - 為什麼？分布式、無協調、趨勢遞增
//
//	2. 存儲架構：Redis（快取）+ PostgreSQL（持久化）
//	   - 為什麼？讀多寫少（100:1），快取可提升 80% 效能
//
//	3. 一致性策略：最終一致性
//	   - 寫入時：直接寫 PostgreSQL，不寫 Redis（避免雙寫不一致）
//	   - 讀取時：先查 Redis，未命中再查 PostgreSQL
//
// 這個包展示的系統設計概念：
//   - Cache-Aside 模式
//   - 讀寫分離
//   - 分布式 ID 生成
package shortener

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/koopa0/system-design/03-url-shortener/pkg/base62"
	"github.com/koopa0/system-design/03-url-shortener/pkg/snowflake"
)

// 常見錯誤定義（Go 慣用法）
var (
	ErrInvalidURL  = errors.New("invalid url")
	ErrCodeExists  = errors.New("short code already exists")
	ErrNotFound    = errors.New("short code not found")
	ErrExpired     = errors.New("url expired")
)

// Store 定義存儲層接口
//
// 設計考量：
//   - 使用接口而非具體實現
//   - 方便單元測試（可以 mock）
//   - 支持替換不同的存儲實現
//
// Go 慣用法：
//   - interface 命名簡潔（Store 而非 Storage/IStorage）
//   - 方法名動詞優先（Save/Load/Increment）
type Store interface {
	// Save 保存 URL 映射
	Save(ctx context.Context, url *URL) error

	// Load 根據短碼加載 URL
	//
	// 查詢邏輯：
	//   1. 先查 Redis 快取（O(1)，< 1ms）
	//   2. 未命中則查 PostgreSQL（< 10ms）
	//   3. 成功後寫入 Redis
	Load(ctx context.Context, shortCode string) (*URL, error)

	// Increment 增加點擊計數
	Increment(ctx context.Context, shortCode string) error
}

// URL 表示一個短網址映射
type URL struct {
	ID        int64      `json:"id"`
	ShortCode string     `json:"short_code"`
	LongURL   string     `json:"long_url"`
	Custom    bool       `json:"custom"`              // 是否自定義
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // nil 表示永不過期
	Clicks    int64      `json:"clicks"`
}

// Expired 檢查是否過期
func (u *URL) Expired() bool {
	return u.ExpiresAt != nil && time.Now().After(*u.ExpiresAt)
}

// Service 短網址服務
//
// Go 慣用法：
//   - 使用組合（而非繼承）
//   - 依賴注入（通過構造函數）
//   - 簡單的結構體，不需要複雜的設計模式
type Service struct {
	store Store
	idgen *snowflake.Generator
}

// New 創建服務實例
//
// Go 慣用法：
//   - 構造函數命名為 New 或 NewService
//   - 返回具體類型（而非接口）
//   - 錯誤處理使用 error 返回值
func New(store Store, machineID int64) (*Service, error) {
	idgen, err := snowflake.NewGenerator(machineID)
	if err != nil {
		return nil, fmt.Errorf("snowflake generator: %w", err)
	}

	return &Service{
		store: store,
		idgen: idgen,
	}, nil
}

// Shorten 創建短網址
//
// 核心流程：
//  1. 驗證輸入
//  2. 生成短碼（Snowflake ID + Base62）
//  3. 保存到資料庫
//
// 設計決策：
//  - 為什麼不預先寫 Redis？
//    → 避免雙寫不一致。讀取時按需快取（Cache-Aside）
//
//  - 為什麼用 Snowflake 而非自增 ID？
//    → 分布式、無協調、趨勢遞增
//
// 參數說明：
//   - longURL: 原始網址
//   - customCode: 自定義短碼（空字符串表示自動生成）
//   - expiresAt: 過期時間（nil 表示永不過期）
func (s *Service) Shorten(ctx context.Context, longURL string, customCode string, expiresAt *time.Time) (*URL, error) {
	// 驗證 URL
	if !validURL(longURL) {
		return nil, ErrInvalidURL
	}

	// 生成短碼
	var code string
	var custom bool

	if customCode != "" {
		// 使用自定義短碼
		code = customCode
		custom = true
	} else {
		// 自動生成：Snowflake ID → Base62
		id, err := s.idgen.Generate()
		if err != nil {
			return nil, fmt.Errorf("generate id: %w", err)
		}
		code = base62.Encode(uint64(id))
	}

	// 創建 URL 對象
	url := &URL{
		ShortCode: code,
		LongURL:   longURL,
		Custom:    custom,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
		Clicks:    0,
	}

	// 保存
	if err := s.store.Save(ctx, url); err != nil {
		return nil, fmt.Errorf("save: %w", err)
	}

	return url, nil
}

// Resolve 解析短碼為長網址
//
// 核心流程：
//  1. 從 Store 加載（Store 會先查快取）
//  2. 檢查過期
//  3. 異步增加點擊數
//
// 設計考量：
//  - 重定向是高頻操作（讀寫比 100:1）
//  - 必須快（目標 P99 < 100ms）
//  - 點擊統計可異步（最終一致性）
func (s *Service) Resolve(ctx context.Context, shortCode string) (string, error) {
	// 加載 URL
	url, err := s.store.Load(ctx, shortCode)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("load: %w", err)
	}

	// 檢查過期
	if url.Expired() {
		return "", ErrExpired
	}

	// 異步增加點擊數
	//
	// 為什麼異步？
	//   - 不阻塞重定向（用戶體驗優先）
	//   - 點擊數允許短暫不準確
	//   - 降低延遲
	go func() {
		// 使用新 context（不受原請求取消影響）
		ctx := context.Background()
		_ = s.store.Increment(ctx, shortCode)
		// 錯誤忽略（可記錄日誌）
	}()

	return url.LongURL, nil
}

// Stats 獲取統計信息
func (s *Service) Stats(ctx context.Context, shortCode string) (*URL, error) {
	url, err := s.store.Load(ctx, shortCode)
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	return url, nil
}

// validURL 驗證 URL 格式
//
// 簡化實現（教學專案聚焦系統設計）：
//   - 檢查不為空
//   - 檢查前綴（http:// 或 https://）
//
// 生產環境應使用 url.Parse 並嚴格檢查
func validURL(s string) bool {
	if len(s) < 10 {
		return false
	}
	return s[:7] == "http://" || (len(s) > 8 && s[:8] == "https://")
}
