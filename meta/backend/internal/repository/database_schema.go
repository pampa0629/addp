package repository

import (
	"fmt"

	"github.com/addp/common/logger"
	"github.com/addp/common/utils"
	"github.com/addp/meta/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

func PrepareSchema(cfg *config.Config) error {
	if cfg.DBSchema != "meta" {
		return fmt.Errorf("meta module schema must be 'meta', got %q", cfg.DBSchema)
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=%s",
		cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, utils.GetTimezone(),
	)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: gormLogger.Default.LogMode(gormLogger.Silent),
	})
	if err != nil {
		return fmt.Errorf("failed to connect database for meta schema preparation: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get schema preparation database instance: %w", err)
	}
	defer sqlDB.Close()

	metadataExists, err := schemaExists(db, "metadata")
	if err != nil {
		return err
	}
	metaExists, err := schemaExists(db, "meta")
	if err != nil {
		return err
	}

	if metadataExists && metaExists {
		return fmt.Errorf("both 'metadata' and 'meta' schemas exist; please resolve before starting meta service")
	}
	if metadataExists {
		if err := db.Exec(`ALTER SCHEMA metadata RENAME TO meta`).Error; err != nil {
			return fmt.Errorf("failed to rename schema metadata to meta: %w", err)
		}
		logger.L().Info("Meta schema renamed", "from", "metadata", "to", "meta")
	}
	return nil
}

func schemaExists(db *gorm.DB, schemaName string) (bool, error) {
	var exists bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.schemata
			WHERE schema_name = ?
		)
	`, schemaName).Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("failed to check schema %s: %w", schemaName, err)
	}
	return exists, nil
}
