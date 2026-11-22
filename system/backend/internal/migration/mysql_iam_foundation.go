package migration

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/addp/common/schema"
	"gorm.io/gorm"
)

const mysqlIAMFoundationOwner = "system_iam_foundation"
const mysqlIAMFoundationVersion int64 = 17

//go:embed mysql_iam/*.sql mysql_module_registry/*.sql mysql_engine_catalog/*.sql mysql_target_locks/*.sql mysql_source_grant_catalog/*.sql mysql_engine_delegations/*.sql
var mysqlIAMFoundationSQL embed.FS

// RequireMySQLIAMFoundation checks the installed owner revision without DDL.
func RequireMySQLIAMFoundation(db *gorm.DB) error {
	return schema.RequireMySQL(db, mysqlIAMFoundationOwner, mysqlIAMFoundationVersion)
}

// MigrateMySQLIAMFoundation installs the staged System IAM schema.
func MigrateMySQLIAMFoundation(db *gorm.DB) error {
	return migrateSystemMySQL(db, mysqlIAMFoundationOwner, mysqlIAMFoundationVersion, "mysql_iam", installMySQLIAMCatalog)
}

const mysqlModuleRegistryOwner = "system_module_registry"
const mysqlModuleRegistryVersion int64 = 1

// CheckMySQLSystemStartup checks published System and Common revisions before
// reporting the remaining storage prerequisite for a serving System process.
func CheckMySQLSystemStartup(db *gorm.DB) error {
	if err := RequireMySQLIAMFoundation(db); err != nil {
		return err
	}
	if err := schema.RequireMySQL(db, mysqlRequestLocksOwner, mysqlRequestLocksVersion); err != nil {
		return err
	}
	if err := schema.RequireMySQL(db, mysqlTargetLocksOwner, mysqlTargetLocksVersion); err != nil {
		return err
	}
	if err := schema.RequireMySQL(db, mysqlModuleRegistryOwner, mysqlModuleRegistryVersion); err != nil {
		return err
	}
	if err := schema.RequireMySQL(db, mysqlEngineCatalogOwner, mysqlEngineCatalogVersion); err != nil {
		return err
	}
	if err := schema.RequireMySQL(db, mysqlSourceGrantCatalogOwner, mysqlSourceGrantCatalogVersion); err != nil {
		return err
	}
	if err := schema.RequireMySQL(db, mysqlEngineDelegationsOwner, mysqlEngineDelegationsVersion); err != nil {
		return err
	}
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	return errors.New("System MySQL installation does not yet include complete source authorization guards")
}

const mysqlEngineDelegationsOwner = "system_engine_delegations"
const mysqlEngineDelegationsVersion = 1

func MigrateMySQLEngineDelegations(db *gorm.DB) error {
	return migrateSystemMySQL(db, mysqlEngineDelegationsOwner, mysqlEngineDelegationsVersion, "mysql_engine_delegations", nil)
}

const mysqlSourceGrantCatalogOwner = "system_source_grant_catalog"
const mysqlSourceGrantCatalogVersion = 3

func MigrateMySQLSourceGrantCatalog(db *gorm.DB) error {
	return migrateSystemMySQL(db, mysqlSourceGrantCatalogOwner, mysqlSourceGrantCatalogVersion, "mysql_source_grant_catalog", nil)
}

const mysqlEngineCatalogOwner = "system_engine_catalog"
const mysqlEngineCatalogVersion int64 = 1

func MigrateMySQLEngineCatalog(db *gorm.DB) error {
	return migrateSystemMySQL(db, mysqlEngineCatalogOwner, mysqlEngineCatalogVersion, "mysql_engine_catalog", nil)
}

// MigrateMySQLModuleRegistry installs the System registry and its first routing revision.
func MigrateMySQLModuleRegistry(db *gorm.DB) error {
	return migrateSystemMySQL(db, mysqlModuleRegistryOwner, mysqlModuleRegistryVersion, "mysql_module_registry", func(tx *gorm.DB, _ int64) error {
		return tx.Exec("INSERT INTO module_registry_state (id, revision) VALUES (1,1)").Error
	})
}

func migrateSystemMySQL(db *gorm.DB, owner string, version int64, directory string, data func(*gorm.DB, int64) error) error {
	if db == nil || db.Dialector.Name() != "mysql" {
		return fmt.Errorf("System migration requires a MySQL connection")
	}
	var name string
	if err := db.Raw("SELECT DATABASE()").Scan(&name).Error; err != nil {
		return err
	}
	if name != "system" {
		return fmt.Errorf("System migration requires the deployed system database")
	}

	return schema.MigrateMySQLWithData(db, owner, version, func(conn *gorm.DB) error {
		migrations, err := fs.ReadDir(mysqlIAMFoundationSQL, directory)
		if err != nil {
			return err
		}
		var triggers []string
		for _, migration := range migrations {
			ddl, err := mysqlIAMFoundationSQL.ReadFile(directory + "/" + migration.Name())
			if err != nil {
				return err
			}
			if strings.HasPrefix(string(ddl), "CREATE TRIGGER ") {
				triggers = append(triggers, string(ddl))
				continue
			}
			if err := conn.Exec(string(ddl)).Error; err != nil {
				return fmt.Errorf("apply System MySQL migration %s/%s: %w", directory, migration.Name(), err)
			}
		}
		return checkMySQLSystemTriggers(conn, triggers)
	}, data)
}
