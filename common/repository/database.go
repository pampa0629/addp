package repository

import (
	"fmt"
	"log"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DatabaseConfig holds database connection configuration
type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	Schema   string // PostgreSQL schema for multi-tenancy
	SSLMode  string // default: "disable"
}

// InitDatabase initializes GORM database connection with schema support
// models parameter should be a slice of model structs to auto-migrate
func InitDatabase(cfg DatabaseConfig, models ...interface{}) (*gorm.DB, error) {
	if cfg.SSLMode == "" {
		cfg.SSLMode = "disable"
	}

	// Build DSN
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode,
	)

	// Add search_path if schema is specified
	if cfg.Schema != "" {
		dsn += fmt.Sprintf(" search_path=%s", cfg.Schema)
	}

	// Connect to database
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	log.Printf("✅ Connected to database: %s@%s:%s/%s (schema: %s)",
		cfg.User, cfg.Host, cfg.Port, cfg.DBName, cfg.Schema)

	// Auto-migrate models if provided
	if len(models) > 0 {
		log.Printf("🔄 Running auto-migration for %d models...", len(models))
		if err := db.AutoMigrate(models...); err != nil {
			return nil, fmt.Errorf("failed to auto-migrate models: %w", err)
		}
		log.Printf("✅ Auto-migration completed successfully")
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
	dbConfig := DatabaseConfig{
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
