package repository

import (
	"fmt"
	"log"

	"github.com/addp/common/cleanup"
	"github.com/addp/common/schema"

	"github.com/addp/graph/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.GetDatabaseDSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 确保 schema 存在
	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", cfg.DBSchema)).Error; err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return nil, fmt.Errorf("failed to ensure execution store: %w", err)
	}

	// 自动迁移
	if err := MigrateGraphTables(db); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	if err := cleanup.MigrateResults(db, "graph"); err != nil {
		return nil, err
	}
	log.Println("✅ Graph 数据库初始化完成")
	return db, nil
}
