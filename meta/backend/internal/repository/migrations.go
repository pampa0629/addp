package repository

import (
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"

	metaMigrations "github.com/addp/meta/migrations"
	"gorm.io/gorm"
)

func applySQLMigrations(db *gorm.DB) error {
	return runSQLMigrations(db, metaMigrations.FS, ".")
}

func runSQLMigrations(db *gorm.DB, migrationFS fs.FS, dir string) error {
	if err := ensureMigrationTable(db); err != nil {
		return err
	}

	names, err := migrationNames(migrationFS, dir)
	if err != nil {
		return err
	}

	for _, name := range names {
		applied, err := migrationApplied(db, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		path := name
		if dir != "." && dir != "" {
			path = strings.TrimSuffix(dir, "/") + "/" + name
		}
		content, err := fs.ReadFile(migrationFS, path)
		if err != nil {
			return fmt.Errorf("failed to read meta migration %s: %w", name, err)
		}

		log.Printf("[Meta Migration] ▶ Applying SQL migration: %s", name)
		if err := db.Exec(string(content)).Error; err != nil {
			return fmt.Errorf("failed to apply meta migration %s: %w", name, err)
		}
		if err := recordMigration(db, name); err != nil {
			return err
		}
		log.Printf("[Meta Migration] ✅ Applied: %s", name)
	}

	return nil
}

func ensureMigrationTable(db *gorm.DB) error {
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS meta.schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`).Error; err != nil {
		return fmt.Errorf("failed to ensure meta.schema_migrations: %w", err)
	}
	return nil
}

func migrationNames(migrationFS fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(migrationFS, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read meta migrations directory: %w", err)
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

func migrationApplied(db *gorm.DB, version string) (bool, error) {
	var applied bool
	if err := db.Raw(`
		SELECT EXISTS (
			SELECT 1
			FROM meta.schema_migrations
			WHERE version = ?
		)
	`, version).Scan(&applied).Error; err != nil {
		return false, fmt.Errorf("failed to check meta migration %s: %w", version, err)
	}
	return applied, nil
}

func recordMigration(db *gorm.DB, version string) error {
	if err := db.Exec(`
		INSERT INTO meta.schema_migrations(version)
		VALUES (?)
		ON CONFLICT (version) DO NOTHING
	`, version).Error; err != nil {
		return fmt.Errorf("failed to record meta migration %s: %w", version, err)
	}
	return nil
}
