package migration

import (
	"embed"
	"fmt"
	"io/fs"

	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

const mysqlIAMFoundationOwner = "system_iam_foundation"
const mysqlIAMFoundationVersion int64 = 4

//go:embed mysql_iam/*.sql
var mysqlIAMFoundationSQL embed.FS

// MigrateMySQLIAMFoundation installs the staged System IAM schema.
func MigrateMySQLIAMFoundation(db *gorm.DB) error {
	if db == nil || db.Dialector.Name() != "mysql" {
		return fmt.Errorf("System IAM foundation requires a MySQL connection")
	}
	var name string
	if err := db.Raw("SELECT DATABASE()").Scan(&name).Error; err != nil {
		return err
	}
	if name != "system" {
		return fmt.Errorf("System IAM foundation requires the deployed system database")
	}

	return schema.MigrateMySQL(db, mysqlIAMFoundationOwner, mysqlIAMFoundationVersion, func(conn *gorm.DB) error {
		migrations, err := fs.ReadDir(mysqlIAMFoundationSQL, "mysql_iam")
		if err != nil {
			return err
		}
		for _, migration := range migrations {
			ddl, err := mysqlIAMFoundationSQL.ReadFile("mysql_iam/" + migration.Name())
			if err != nil {
				return err
			}
			if err := conn.Exec(string(ddl)).Error; err != nil {
				return fmt.Errorf("apply System MySQL IAM migration %s: %w", migration.Name(), err)
			}
		}
		return nil
	})
}
