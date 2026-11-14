package schema

import (
	"fmt"

	"gorm.io/gorm"
)

// EnsureNamespace prepares a module namespace before its own migrations run.
func EnsureNamespace(db *gorm.DB, name string) error {
	if db == nil || !namePattern.MatchString(name) || db.Dialector.Name() != "postgres" {
		return fmt.Errorf("invalid platform database namespace %q", name)
	}
	return createPostgresNamespace(db, name)
}

func createPostgresNamespace(db *gorm.DB, name string) error {
	return db.Exec("CREATE SCHEMA IF NOT EXISTS " + name).Error
}

func migratePostgres(db *gorm.DB, name string, version int64, apply func(*gorm.DB) error) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "addp.schema/"+name).Error; err != nil {
			return err
		}
		if err := createPostgresNamespace(tx, name); err != nil {
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
