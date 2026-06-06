package repository

import (
	"fmt"
	"time"

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
	if err := PrepareSchema(cfg); err != nil {
		return nil, err
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

	// Initialize database with auto-migration
	db, err := commonRepo.InitDatabase(dbConfig,
		&models.MetaNode{}, // 元数据节点（schema/prefix）
		&models.MetaItem{}, // 元数据条目（table/object）
		&models.ScanTask{}, // 扫描任务定义
	)
	if err != nil {
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

	if err := applySQLMigrations(db); err != nil {
		return nil, err
	}

	// 运行数据库约束迁移（用于新部署）
	// 注意：这些约束 GORM AutoMigrate 无法创建，需要手动执行 SQL
	if err := applyDatabaseConstraints(db); err != nil {
		logger.L().Warn("数据库约束应用失败（可能已存在）", "error", err)
		// 不返回错误，因为约束可能已存在
	}

	DB = db
	logger.L().Info("数据库连接成功", "host", cfg.DBHost, "schema", cfg.DBSchema)
	return db, nil
}

// Note: autoMigrate function removed - now handled by commonRepo.InitDatabase
