package repository

import (
	"context"

	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

// SchemaVersion tracks the Security-owned table and constraint contract.
const SchemaVersion int64 = 6

func PrepareDatabase(ctx context.Context, cfg database.Config) error {
	db, err := bootstrap.OpenPlatformInstallation(cfg, "security")
	if err != nil {
		return err
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer pool.Close()
	db = db.WithContext(ctx)
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	if db.Dialector.Name() == "mysql" {
		return schema.MigrateMySQL(db, "security", SchemaVersion, Migrate)
	}
	return schema.Migrate(db, "security", SchemaVersion, Migrate)
}

func OpenRuntime(cfg database.Config) (*gorm.DB, error) {
	db, err := bootstrap.OpenPlatformRuntime(cfg, "security", &gorm.Config{TranslateError: true})
	if err != nil {
		return nil, err
	}
	if err = schema.Require(db, "security", SchemaVersion); err == nil {
		err = schema.Require(db, "common", schema.CommonVersion)
	}
	if err != nil {
		if pool, poolErr := db.DB(); poolErr == nil {
			pool.Close()
		}
		return nil, err
	}
	return db, nil
}
