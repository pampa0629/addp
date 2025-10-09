package repository

import (
	"fmt"
	"log"

	"github.com/addp/meta/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// InitDatabase 初始化数据库连接
func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
		cfg.DBHost,
		cfg.DBPort,
		cfg.DBUser,
		cfg.DBPassword,
		cfg.DBName,
		cfg.DBSchema,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
		// 不使用 TablePrefix，直接通过 search_path 访问正确的 schema
	})

	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 设置连接池
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %w", err)
	}

	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)

	// 自动迁移 - 跳过 AutoMigrate，表已存在
	// if err := autoMigrate(db); err != nil {
	// 	return nil, fmt.Errorf("failed to auto migrate: %w", err)
	// }

	DB = db
	log.Println("Database connected successfully (migration skipped)")
	return db, nil
}

// autoMigrate 自动迁移所有表
func autoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		// &models.MetaResource{},
		// &models.MetaNode{},
		// &models.MetaItem{},
		// &models.ScanLog{},
	)
}
