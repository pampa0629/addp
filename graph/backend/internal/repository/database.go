package repository

import (
	"fmt"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/schema"
	"github.com/addp/graph/internal/config"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func InitDatabase(cfg *config.Config) (*gorm.DB, error) {
	db, err := bootstrap.OpenPlatformRuntime(cfg.PlatformDatabase(), "graph", &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return nil, err
	}
	if err := schema.Require(db, "graph", SchemaVersion); err != nil {
		return nil, err
	}
	return db, nil
}
