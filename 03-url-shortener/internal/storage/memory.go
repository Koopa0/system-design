// Package storage 實現各種存儲後端
//
// 存儲架構演進：
//   V1：Memory（單機、開發測試）
//   V2：PostgreSQL（持久化、生產環境）
//   V3：PostgreSQL + Redis（快取加速）
//   V4：分片 PostgreSQL + Redis Cluster（水平擴展）
package storage

import (
	"context"
	"sync"

	"github.com/koopa0/system-design/03-url-shortener/internal/shortener"
)

// Memory 內存存儲實現（V1 架構）
//
// 使用場景：
//   - 開發環境快速測試
//   - 單元測試（隔離外部依賴）
//   - 演示系統設計概念
//
// 系統設計權衡：
//   ✅ 優點：
//      - 零延遲（無網絡開銷）
//      - 零依賴（不需要資料庫）
//      - 簡單直觀
//
//   ❌ 缺點：
//      - 不持久化（重啟丟失）
//      - 無法分布式（單機限制）
//      - 內存受限（無法支撐大規模）
//
// 何時使用：
//   - 開發階段：快速驗證邏輯
//   - 單元測試：不需要 Mock
//   - 演示/教學：聚焦系統設計
type Memory struct {
	mu   sync.RWMutex
	urls map[string]*shortener.URL
}

// NewMemory 創建內存存儲實例
func NewMemory() *Memory {
	return &Memory{
		urls: make(map[string]*shortener.URL),
	}
}

// Save 保存短網址
func (m *Memory) Save(ctx context.Context, url *shortener.URL) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 檢查短碼是否已存在
	if _, exists := m.urls[url.ShortCode]; exists {
		return shortener.ErrCodeExists
	}

	m.urls[url.ShortCode] = url
	return nil
}

// Load 加載短網址
//
// 系統設計考量：
//   - 並發安全：使用 RLock（允許多個讀者並發）
//   - 數據隔離：返回副本而非指針（防止外部修改）
//     → 為什麼返回副本？
//     → 防止調用者直接修改 Clicks 等字段（繞過 IncrementClicks）
//     → 避免數據競爭（data race）
func (m *Memory) Load(ctx context.Context, shortCode string) (*shortener.URL, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	url, exists := m.urls[shortCode]
	if !exists {
		return nil, shortener.ErrNotFound
	}

	if url.IsExpired() {
		return nil, shortener.ErrExpired
	}

	// 返回副本，防止外部修改
	urlCopy := *url
	return &urlCopy, nil
}

// IncrementClicks 增加點擊計數
func (m *Memory) IncrementClicks(ctx context.Context, shortCode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	url, exists := m.urls[shortCode]
	if !exists {
		return shortener.ErrNotFound
	}

	url.Clicks++
	return nil
}
