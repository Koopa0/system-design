// Package migrations 提供資料庫遷移功能
package migrations

import (
	"embed"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed all:migrations
var migrationsFS embed.FS

// Migrator 管理資料庫遷移
type Migrator struct {
	migrate *migrate.Migrate
	logger  *slog.Logger
}

// New 建立新的遷移管理器
func New(databaseURL string, logger *slog.Logger) (*Migrator, error) {
	// 建立嵌入檔案系統的源
	source, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("建立遷移源失敗: %w", err)
	}

	// 建立遷移實例
	m, err := migrate.NewWithSourceInstance("iofs", source, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("建立遷移實例失敗: %w", err)
	}

	return &Migrator{
		migrate: m,
		logger:  logger,
	}, nil
}

// Up 執行所有待處理的遷移
func (m *Migrator) Up() error {
	m.logger.Info("開始執行資料庫遷移")

	// 獲取當前版本
	version, dirty, err := m.migrate.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return fmt.Errorf("獲取當前版本失敗: %w", err)
	}

	if dirty {
		m.logger.Warn("資料庫處於髒狀態，嘗試修復", "version", version)
		// 確保版本號在有效範圍內
		const maxInt = int(^uint(0) >> 1)
		if version > uint(maxInt) {
			return fmt.Errorf("版本號超出範圍: %d", version)
		}
		if err := m.migrate.Force(int(version)); err != nil {
			return fmt.Errorf("修復髒狀態失敗: %w", err)
		}
	}

	// 執行遷移
	if err := m.migrate.Up(); err != nil {
		if err == migrate.ErrNoChange {
			m.logger.Info("資料庫已是最新版本")
			return nil
		}
		return fmt.Errorf("執行遷移失敗: %w", err)
	}

	// 獲取新版本
	newVersion, _, _ := m.migrate.Version()
	m.logger.Info("資料庫遷移成功", "new_version", newVersion)

	return nil
}

// Down 回滾一個版本
func (m *Migrator) Down() error {
	m.logger.Info("開始回滾資料庫")

	if err := m.migrate.Steps(-1); err != nil {
		if err == migrate.ErrNoChange {
			m.logger.Info("沒有可回滾的版本")
			return nil
		}
		return fmt.Errorf("回滾失敗: %w", err)
	}

	version, _, _ := m.migrate.Version()
	m.logger.Info("資料庫回滾成功", "current_version", version)

	return nil
}

// Reset 重置資料庫（危險操作）
func (m *Migrator) Reset() error {
	m.logger.Warn("開始重置資料庫")

	if err := m.migrate.Down(); err != nil {
		if err == migrate.ErrNoChange {
			m.logger.Info("資料庫已重置")
			return nil
		}
		return fmt.Errorf("重置失敗: %w", err)
	}

	m.logger.Info("資料庫重置成功")
	return nil
}

// Version 獲取當前版本
func (m *Migrator) Version() (uint, bool, error) {
	return m.migrate.Version()
}

// Close 關閉遷移管理器
func (m *Migrator) Close() error {
	sourceErr, dbErr := m.migrate.Close()
	if sourceErr != nil {
		return fmt.Errorf("關閉源失敗: %w", sourceErr)
	}
	if dbErr != nil {
		return fmt.Errorf("關閉資料庫連線失敗: %w", dbErr)
	}
	return nil
}
