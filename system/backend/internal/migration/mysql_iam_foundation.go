package migration

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"text/template"

	"github.com/addp/common/schema"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

const mysqlIAMFoundationOwner = "system_iam_foundation"
const mysqlIAMFoundationVersion int64 = 23

//go:embed mysql_iam/*.sql mysql_module_registry/*.sql mysql_engine_catalog/*.sql mysql_target_locks/*.sql mysql_source_grant_catalog/*.sql mysql_engine_delegations/*.sql mysql_cleanup/*.sql mysql_execution_authorization/*.sql
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

// CheckMySQLSystemStartup verifies the installed owners using the runtime connection.
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
	if err := schema.RequireMySQL(db, mysqlExecutionAuthorizationOwner, mysqlExecutionAuthorizationVersion); err != nil {
		return err
	}
	if err := schema.RequireMySQL(db, mysqlSourceGrantCatalogOwner, mysqlSourceGrantCatalogVersion); err != nil {
		return err
	}
	if err := schema.RequireMySQL(db, mysqlEngineDelegationsOwner, mysqlEngineDelegationsVersion); err != nil {
		return err
	}
	if err := schema.RequireMySQL(db, mysqlCleanupOwner, mysqlCleanupVersion); err != nil {
		return err
	}
	if err := schema.Require(db, "common", schema.CommonVersion); err != nil {
		return err
	}
	return schema.RequireMySQL(db, mysqlGrantProcedureOwner, mysqlGrantProcedureVersion)
}

const mysqlCleanupOwner = "system_cleanup_requests"
const mysqlCleanupVersion = 1

func MigrateMySQLCleanupRequests(db *gorm.DB) error {
	return migrateSystemMySQL(db, mysqlCleanupOwner, mysqlCleanupVersion, "mysql_cleanup", nil)
}

const mysqlEngineDelegationsOwner = "system_engine_delegations"
const mysqlEngineDelegationsVersion = 1

func MigrateMySQLEngineDelegations(db *gorm.DB) error {
	return migrateSystemMySQL(db, mysqlEngineDelegationsOwner, mysqlEngineDelegationsVersion, "mysql_engine_delegations", nil)
}

const mysqlSourceGrantCatalogOwner = "system_source_grant_catalog"
const mysqlSourceGrantCatalogVersion = 6

//go:embed mysql_source_grant_node.sql
var mysqlSourceGrantNode string

//go:embed mysql_source_grant_node_v5.sql
var mysqlSourceGrantNodeV5 string

//go:embed mysql_source_grant_node_v4.sql
var mysqlSourceGrantNodeV4 string

//go:embed mysql_source_identity.sql.tmpl
var mysqlSourceIdentitySQL string

func MigrateMySQLSourceGrantCatalog(db *gorm.DB) error {
	return migrateSystemMySQL(db, mysqlSourceGrantCatalogOwner, mysqlSourceGrantCatalogVersion, "mysql_source_grant_catalog", nil)
}

const mysqlEngineCatalogOwner = "system_engine_catalog"
const mysqlEngineCatalogVersion int64 = 3

func MigrateMySQLEngineCatalog(db *gorm.DB) error {
	return migrateSystemMySQL(db, mysqlEngineCatalogOwner, mysqlEngineCatalogVersion, "mysql_engine_catalog", nil)
}

const mysqlExecutionAuthorizationOwner = "system_execution_authorization"
const mysqlExecutionAuthorizationVersion = 1

// MigrateMySQLExecutionAuthorization installs the general execution grants after the IAM and engine tables.
func MigrateMySQLExecutionAuthorization(db *gorm.DB) error {
	return migrateSystemMySQL(db, mysqlExecutionAuthorizationOwner, mysqlExecutionAuthorizationVersion, "mysql_execution_authorization", nil)
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
		if owner == mysqlEngineCatalogOwner && !conn.Migrator().HasColumn("engines", "runtime_registrar") {
			if err := conn.Exec("ALTER TABLE engines ADD COLUMN runtime_registrar VARCHAR(96)").Error; err != nil {
				return err
			}
		}
		if owner == mysqlEngineCatalogOwner && !conn.Migrator().HasColumn("engines", "pool_only_since_version") {
			if err := conn.Exec("ALTER TABLE engines ADD COLUMN pool_only_since_version BIGINT NULL, ADD CONSTRAINT ck_engine_pool_version CHECK (pool_only_since_version >= 1 AND pool_only_since_version < version)").Error; err != nil {
				return err
			}
		}
		if owner == mysqlSourceGrantCatalogOwner {
			oldPosition := len(triggers)
			identity, err := template.New("source item identity").Parse(mysqlSourceIdentitySQL)
			if err != nil {
				return err
			}
			for _, grant := range []bool{false, true} {
				var trigger strings.Builder
				if err := identity.Execute(&trigger, struct{ Grant bool }{grant}); err != nil {
					return err
				}
				triggers = append(triggers, trigger.String())
			}
			triggers = append(triggers, mysqlSourceGrantNode)
			// Check the installed guards in their original order before retiring any.
			withOld := append([]string{}, triggers...)
			var retireNames []string
			for _, old := range []struct{ name, ddl string }{
				{"source_grant_node_target", mysqlSourceGrantNodeV4},
				{"source_grant_catalog_node", mysqlSourceGrantNodeV5},
			} {
				var present bool
				if err := conn.Raw("SELECT EXISTS (SELECT 1 FROM information_schema.triggers WHERE trigger_schema=DATABASE() AND trigger_name=?)", old.name).Row().Scan(&present); err != nil {
					return err
				}
				if present {
					position := len(withOld) - 1
					if old.name == "source_grant_node_target" {
						position = oldPosition
					}
					withOld = slices.Insert(withOld, position, old.ddl)
					retireNames = append(retireNames, old.name)
				}
			}
			if len(retireNames) > 0 {
				if err := schema.CheckMySQLTriggers(conn, withOld); err != nil {
					return err
				}
			}
			for _, name := range retireNames {
				retire := "DROP TRIGGER " + name
				if err := conn.Exec(retire).Error; err != nil {
					var denied *mysqlDriver.MySQLError
					if errors.As(err, &denied) && denied.Number == 1419 {
						return &schema.PendingMySQLTriggers{Statements: []string{retire}}
					}
					return err
				}
			}
		}
		return schema.CheckMySQLTriggers(conn, triggers)
	}, data)
}
