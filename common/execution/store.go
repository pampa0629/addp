package execution

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"

	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

const executionStoreMigrationLockID int64 = 2026030501

// EnsureStore ensures the common execution schema objects are present.
func EnsureStore(db *gorm.DB) error {
	return withExecutionStoreMigrationLock(db, ensureStore)
}

func ensureStore(db *gorm.DB) error {
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS common").Error; err != nil {
		return fmt.Errorf("failed to create common schema: %w", err)
	}

	exists, err := taskExecutionsTableExists(db)
	if err != nil {
		return err
	}

	if exists {
		if err := runSQLMigrations(db); err != nil {
			return err
		}
	}

	if err := db.AutoMigrate(&TaskExecution{}); err != nil {
		return fmt.Errorf("failed to auto-migrate TaskExecution: %w", err)
	}

	if !exists {
		if err := runSQLMigrations(db); err != nil {
			return err
		}
	}

	return nil
}

func withExecutionStoreMigrationLock(db *gorm.DB, fn func(*gorm.DB) error) error {
	if db.Dialector.Name() != "postgres" {
		return fn(db)
	}

	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", executionStoreMigrationLockID).Error; err != nil {
			return fmt.Errorf("failed to acquire execution store migration lock: %w", err)
		}
		return fn(tx)
	})
}

func taskExecutionsTableExists(db *gorm.DB) (bool, error) {
	var exists bool
	err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'common'
			  AND table_name = 'task_executions'
		)
	`).Scan(&exists).Error
	if err != nil {
		return false, fmt.Errorf("failed to check common.task_executions existence: %w", err)
	}
	return exists, nil
}

func runSQLMigrations(db *gorm.DB) error {
	if err := ensureExecutionMigrationTable(db); err != nil {
		return err
	}

	names, err := executionMigrationNames(migrationFiles, "migrations")
	if err != nil {
		return err
	}

	for _, name := range names {
		applied, err := executionMigrationApplied(db, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		content, err := fs.ReadFile(migrationFiles, "migrations/"+name)
		if err != nil {
			return fmt.Errorf("failed to read execution migration file %s: %w", name, err)
		}

		log.Printf("[Execution Migration] ▶ Applying SQL migration: %s", name)
		if err := db.Exec(string(content)).Error; err != nil {
			return fmt.Errorf("failed to apply execution migration %s: %w", name, err)
		}
		if err := recordExecutionMigration(db, name); err != nil {
			return err
		}
		log.Printf("[Execution Migration] ✅ Applied: %s", name)
	}

	return nil
}

func executionMigrationNames(migrationFS fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(migrationFS, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read execution migrations directory: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") || strings.HasSuffix(name, "_down.sql") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func ensureExecutionMigrationTable(db *gorm.DB) error {
	if db.Dialector.Name() != "postgres" {
		if err := db.Exec(`
			CREATE TABLE IF NOT EXISTS common.execution_schema_migrations (
				version TEXT PRIMARY KEY,
				applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			)
		`).Error; err != nil {
			return fmt.Errorf("failed to ensure common.execution_schema_migrations: %w", err)
		}
		return nil
	}

	if err := db.Exec(`
		CREATE SCHEMA IF NOT EXISTS common;
		CREATE TABLE IF NOT EXISTS common.execution_schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`).Error; err != nil {
		return fmt.Errorf("failed to ensure common.execution_schema_migrations: %w", err)
	}
	return nil
}

func executionMigrationApplied(db *gorm.DB, version string) (bool, error) {
	var applied bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM common.execution_schema_migrations
			WHERE version = ?
		)
	`, version).Scan(&applied).Error; err != nil {
		return false, fmt.Errorf("failed to check execution migration %s: %w", version, err)
	}
	return applied, nil
}

func recordExecutionMigration(db *gorm.DB, version string) error {
	if err := db.Exec(`
		INSERT INTO common.execution_schema_migrations(version)
		VALUES (?)
		ON CONFLICT (version) DO NOTHING
	`, version).Error; err != nil {
		return fmt.Errorf("failed to record execution migration %s: %w", version, err)
	}
	return nil
}
