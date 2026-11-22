// Package schema coordinates Backend-owned startup migrations and read-only Worker checks.
package schema

import (
	"fmt"
	"regexp"

	"github.com/addp/common/execution"
	"github.com/addp/common/runtimehealth"
	"gorm.io/gorm"
)

// CommonVersion must increase when the shared execution/runtime-health schema changes.
const CommonVersion int64 = 3

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Migrate publishes a revision only after all owner migrations commit successfully.
func Migrate(db *gorm.DB, name string, version int64, apply func(*gorm.DB) error) error {
	if db == nil || db.Dialector.Name() != "postgres" || !namePattern.MatchString(name) || version < 1 || apply == nil {
		return fmt.Errorf("invalid PostgreSQL schema migration contract: %q version %d", name, version)
	}
	return migratePostgres(db, name, version, apply)
}

// Require performs no DDL or data writes. Standalone Workers fail before claiming work.
func Require(db *gorm.DB, name string, version int64) error {
	if db == nil || !namePattern.MatchString(name) || version < 1 {
		return fmt.Errorf("invalid schema requirement: %q", name)
	}
	if db.Dialector.Name() == "mysql" {
		return requireMySQLRevision(db, name+".startup_schema_revision", name, version)
	}
	var current int64
	if err := db.Raw("SELECT version FROM " + name + ".startup_schema_revision WHERE singleton = TRUE").Scan(&current).Error; err != nil {
		return fmt.Errorf("schema %s is not initialized; prepare the schema before startup: %w", name, err)
	}
	if current != version {
		return fmt.Errorf("schema %s version mismatch: database=%d process=%d; complete the schema migration before startup", name, current, version)
	}
	return nil
}

// InitializeCommon prepares the Common-owned schema on the deployment connection.
func InitializeCommon(db *gorm.DB) error {
	apply := func(session *gorm.DB) error {
		if err := execution.EnsureStore(session); err != nil {
			return err
		}
		return runtimehealth.EnsureStore(session)
	}
	if db != nil && db.Dialector.Name() == "mysql" {
		var currentDatabase string
		if err := db.Raw("SELECT DATABASE()").Scan(&currentDatabase).Error; err != nil {
			return err
		}
		if currentDatabase != "common" {
			return fmt.Errorf("Common MySQL migration requires the deployed common database")
		}
		return MigrateMySQL(db, "common", CommonVersion, apply)
	}
	return Migrate(db, "common", CommonVersion, apply)
}
