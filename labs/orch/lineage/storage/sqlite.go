package storage

import (
	"fmt"
	"lineage/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// NewSQLiteDB 创建 SQLite 数据库连接
func NewSQLiteDB(dbPath string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SQLite: %w", err)
	}

	// 自动迁移表结构
	if err := db.AutoMigrate(&models.DataLineage{}); err != nil {
		return nil, fmt.Errorf("failed to migrate SQLite schema: %w", err)
	}

	return db, nil
}
