package repository

import (
	"fmt"
	"io/fs"
	"log"
	"sort"
	"strings"

	orchestratorMigrations "github.com/addp/orchestrator/migrations"
	"gorm.io/gorm"
)

func ApplySQLMigrations(db *gorm.DB) error {
	if err := ensureMigrationTable(db); err != nil {
		return err
	}

	names, err := migrationNames(orchestratorMigrations.FS, ".")
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

		content, err := fs.ReadFile(orchestratorMigrations.FS, name)
		if err != nil {
			return fmt.Errorf("failed to read orchestrator migration %s: %w", name, err)
		}

		log.Printf("[Orchestrator Migration] ▶ Applying SQL migration: %s", name)
		if err := db.Exec(string(content)).Error; err != nil {
			return fmt.Errorf("failed to apply orchestrator migration %s: %w", name, err)
		}
		if err := recordMigration(db, name); err != nil {
			return err
		}
		log.Printf("[Orchestrator Migration] ✅ Applied: %s", name)
	}

	return nil
}

func ensureMigrationTable(db *gorm.DB) error {
	if err := db.Exec(`
		CREATE SCHEMA IF NOT EXISTS orchestrator;
		CREATE TABLE IF NOT EXISTS orchestrator.schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`).Error; err != nil {
		return fmt.Errorf("failed to ensure orchestrator.schema_migrations: %w", err)
	}
	return nil
}

func migrationNames(migrationFS fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(migrationFS, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read orchestrator migrations directory: %w", err)
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
			FROM orchestrator.schema_migrations
			WHERE version = ?
		)
	`, version).Scan(&applied).Error; err != nil {
		return false, fmt.Errorf("failed to check orchestrator migration %s: %w", version, err)
	}
	return applied, nil
}

func recordMigration(db *gorm.DB, version string) error {
	if err := db.Exec(`
		INSERT INTO orchestrator.schema_migrations(version)
		VALUES (?)
		ON CONFLICT (version) DO NOTHING
	`, version).Error; err != nil {
		return fmt.Errorf("failed to record orchestrator migration %s: %w", version, err)
	}
	return nil
}
