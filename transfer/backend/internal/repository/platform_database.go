package repository

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/addp/common/cleanup"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/common/infra/database"
	"github.com/addp/common/schema"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
)

//go:embed tables.mysql.sql
var mysqlTables string

func OpenDatabase(config database.Config) (*gorm.DB, error) {
	db, err := bootstrap.OpenPlatformRuntime(config, "transfer", &gorm.Config{TranslateError: true})
	if err != nil {
		return nil, err
	}
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return nil, err
	}
	if err := schema.Require(db, "transfer", SchemaVersion); err != nil {
		return nil, err
	}
	return db, nil
}

func PrepareDatabase(db *gorm.DB) error {
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	if db.Dialector.Name() == "mysql" {
		if _, err := projectionstore.Migrate(db, "transfer", "transfer", nil); err != nil {
			return err
		}
		return schema.MigrateMySQL(db, "transfer", SchemaVersion, func(tx *gorm.DB) error {
			for _, stmt := range strings.Split(strings.TrimSuffix(strings.TrimSpace(mysqlTables), ";"), ";") {
				if err := tx.Exec(stmt).Error; err != nil {
					header, _, _ := strings.Cut(strings.TrimSpace(stmt), " (")
					return fmt.Errorf("%s: %w", header, err)
				}
			}
			if err := cleanup.PrepareMySQLResultTable(tx, "transfer"); err != nil {
				return err
			}
			return schema.CheckMySQLTriggers(tx, []string{cleanup.MySQLResultUpdateGuard})
		})
	}
	return schema.Migrate(db, "transfer", SchemaVersion, func(tx *gorm.DB) error {
		if err := MigrateCaptureProviderResources(tx); err != nil {
			return err
		}
		if err := RetireDevelopmentAuthorizationHistory(tx); err != nil {
			return err
		}
		if err := RemoveExecutionLogs(tx); err != nil {
			return err
		}
		if err := tx.AutoMigrate(transferSchemaModels()...); err != nil {
			return fmt.Errorf("auto-migrate transfer models: %w", err)
		}

		if err := MigrateInfraObjectReferences(tx); err != nil {
			return err
		}
		if err := cleanup.MigrateResults(tx, "transfer"); err != nil {
			return err
		}
		_, err := projectionstore.Migrate(tx, "transfer", "transfer", nil)
		return err
	})
}

func transferSchemaModels() []interface{} {
	return []interface{}{
		&models.TransferTask{},
		&models.RunApproval{},
		&models.SyncState{},
		&models.RuntimeLease{},
		&models.RuntimeSession{},
		&models.CleanupOperation{},
		&models.CaptureResource{},
		&models.CaptureMetricHistory{},
		&models.PostgreSQLCaptureResource{},
		&models.MySQLCaptureResource{},
		&models.OracleCaptureResource{},
		&models.SchemaChangeRequest{},
		&models.DeadLetter{},
		&models.ContinuousPolicy{},
	}
}
