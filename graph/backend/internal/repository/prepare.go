package repository

import (
	_ "embed"

	"strings"

	"github.com/addp/common/cleanup"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/schema"

	"gorm.io/gorm"
)

const SchemaVersion = 1

//go:embed tables.mysql.sql
var mysqlTables string

func PrepareDatabase(db *gorm.DB) error {
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	if db.Dialector.Name() == "mysql" {
		if _, err := projectionstore.Migrate(db, "graph", "graph", nil); err != nil {
			return err
		}
		return schema.MigrateMySQL(db, "graph", SchemaVersion, prepareMySQL)
	}
	return schema.Migrate(db, "graph", SchemaVersion, func(tx *gorm.DB) error {
		if err := MigrateGraphTables(tx); err != nil {
			return err
		}
		if _, err := projectionstore.Migrate(tx, "graph", "graph", nil); err != nil {
			return err
		}
		return cleanup.MigrateResults(tx, "graph")
	})
}

func prepareMySQL(db *gorm.DB) error {
	if err := cleanup.PrepareMySQLResultTable(db, "graph"); err != nil {
		return err
	}
	for _, stmt := range strings.Split(strings.TrimSuffix(strings.TrimSpace(mysqlTables), ";"), ";") {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return schema.CheckMySQLTriggers(db, []string{cleanup.MySQLResultUpdateGuard})
}
