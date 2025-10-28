package repository

import (
	"fmt"

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

	// Set search_path to allow access to manager, metadata, and system schemas
	db.Exec(fmt.Sprintf("SET search_path TO %s,metadata,system", cfg.DBSchema))

	return db, nil
}
