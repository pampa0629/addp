package migration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
)

const (
	DefaultSchemaName      = "system"
	DefaultMigrationsRoot  = "sql"
	DefaultMigrationsTable = "schema_migrations"
)

type Runner struct {
	DSN  string
	FS   fs.FS
	Root string
}

func NewRunner(dsn string) *Runner {
	return &Runner{DSN: dsn, FS: EmbeddedSQL, Root: DefaultMigrationsRoot}
}

func (r *Runner) Run(ctx context.Context) error {
	if r.DSN == "" {
		return errors.New("migration database DSN is empty")
	}
	if r.FS == nil {
		return errors.New("migration filesystem is nil")
	}
	root := r.Root
	if root == "" {
		root = DefaultMigrationsRoot
	}
	catalog, err := ReadCatalog(r.FS, root)
	if err != nil {
		return err
	}

	db, err := sql.Open("postgres", r.DSN)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping migration database: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS system`); err != nil {
		return fmt.Errorf("ensure system schema: %w", err)
	}
	if err := rejectLegacySchema(ctx, db); err != nil {
		return err
	}
	preMigrationVersion, preMigrationDirty, hasMigrationState, err := readExistingMigrationState(ctx, db)
	if err != nil {
		return err
	}
	if hasMigrationState {
		if preMigrationDirty {
			return fmt.Errorf("system IAM migration version %d is dirty", preMigrationVersion)
		}
		if preMigrationVersion > catalog.LatestVersion {
			return fmt.Errorf("database migration version %d is newer than embedded version %d", preMigrationVersion, catalog.LatestVersion)
		}
		if err := verifyRecordedMigrationChecksums(ctx, db, catalog, preMigrationVersion); err != nil {
			return err
		}
	}

	sourceDriver, err := iofs.New(r.FS, root)
	if err != nil {
		return fmt.Errorf("create embedded migration source: %w", err)
	}
	databaseDriver, err := postgres.WithInstance(db, &postgres.Config{
		SchemaName:      DefaultSchemaName,
		MigrationsTable: DefaultMigrationsTable,
	})
	if err != nil {
		return fmt.Errorf("create PostgreSQL migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, DefaultSchemaName, databaseDriver)
	if err != nil {
		return fmt.Errorf("create migration runner: %w", err)
	}
	migrationClosed := false
	defer func() {
		if !migrationClosed {
			_, _ = m.Close()
		}
	}()
	// The database advisory lock is the startup gate. Orchestration may terminate
	// the process, but System must never continue while another instance migrates.
	m.LockTimeout = time.Duration(math.MaxInt64)

	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("read migration version: %w", err)
	}
	if dirty {
		return fmt.Errorf("system IAM migration version %d is dirty", version)
	}
	if err == nil && version > catalog.LatestVersion {
		return fmt.Errorf("database migration version %d is newer than embedded version %d", version, catalog.LatestVersion)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply system IAM migrations: %w", err)
	}
	version, dirty, err = m.Version()
	if err != nil {
		return fmt.Errorf("read applied migration version: %w", err)
	}
	if dirty {
		return fmt.Errorf("system IAM migration version %d is dirty after Up", version)
	}
	if version != catalog.LatestVersion {
		return fmt.Errorf("applied migration version %d does not match embedded version %d", version, catalog.LatestVersion)
	}
	sourceCloseErr, databaseCloseErr := m.Close()
	migrationClosed = true
	if sourceCloseErr != nil {
		return fmt.Errorf("close migration source: %w", sourceCloseErr)
	}
	if databaseCloseErr != nil {
		return fmt.Errorf("close migration database driver: %w", databaseCloseErr)
	}
	checksumDB, err := sql.Open("postgres", r.DSN)
	if err != nil {
		return fmt.Errorf("open migration checksum database: %w", err)
	}
	checksumDB.SetMaxOpenConns(1)
	checksumDB.SetMaxIdleConns(1)
	defer checksumDB.Close()
	if err := checksumDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping migration checksum database: %w", err)
	}
	if err := recordAndVerifyMigrationChecksums(ctx, checksumDB, catalog, version); err != nil {
		return err
	}
	return nil
}

func readExistingMigrationState(ctx context.Context, db *sql.DB) (uint, bool, bool, error) {
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('system.schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return 0, false, false, fmt.Errorf("inspect migration state table: %w", err)
	}
	if !exists {
		return 0, false, false, nil
	}
	var version uint
	var dirty bool
	if err := db.QueryRowContext(ctx, `SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		return 0, false, false, fmt.Errorf("read migration state: %w", err)
	}
	return version, dirty, true, nil
}

func rejectLegacySchema(ctx context.Context, db *sql.DB) error {
	var legacy bool
	err := db.QueryRowContext(ctx, `
		SELECT to_regclass('system.users') IS NOT NULL
		   AND to_regclass('system.schema_migrations') IS NULL
	`).Scan(&legacy)
	if err != nil {
		return fmt.Errorf("inspect legacy IAM schema: %w", err)
	}
	if legacy {
		return errors.New("legacy system IAM schema detected; rebuild the development IAM database before running target migrations")
	}
	return nil
}
