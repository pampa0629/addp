package repository

import (
	"fmt"
	"log"

	commonExecution "github.com/addp/common/execution"
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

	if err := commonExecution.EnsureStore(db); err != nil {
		return nil, fmt.Errorf("failed to ensure execution store: %w", err)
	}

	// AutoMigrate - 确保表结构最新
	// 所有表由 GORM AutoMigrate 管理（符合统一的数据库表创建策略）
	if err := db.AutoMigrate(
		&models.DevTask{}, // 开发任务（query、workflow、script）
	); err != nil {
		return nil, fmt.Errorf("failed to auto migrate: %w", err)
	}

	if err := db.Exec("DROP TABLE IF EXISTS develop.dev_items").Error; err != nil {
		return nil, fmt.Errorf("failed to drop legacy develop.dev_items: %w", err)
	}

	log.Println("✅ Database connected successfully (AutoMigrate 完成)")

	return db, nil
}
