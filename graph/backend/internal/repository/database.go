package repository

import (
	"fmt"
	"log"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/graph/internal/config"
	"github.com/addp/graph/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(cfg.GetDatabaseDSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 确保 schema 存在
	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", cfg.DBSchema)).Error; err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	if err := commonExecution.EnsureStore(db); err != nil {
		return nil, fmt.Errorf("failed to ensure execution store: %w", err)
	}

	// 自动迁移
	if err := db.AutoMigrate(
		&models.Ontology{},
		&models.EntityType{},
		&models.RelationType{},
		&models.OntologyVersion{},
		&models.KnowledgeGraph{},
		&models.BuildTask{},
		&models.BuildMaterial{},
		&models.ReviewItem{},
	); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	log.Println("✅ Graph 数据库初始化完成")
	return db, nil
}
