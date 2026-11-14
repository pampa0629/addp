package repository

import (
	"fmt"
	"log"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/common/schema"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenDatabase opens a connection without DDL; safe for Worker startup.
func OpenDatabase(cfg database.Config) (*gorm.DB, error) {
	return bootstrap.OpenDatabase(cfg, &gorm.Config{Logger: logger.Default.LogMode(logger.Info)})
}

// InitDatabase is the Backend initialization entry; Workers use OpenDatabase.
func InitDatabase(cfg database.Config, models ...interface{}) (*gorm.DB, error) {
	db, err := OpenDatabase(cfg)
	if err != nil {
		return nil, err
	}
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return nil, err
	}
	if cfg.Schema != "" {
		if err := schema.EnsureNamespace(db, cfg.Schema); err != nil {
			return nil, err
		}
	}
	// Auto-migrate models if provided
	if len(models) > 0 {
		log.Printf("[Migration] 🚀 Starting AutoMigrate for %d models in schema '%s'", len(models), cfg.Schema)

		// Migrate each model individually for better error reporting
		for i, model := range models {
			modelName := fmt.Sprintf("%T", model)
			log.Printf("[Migration]    ▶ [%d/%d] Migrating: %s", i+1, len(models), modelName)

			if err := db.AutoMigrate(model); err != nil {
				return nil, fmt.Errorf("failed to auto-migrate %s: %w", modelName, err)
			}
		}

		log.Printf("[Migration] ✅ AutoMigrate completed successfully for schema '%s'", cfg.Schema)
	}

	return db, nil
}

// InitDatabaseWithConfig is a convenience function that accepts a struct with config fields
// This allows for easier integration with existing config structs
func InitDatabaseWithConfig(cfg interface {
	GetDBHost() string
	GetDBPort() string
	GetDBUser() string
	GetDBPassword() string
	GetDBName() string
	GetDBSchema() string
}, models ...interface{}) (*gorm.DB, error) {
	dbConfig := database.Config{
		Host:     cfg.GetDBHost(),
		Port:     cfg.GetDBPort(),
		User:     cfg.GetDBUser(),
		Password: cfg.GetDBPassword(),
		DBName:   cfg.GetDBName(),
		Schema:   cfg.GetDBSchema(),
		SSLMode:  "disable",
	}

	return InitDatabase(dbConfig, models...)
}
