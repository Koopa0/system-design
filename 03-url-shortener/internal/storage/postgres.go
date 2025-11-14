package storage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/koopa0/system-design/03-url-shortener/internal/shortener"
)

// Postgres PostgreSQL 存儲實現
//
// 系統設計考量：
//
//  1. 表結構設計：
//     - id：主鍵（Snowflake ID）
//     - short_code：唯一索引（查詢優化）
//     - long_url：原始 URL
//     - clicks：點擊計數
//     - created_at：創建時間
//     - expires_at：過期時間（可選）
//
//  2. 索引策略：
//     - PRIMARY KEY (id)：聚簇索引
//     - UNIQUE INDEX (short_code)：查詢加速
//     - INDEX (created_at)：時間範圍查詢
//
//  3. 併發控制：
//     - short_code UNIQUE 約束：防止重複
//     - UPDATE ... SET clicks = clicks + 1：原子操作
//
// 表結構 SQL：
//
//	CREATE TABLE urls (
//	  id         BIGINT PRIMARY KEY,
//	  short_code VARCHAR(20) UNIQUE NOT NULL,
//	  long_url   TEXT NOT NULL,
//	  clicks     BIGINT DEFAULT 0,
//	  created_at TIMESTAMP NOT NULL,
//	  expires_at TIMESTAMP
//	);
//
//	CREATE INDEX idx_short_code ON urls(short_code);
//	CREATE INDEX idx_created_at ON urls(created_at);
type Postgres struct {
	db *sql.DB
}

// NewPostgres 創建 PostgreSQL 存儲實例
//
// 參數：
//   - db：資料庫連接（由調用方管理生命週期）
//
// 連接池配置（調用方負責）：
//
//	db.SetMaxOpenConns(25)        // 最大連接數
//	db.SetMaxIdleConns(5)         // 最大空閒連接
//	db.SetConnMaxLifetime(5*time.Minute)
func NewPostgres(db *sql.DB) *Postgres {
	return &Postgres{db: db}
}

// Save 保存短網址
//
// SQL：INSERT INTO urls (...) VALUES (...)
//
// 錯誤處理：
//   - UNIQUE 約束衝突 → ErrCodeExists
func (p *Postgres) Save(ctx context.Context, url *shortener.URL) error {
	query := `
		INSERT INTO urls (id, short_code, long_url, clicks, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := p.db.ExecContext(ctx, query,
		url.ID,
		url.ShortCode,
		url.LongURL,
		url.Clicks,
		url.CreatedAt,
		url.ExpiresAt, // nullable
	)

	if err != nil {
		// 檢查是否為 UNIQUE 約束衝突
		//
		// PostgreSQL 錯誤碼：
		//   - 23505: unique_violation
		//
		// 這裡簡化處理：檢查錯誤信息
		// 生產環境應使用 pq.Error 檢查錯誤碼
		if isDuplicateKeyError(err) {
			return shortener.ErrCodeExists
		}
		return err
	}

	return nil
}

// Load 加載短網址
//
// SQL：SELECT * FROM urls WHERE short_code = $1
//
// 錯誤處理：
//   - sql.ErrNoRows → ErrNotFound
//   - 過期檢查在業務層（resolve.go）
func (p *Postgres) Load(ctx context.Context, shortCode string) (*shortener.URL, error) {
	query := `
		SELECT id, short_code, long_url, clicks, created_at, expires_at
		FROM urls
		WHERE short_code = $1
	`

	var url shortener.URL
	var expiresAt sql.NullTime // 處理 NULL 值

	err := p.db.QueryRowContext(ctx, query, shortCode).Scan(
		&url.ID,
		&url.ShortCode,
		&url.LongURL,
		&url.Clicks,
		&url.CreatedAt,
		&expiresAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, shortener.ErrNotFound
		}
		return nil, err
	}

	// 處理 expires_at（可能為 NULL）
	if expiresAt.Valid {
		url.ExpiresAt = &expiresAt.Time
	}

	return &url, nil
}

// IncrementClicks 增加點擊計數
//
// SQL：UPDATE urls SET clicks = clicks + 1 WHERE short_code = $1
//
// 系統設計考量：
//   - 原子操作：clicks = clicks + 1（資料庫保證）
//   - 性能優化：僅更新一個字段
//   - 返回值檢查：如果 RowsAffected = 0，說明短碼不存在
func (p *Postgres) IncrementClicks(ctx context.Context, shortCode string) error {
	query := `
		UPDATE urls
		SET clicks = clicks + 1
		WHERE short_code = $1
	`

	result, err := p.db.ExecContext(ctx, query, shortCode)
	if err != nil {
		return err
	}

	// 檢查是否有行被更新
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return shortener.ErrNotFound
	}

	return nil
}

// isDuplicateKeyError 檢查是否為重複鍵錯誤
//
// 簡化實現：檢查錯誤信息
//
// 生產環境應該：
//
//	import "github.com/lib/pq"
//	if pqErr, ok := err.(*pq.Error); ok {
//	    return pqErr.Code == "23505" // unique_violation
//	}
func isDuplicateKeyError(err error) bool {
	// 簡化實現：檢查錯誤信息是否包含 "duplicate" 或 "unique"
	errMsg := err.Error()
	return contains(errMsg, "duplicate") || contains(errMsg, "unique")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			containsInner(s, substr)))
}

func containsInner(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// CreateTable 創建資料庫表（初始化用）
//
// 僅在開發環境使用，生產環境應使用遷移工具（如 migrate）
func (p *Postgres) CreateTable(ctx context.Context) error {
	query := `
		CREATE TABLE IF NOT EXISTS urls (
			id         BIGINT PRIMARY KEY,
			short_code VARCHAR(20) UNIQUE NOT NULL,
			long_url   TEXT NOT NULL,
			clicks     BIGINT DEFAULT 0,
			created_at TIMESTAMP NOT NULL,
			expires_at TIMESTAMP
		);

		CREATE INDEX IF NOT EXISTS idx_short_code ON urls(short_code);
		CREATE INDEX IF NOT EXISTS idx_created_at ON urls(created_at);
	`

	_, err := p.db.ExecContext(ctx, query)
	return err
}
