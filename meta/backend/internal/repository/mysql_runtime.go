package repository

import (
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

//go:embed mysql_runtime/*.sql
var mysqlRuntimeDDL embed.FS

// PrepareMySQL installs the scan owners first. The deployment DBA can apply all
// pending triggers in one reviewed batch, before the installation publishes Meta.
func PrepareMySQL(db *gorm.DB) error {
	if err := MigrateMySQLScanTasks(db); err != nil {
		return err
	}
	if err := MigrateMySQLScanCatalog(db); err != nil {
		return err
	}
	var factsPending *schema.PendingMySQLTriggers
	if err := MigrateMySQLScanChanges(db); err != nil && !errors.As(err, &factsPending) {
		return err
	}
	files, err := mysqlRuntimeDDL.ReadDir("mysql_runtime")
	if err != nil {
		return err
	}
	var tables []string
	triggers := mysqlFactTriggers()
	for _, file := range files {
		data, err := mysqlRuntimeDDL.ReadFile("mysql_runtime/" + file.Name())
		if err != nil {
			return err
		}
		ddl := string(data)
		if strings.HasPrefix(ddl, "CREATE TRIGGER ") {
			triggers = append(triggers, ddl)
		} else {
			tables = append(tables, ddl)
		}
	}
	verified := false
	err = schema.MigrateMySQL(db, "meta", SchemaVersion, func(conn *gorm.DB) error {
		for _, ddl := range tables {
			if err := conn.Exec(ddl).Error; err != nil {
				return fmt.Errorf("prepare Meta runtime tables: %w", err)
			}
		}
		if err := schema.CheckMySQLTriggers(conn, triggers); err != nil {
			return err
		}
		if err := schema.RequireMySQL(conn, mysqlScanChangesOwner, mysqlScanChangesVersion); err != nil {
			return err
		}
		verified = true
		return nil
	})
	if err != nil || verified {
		return err
	}
	// A published revision alone cannot attest to the current trigger bodies.
	if err := schema.CheckMySQLTriggers(db, triggers); err != nil {
		return err
	}
	return schema.RequireMySQL(db, mysqlScanChangesOwner, mysqlScanChangesVersion)
}
