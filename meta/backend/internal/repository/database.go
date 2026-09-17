package repository

import (
	"fmt"
	"time"

	"github.com/addp/common/schema"

	"github.com/addp/common/logger"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

var DB *gorm.DB

// InitDatabase 初始化数据库连接
func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	if cfg.DBSchema != "meta" {
		return nil, fmt.Errorf("meta module schema must be meta")
	}

	// Use common repository InitDatabase
	dbConfig := commonRepo.DatabaseConfig{
		Host:     cfg.DBHost,
		Port:     cfg.DBPort,
		User:     cfg.DBUser,
		Password: cfg.DBPassword,
		DBName:   cfg.DBName,
		Schema:   cfg.DBSchema,
		SSLMode:  "disable",
	}

	db, err := commonRepo.OpenDatabase(dbConfig)
	if err != nil {
		return nil, err
	}
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return nil, err
	}
	if err := schema.Migrate(db, "meta", SchemaVersion, func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}, &models.ScanTask{}); err != nil {
			return err
		}
		return applySQLMigrations(tx)
	}); err != nil {
		return nil, err
	}

	// Apply custom settings for Meta module
	// Set connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %w", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)

	// Apply custom logger (optional, overwrites common logger)
	dbLogger := newGormLogger(logger.With("component", "gorm"), gormLogger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  gormLogger.Warn,
		IgnoreRecordNotFoundError: true,
	})
	db.Logger = dbLogger

	DB = db
	logger.L().Info("数据库连接成功", "host", cfg.DBHost, "schema", cfg.DBSchema)
	return db, nil
}

// Note: autoMigrate function removed - now handled by commonRepo.InitDatabase
