package repository

import (
	"fmt"
	"log"

	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	// search_path 设置为 manager,metadata,system 让Manager可以访问三个schema
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s,metadata,system",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSchema,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	// 确保连接后search_path正确
	db.Exec(fmt.Sprintf("SET search_path TO %s,metadata,system", cfg.DBSchema))

	// 自动迁移 (不再包括DataSource,使用system.resources)
	if err := db.AutoMigrate(
		&models.Directory{},
		&models.ManagedTable{},
		&models.ManagedFile{},
	); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	log.Println("Database connected and migrated successfully")
	return db, nil
}