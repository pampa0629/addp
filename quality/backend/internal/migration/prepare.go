package migration

import (
	"context"
	_ "embed"
	"strings"
	"time"

	"github.com/addp/common/cleanup"
	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

//go:embed tables.mysql.sql
var mysqlTables string

func PrepareDatabase(db *gorm.DB) error {
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(db.Statement.Context, 2*time.Minute)
	defer cancel()
	db = db.WithContext(ctx)
	if db.Dialector.Name() == "mysql" {
		return schema.MigrateMySQL(db, "quality", SchemaVersion, func(tx *gorm.DB) error {
			for _, statement := range strings.Split(strings.TrimSuffix(strings.TrimSpace(mysqlTables), ";"), ";") {
				if err := tx.Exec(statement).Error; err != nil {
					return err
				}
			}
			if err := cleanup.PrepareMySQLResultTable(tx, "quality"); err != nil {
				return err
			}
			return schema.CheckMySQLTriggers(tx, []string{cleanup.MySQLResultUpdateGuard})
		})
	}
	return schema.Migrate(db, "quality", SchemaVersion, func(tx *gorm.DB) error { return NewRunner(tx).Run(ctx) })
}
