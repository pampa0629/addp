package repository

import (
	"fmt"
	"time"

	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/models"
	commonRepo "github.com/addp/common/repository"
	"gorm.io/gorm"
)

func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	// Use common repository InitDatabase
	// Note: Manager needs access to manager, metadata, and system schemas
	dbConfig := commonRepo.DatabaseConfig{
		Host:     cfg.DBHost,
		Port:     cfg.DBPort,
		User:     cfg.DBUser,
		Password: cfg.DBPassword,
		DBName:   cfg.DBName,
		Schema:   cfg.DBSchema,
		SSLMode:  "disable",
	}

	// Initialize database with auto-migration
	db, err := commonRepo.InitDatabase(dbConfig,
		&models.Directory{},
		&models.SearchHistory{},
		&models.ManagedTable{},
		&models.ManagedFile{},
	)
	if err != nil {
		return nil, err
	}

	// Configure connection pool for optimal performance
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Connection pool settings
	sqlDB.SetMaxOpenConns(100)                 // 最大打开连接数 (支持高并发MVT请求)
	sqlDB.SetMaxIdleConns(20)                  // 最大空闲连接数 (连接复用)
	sqlDB.SetConnMaxLifetime(10 * time.Minute) // 连接最大生命周期

	// Set search_path to allow access to manager, metadata, and system schemas
	db.Exec(fmt.Sprintf("SET search_path TO %s,metadata,system", cfg.DBSchema))

	return db, nil
}
