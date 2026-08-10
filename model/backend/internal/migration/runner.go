package migration

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gorm.io/gorm"
)

//go:embed sql/*.sql
var migrationFiles embed.FS

const migrationLockID int64 = 2026081001

func Run(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("model migration database is required")
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", migrationLockID).Error; err != nil {
				return fmt.Errorf("acquire model migration lock: %w", err)
			}
		}
		if err := ensureMigrationTable(tx); err != nil {
			return err
		}
		names, err := migrationNames(migrationFiles, "sql")
		if err != nil {
			return err
		}
		for _, name := range names {
			var count int64
			if err := tx.Table("model.schema_migrations").Where("version = ?", name).Count(&count).Error; err != nil {
				return fmt.Errorf("check model migration %s: %w", name, err)
			}
			if count > 0 {
				continue
			}
			content, err := fs.ReadFile(migrationFiles, "sql/"+name)
			if err != nil {
				return fmt.Errorf("read model migration %s: %w", name, err)
			}
			if err := tx.Exec(string(content)).Error; err != nil {
				return fmt.Errorf("apply model migration %s: %w", name, err)
			}
			if err := tx.Exec("INSERT INTO model.schema_migrations(version) VALUES (?)", name).Error; err != nil {
				return fmt.Errorf("record model migration %s: %w", name, err)
			}
		}
		return nil
	})
}

func migrationNames(source fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(source, dir)
	if err != nil {
		return nil, fmt.Errorf("read model migration directory: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func ensureMigrationTable(db *gorm.DB) error {
	if err := db.Exec(`
		CREATE SCHEMA IF NOT EXISTS model;
		CREATE TABLE IF NOT EXISTS model.schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`).Error; err != nil {
		return fmt.Errorf("ensure model migration table: %w", err)
	}
	return nil
}
