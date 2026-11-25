package repository

import (
	_ "embed"
	"strings"

	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

const SchemaVersion = 2

//go:embed tables.mysql.sql
var mysqlTables string

//go:embed guards.mysql.sql
var mysqlGuards string

func PrepareDatabase(db *gorm.DB) error {
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	if db.Dialector.Name() == "mysql" {
		return schema.MigrateMySQL(db, "catalog", SchemaVersion, func(conn *gorm.DB) error {
			for _, stmt := range strings.Split(strings.TrimSuffix(strings.TrimSpace(mysqlTables), ";"), ";") {
				if err := conn.Exec(stmt).Error; err != nil {
					return err
				}
			}
			return schema.CheckMySQLTriggers(conn, strings.Split(strings.TrimSuffix(strings.TrimSpace(mysqlGuards), "$$"), "$$\n\n"))
		})
	}
	return schema.Migrate(db, "catalog", SchemaVersion, Migrate)
}
