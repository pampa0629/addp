package repository

import (
	"fmt"
	"log"

	"github.com/addp/develop/backend/internal/config"
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitDatabase 初始化数据库连接
func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		cfg.PostgresHost,
		cfg.PostgresPort,
		cfg.PostgresUser,
		cfg.PostgresPassword,
		cfg.PostgresDB,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 设置默认 schema
	if err := db.Exec(fmt.Sprintf("SET search_path TO %s, public", cfg.DBSchema)).Error; err != nil {
		return nil, fmt.Errorf("failed to set search_path: %w", err)
	}

	// AutoMigrate - 确保表结构最新
	// 注意: dev_items 和 dev_executions 已通过迁移脚本创建，无需 AutoMigrate
	if err := db.AutoMigrate(
		&models.Script{},
		&models.ScriptVersion{},
		&models.ScriptDependency{},
		// Phase 1: dev_items 和 dev_executions 由 SQL 迁移脚本管理
		// &models.DevItem{},
		// &models.DevExecution{},
	); err != nil {
		return nil, fmt.Errorf("failed to auto migrate: %w", err)
	}

	log.Println("✅ Database connected successfully (Phase 1: 使用 SQL 迁移脚本)")

	return db, nil
}
