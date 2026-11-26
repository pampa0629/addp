package repository

import (
	_ "embed"
	"strings"

	"github.com/addp/common/cleanup"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/exportartifact"
	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

const SchemaVersion = 2

//go:embed tables.mysql.sql
var mysqlTables string

//go:embed catalog_changes.mysql.sql
var catalogTriggers string

func PrepareDatabase(db *gorm.DB) error {
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	if db.Dialector.Name() == "mysql" {
		if _, err := projectionstore.Migrate(db, "develop", "develop", nil); err != nil {
			return err
		}
		return schema.MigrateMySQL(db, "develop", SchemaVersion, prepareMySQL)
	}
	return schema.Migrate(db, "develop", SchemaVersion, func(tx *gorm.DB) error {
		if err := schema.CheckDevelopFileCutover(tx.Statement.Context, tx.Statement.ConnPool, "postgres"); err != nil {
			return err
		}
		if err := preparePostgres(tx); err != nil {
			return err
		}
		if _, err := projectionstore.Migrate(tx, "develop", "develop", nil); err != nil {
			return err
		}
		return nil
	})
}

func prepareMySQL(db *gorm.DB) error {
	if err := schema.CheckDevelopFileCutover(db.Statement.Context, db.Statement.ConnPool, "mysql"); err != nil {
		return err
	}
	if err := exportartifact.EnsureStore(db, "develop.export_sessions"); err != nil {
		return err
	}
	if err := cleanup.PrepareMySQLResultTable(db, "develop"); err != nil {
		return err
	}
	for _, stmt := range strings.Split(strings.TrimSuffix(strings.TrimSpace(mysqlTables), ";"), ";") {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return schema.CheckMySQLTriggers(db, append([]string{cleanup.MySQLResultUpdateGuard}, strings.Split(strings.TrimSuffix(strings.TrimSpace(catalogTriggers), "$$"), "$$\n\n")...))
}
