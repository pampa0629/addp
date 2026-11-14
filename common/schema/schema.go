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
	var current int64
	if err := db.Raw("SELECT version FROM " + name + ".startup_schema_revision WHERE singleton = TRUE").Scan(&current).Error; err != nil {
		return fmt.Errorf("schema %s is not initialized; start its Backend first: %w", name, err)
	}
	if current != version {
		return fmt.Errorf("schema %s version mismatch: database=%d process=%d; complete Backend migration before starting Worker", name, current, version)
	}
	return nil
}

// InitializeCommon is called only by System Backend; domain implementations stay in common.
func InitializeCommon(db *gorm.DB) error {
	return Migrate(db, "common", CommonVersion, func(tx *gorm.DB) error {
		if err := execution.EnsureStore(tx); err != nil {
			return err
		}
		return runtimehealth.EnsureStore(tx)
	})
}
