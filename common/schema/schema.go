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
const CommonVersion int64 = 1

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Migrate publishes a revision only after all owner migrations commit successfully.
func Migrate(db *gorm.DB, name string, version int64, apply func(*gorm.DB) error) error {
	if db == nil || db.Dialector.Name() != "postgres" || !namePattern.MatchString(name) || version < 1 || apply == nil {
		return fmt.Errorf("invalid PostgreSQL schema migration contract: %q version %d", name, version)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "addp.schema/"+name).Error; err != nil {
			return err
		}
		if err := tx.Exec("CREATE SCHEMA IF NOT EXISTS " + name).Error; err != nil {
			return err
		}
		table := name + ".startup_schema_revision"
		if err := tx.Exec("CREATE TABLE IF NOT EXISTS " + table + " (singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton), version BIGINT NOT NULL)").Error; err != nil {
			return err
		}
		var current int64
		if err := tx.Raw("SELECT COALESCE(MAX(version), 0) FROM " + table).Scan(&current).Error; err != nil {
			return err
		}
		if current > version {
			return fmt.Errorf("schema %s version %d is newer than this process requires (%d)", name, current, version)
		}
		if current == version {
			return nil
		}
		if err := apply(tx); err != nil {
			return fmt.Errorf("migrate schema %s to %d: %w", name, version, err)
		}
		return tx.Exec("INSERT INTO "+table+" (singleton, version) VALUES (TRUE, ?) ON CONFLICT (singleton) DO UPDATE SET version = EXCLUDED.version", version).Error
	})
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
