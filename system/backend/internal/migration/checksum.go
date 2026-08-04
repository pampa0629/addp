package migration

import (
	"context"
	"database/sql"
	"fmt"
)

const (
	migrationChecksumsTable             = "system.schema_migration_checksums"
	migrationChecksumsIntroducedVersion = uint(38)
)

func verifyRecordedMigrationChecksums(ctx context.Context, db *sql.DB, catalog Catalog, appliedVersion uint) error {
	exists, err := migrationChecksumTableExists(ctx, db)
	if err != nil {
		return err
	}
	if !exists {
		if appliedVersion >= migrationChecksumsIntroducedVersion {
			return fmt.Errorf("migration checksum table is missing at applied version %d", appliedVersion)
		}
		return nil
	}

	recorded, err := readRecordedMigrationChecksums(ctx, db)
	if err != nil {
		return err
	}
	return compareMigrationChecksums(catalog, appliedVersion, recorded, false)
}

func recordAndVerifyMigrationChecksums(ctx context.Context, db *sql.DB, catalog Catalog, appliedVersion uint) error {
	exists, err := migrationChecksumTableExists(ctx, db)
	if err != nil {
		return err
	}
	if !exists {
		if appliedVersion >= migrationChecksumsIntroducedVersion {
			return fmt.Errorf("migration checksum table is missing at applied version %d", appliedVersion)
		}
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration checksum transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	for _, migrationFile := range catalog.Files {
		if migrationFile.Version > appliedVersion {
			break
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO system.schema_migration_checksums (version, filename, sha256)
			VALUES ($1, $2, $3)
			ON CONFLICT (version) DO NOTHING
		`, migrationFile.Version, migrationFile.Name, migrationFile.SHA256); err != nil {
			return fmt.Errorf("record migration checksum %06d: %w", migrationFile.Version, err)
		}
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT version, filename, sha256
		FROM system.schema_migration_checksums
		ORDER BY version
	`)
	if err != nil {
		return fmt.Errorf("read migration checksums: %w", err)
	}
	recorded, err := scanRecordedMigrationChecksums(rows)
	if err != nil {
		return err
	}
	if err := compareMigrationChecksums(catalog, appliedVersion, recorded, true); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration checksums: %w", err)
	}
	return nil
}

func migrationChecksumTableExists(ctx context.Context, db *sql.DB) (bool, error) {
	var exists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, migrationChecksumsTable).Scan(&exists); err != nil {
		return false, fmt.Errorf("inspect migration checksum table: %w", err)
	}
	return exists, nil
}

type recordedMigrationChecksum struct {
	Version  uint
	Filename string
	SHA256   string
}

func readRecordedMigrationChecksums(ctx context.Context, db *sql.DB) ([]recordedMigrationChecksum, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT version, filename, sha256
		FROM system.schema_migration_checksums
		ORDER BY version
	`)
	if err != nil {
		return nil, fmt.Errorf("read migration checksums: %w", err)
	}
	return scanRecordedMigrationChecksums(rows)
}

type checksumRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

func scanRecordedMigrationChecksums(rows checksumRows) ([]recordedMigrationChecksum, error) {
	defer rows.Close()
	recorded := make([]recordedMigrationChecksum, 0)
	for rows.Next() {
		var item recordedMigrationChecksum
		if err := rows.Scan(&item.Version, &item.Filename, &item.SHA256); err != nil {
			return nil, fmt.Errorf("scan migration checksum: %w", err)
		}
		recorded = append(recorded, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate migration checksums: %w", err)
	}
	return recorded, nil
}

func compareMigrationChecksums(catalog Catalog, appliedVersion uint, recorded []recordedMigrationChecksum, requireComplete bool) error {
	if appliedVersion > catalog.LatestVersion {
		return fmt.Errorf("applied migration version %d exceeds embedded catalog %d", appliedVersion, catalog.LatestVersion)
	}
	byVersion := make(map[uint]recordedMigrationChecksum, len(recorded))
	for _, item := range recorded {
		if item.Version > appliedVersion {
			return fmt.Errorf("migration checksum version %d exceeds applied version %d", item.Version, appliedVersion)
		}
		byVersion[item.Version] = item
	}
	for _, migrationFile := range catalog.Files {
		if migrationFile.Version > appliedVersion {
			break
		}
		item, exists := byVersion[migrationFile.Version]
		if !exists {
			if requireComplete {
				return fmt.Errorf("migration checksum %06d is missing", migrationFile.Version)
			}
			continue
		}
		if item.Filename != migrationFile.Name || item.SHA256 != migrationFile.SHA256 {
			return fmt.Errorf(
				"applied migration %06d does not match embedded file: recorded %q %s, embedded %q %s",
				migrationFile.Version, item.Filename, item.SHA256, migrationFile.Name, migrationFile.SHA256,
			)
		}
	}
	return nil
}
