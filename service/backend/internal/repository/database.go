package repository

import (
	"fmt"
	"log"

	"github.com/addp/service/internal/config"
	"github.com/addp/service/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitDatabase 初始化数据库连接
func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 创建 schema
	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", cfg.DBSchema)).Error; err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	// 自动迁移表结构
	if err := AutoMigrate(db); err != nil {
		return nil, err
	}

	log.Printf("✅ Database initialized successfully (schema: %s)", cfg.DBSchema)
	return db, nil
}

// AutoMigrate 自动迁移所有表结构
func AutoMigrate(db *gorm.DB) error {
	migrateModels := []interface{}{
		&models.ExternalService{},
		&models.ServiceLayer{},
		&models.InternalService{},
		&models.InternalServiceLayer{},
	}

	for _, model := range migrateModels {
		if err := db.AutoMigrate(model); err != nil {
			return fmt.Errorf("failed to migrate %T: %w", model, err)
		}
	}

	log.Println("✅ Database migration completed")
	return nil
}
