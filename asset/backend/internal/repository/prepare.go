package repository

import (
	_ "embed"
	"github.com/addp/common/cleanup"
	"github.com/addp/common/schema"
	"gorm.io/gorm"
	"strings"
)

const SchemaVersion = 1

//go:embed tables.mysql.sql
var mysqlTables string

func PrepareDatabase(db *gorm.DB, seed func(*gorm.DB) error) error {
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	if db.Dialector.Name() == "mysql" {
		return schema.MigrateMySQLWithData(db, "asset", SchemaVersion, prepareMySQL, func(tx *gorm.DB, _ int64) error { return seed(tx) })
	}
	return schema.Migrate(db, "asset", SchemaVersion, func(tx *gorm.DB) error {
		if err := Migrate(tx); err != nil {
			return err
		}
		return seed(tx)
	})
}

func prepareMySQL(db *gorm.DB) error {
	if err := cleanup.PrepareMySQLResultTable(db, "asset"); err != nil {
		return err
	}
	for _, statement := range strings.Split(strings.TrimSuffix(strings.TrimSpace(mysqlTables), ";"), ";") {
		if err := db.Exec(statement).Error; err != nil {
			return err
		}
	}
	return schema.CheckMySQLTriggers(db, []string{cleanup.MySQLResultUpdateGuard})
}
