package repository

import (
	"fmt"
	"time"

	"github.com/addp/common/cleanup"
	"github.com/addp/common/schema"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/common/logger"
	"github.com/addp/meta/internal/config"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

var DB *gorm.DB

// InitDatabase 初始化数据库连接
func InitDatabase(cfg *config.Config, retireCollectionApproval func(*gorm.DB) error) (*gorm.DB, error) {
	if cfg.DBSchema != "meta" {
		return nil, fmt.Errorf("meta module schema must be meta")
	}

	dbConfig := database.Config{
		Host:     cfg.DBHost,
		Port:     cfg.DBPort,
		User:     cfg.DBUser,
		Password: cfg.DBPassword,
		DBName:   cfg.DBName,
		Schema:   cfg.DBSchema,
		SSLMode:  "disable",
	}

	db, err := bootstrap.OpenRuntimeDatabase(dbConfig, "meta", "ADDP_MYSQL_META", &gorm.Config{Logger: gormLogger.Default.LogMode(gormLogger.Info)})
	if err != nil {
		return nil, err
	}
	if db.Dialector.Name() == "mysql" {
		err = RequireMySQLRuntime(db)
	} else {
		err = migratePostgres(db, retireCollectionApproval)
	}
	if err != nil {
		if pool, openErr := db.DB(); openErr == nil {
			_ = pool.Close()
		}
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

func migratePostgres(db *gorm.DB, retireCollectionApproval func(*gorm.DB) error) error {
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	return schema.Migrate(db, "meta", SchemaVersion, func(tx *gorm.DB) error {
		if err := tx.AutoMigrate(&models.MetaNode{}, &models.MetaItem{}, &models.ScanTask{}, &models.ObjectIndexDeletion{}); err != nil {
			return err
		}
		if err := applySQLMigrations(tx); err != nil {
			return err
		}
		if retireCollectionApproval == nil {
			return fmt.Errorf("collection approval retirement migration is required")
		}
		if err := retireCollectionApproval(tx); err != nil {
			return err
		}
		return cleanup.MigrateResults(tx, "meta")
	})

}
