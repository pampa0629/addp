package migration

import (
	"context"
	"fmt"
	"io/fs"

	"gorm.io/gorm"
)

const (
	DefaultMigrationsRoot = "sql"
	migrationLockID       = int64(2026081201)
)

type Runner struct {
	db   *gorm.DB
	fs   fs.FS
	root string
}

func NewRunner(db *gorm.DB) *Runner {
	return &Runner{db: db, fs: EmbeddedSQL, root: DefaultMigrationsRoot}
}

func NewRunnerWithSource(db *gorm.DB, source fs.FS, root string) *Runner {
	return &Runner{db: db, fs: source, root: root}
}

func (r *Runner) Run(ctx context.Context) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("quality migration database is required")
	}
	if r.fs == nil {
		return fmt.Errorf("quality migration filesystem is required")
	}
	catalog, err := ReadCatalog(r.fs, r.root)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() != "postgres" {
			return fmt.Errorf("quality migrations require PostgreSQL")
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", migrationLockID).Error; err != nil {
			return fmt.Errorf("acquire quality migration lock: %w", err)
		}
		if err := tx.Exec(`
			CREATE SCHEMA IF NOT EXISTS quality;
			CREATE TABLE IF NOT EXISTS quality.schema_migrations (
				version BIGINT PRIMARY KEY,
				filename TEXT NOT NULL,
				sha256 CHAR(64) NOT NULL,
				applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)
		`).Error; err != nil {
			return fmt.Errorf("ensure quality migration state: %w", err)
		}

		var applied []appliedMigration
		if err := tx.Table("quality.schema_migrations").Order("version ASC").Find(&applied).Error; err != nil {
			return fmt.Errorf("read quality migration state: %w", err)
		}
		if err := verifyAppliedMigrations(catalog, applied); err != nil {
			return err
		}
		for _, file := range catalog.Files[len(applied):] {
			if err := tx.Exec(file.Contents).Error; err != nil {
				return fmt.Errorf("apply quality migration %s: %w", file.Name, err)
			}
			if err := tx.Table("quality.schema_migrations").Create(&appliedMigration{
				Version: file.Version, Filename: file.Name, SHA256: file.SHA256,
			}).Error; err != nil {
				return fmt.Errorf("record quality migration %s: %w", file.Name, err)
			}
		}
		return nil
	})
}

type appliedMigration struct {
	Version  uint   `gorm:"column:version"`
	Filename string `gorm:"column:filename"`
	SHA256   string `gorm:"column:sha256"`
}

func (appliedMigration) TableName() string { return "quality.schema_migrations" }

func verifyAppliedMigrations(catalog Catalog, applied []appliedMigration) error {
	if len(applied) > len(catalog.Files) {
		return fmt.Errorf("quality database migration version exceeds embedded catalog")
	}
	for index, item := range applied {
		expected := catalog.Files[index]
		if item.Version != expected.Version {
			return fmt.Errorf("quality applied migration sequence is broken: expected %06d, found %06d", expected.Version, item.Version)
		}
		if item.Filename != expected.Name || item.SHA256 != expected.SHA256 {
			return fmt.Errorf("quality applied migration %06d does not match embedded file", item.Version)
		}
	}
	return nil
}
