package repository

import (
	"embed"
	"fmt"
	"log"
	"sort"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/addp/common/utils"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

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
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName, cfg.SSLMode,
		utils.GetTimezone(),
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

	// Create schema if it doesn't exist and schema is specified
	if cfg.Schema != "" && cfg.Schema != "public" && cfg.Schema != "system" {
		createSchemaSQL := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", cfg.Schema)
		if err := db.Exec(createSchemaSQL).Error; err != nil {
			return nil, fmt.Errorf("failed to create schema %s: %w", cfg.Schema, err)
		}
		log.Printf("✅ Ensured schema exists: %s", cfg.Schema)
	}

	log.Printf("✅ Connected to database: %s@%s:%s/%s (schema: %s)",
		cfg.User, cfg.Host, cfg.Port, cfg.DBName, cfg.Schema)

	// Run SQL migrations (before AutoMigrate to ensure schema changes are applied first)
	if err := runSQLMigrations(db); err != nil {
		return nil, fmt.Errorf("failed to run SQL migrations: %w", err)
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

// runSQLMigrations 按文件名顺序执行 migrations/ 目录下的所有 SQL 文件
func runSQLMigrations(db *gorm.DB) error {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// 按文件名排序（001_xxx.sql < 002_xxx.sql）
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		content, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", name, err)
		}

		log.Printf("[Migration] ▶ Applying SQL migration: %s", name)
		if err := db.Exec(string(content)).Error; err != nil {
			return fmt.Errorf("failed to apply migration %s: %w", name, err)
		}
		log.Printf("[Migration] ✅ Applied: %s", name)
	}

	return nil
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
