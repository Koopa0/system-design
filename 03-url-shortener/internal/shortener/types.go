// Package shortener 實現 URL 短網址的核心功能
package shortener

import (
	"errors"
	"time"
)

// URL 表示一個短網址記錄
//
// 數據模型設計：
//
//   - ID：分布式唯一標識（Snowflake）
//     → 全局唯一、趨勢遞增（有利於資料庫索引）
//     → 包含時間戳（可排序、可追溯）
//
//   - ShortCode：用戶訪問的短碼
//     → Base62 編碼（URL 安全，無需轉義）
//     → 長度 6-8 字符（平衡可讀性與容量）
//
//   - Clicks：點擊統計
//     → 設計問題：精確統計 vs 性能？
//     → 選擇：允許最終一致性（異步更新）
//
//   - ExpiresAt：過期機制
//     → 設計問題：主動刪除 vs 惰性刪除？
//     → 選擇：惰性刪除（訪問時檢查）+ 定期清理
type URL struct {
	ID        int64      `json:"id"`                   // Snowflake ID
	ShortCode string     `json:"short_code"`           // Base62 短碼（如 "8M0kX"）
	LongURL   string     `json:"long_url"`             // 原始 URL
	Clicks    int64      `json:"clicks"`               // 點擊次數
	CreatedAt time.Time  `json:"created_at"`           // 創建時間
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // 過期時間（可選）
}

// IsExpired 檢查 URL 是否已過期
func (u *URL) IsExpired() bool {
	if u.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*u.ExpiresAt)
}

// 錯誤定義
//
// HTTP 狀態碼映射：
//   - ErrInvalidURL   → 400 Bad Request
//   - ErrNotFound     → 404 Not Found
//   - ErrExpired      → 410 Gone（更精確的語義）
//   - ErrCodeExists   → 409 Conflict
//
// 設計考量：
//   - 區分不存在（404）和已過期（410）
//   - 客戶端可根據狀態碼做不同處理
var (
	// ErrInvalidURL 當 URL 格式無效時返回
	ErrInvalidURL = errors.New("invalid url format")

	// ErrNotFound 當短碼不存在時返回
	ErrNotFound = errors.New("short code not found")

	// ErrExpired 當 URL 已過期時返回
	ErrExpired = errors.New("url has expired")

	// ErrCodeExists 當自定義短碼已存在時返回
	ErrCodeExists = errors.New("custom short code already exists")
)
