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
	if err := db.AutoMigrate(
		&models.Execution{},
		&models.Script{},
		&models.ScriptVersion{},
		&models.ScriptDependency{},
		&models.SpatialTask{},
		&models.GISExecution{},
	); err != nil {
		return nil, fmt.Errorf("failed to auto migrate: %w", err)
	}

	log.Println("✅ Database connected and migrated successfully")

	return db, nil
}
