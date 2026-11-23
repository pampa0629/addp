package duckdb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// The native build pins this value in both the runtime and its startup verifier.
var postgresScannerSHA256 string

const postgresScannerName = "postgres_scanner.duckdb_extension"

func openQueryConnection(ctx context.Context, withPostgres bool) (*sql.DB, *sql.Conn, error) {
	dsn := ""
	if withPostgres {
		dsn = "?allow_unsigned_extensions=true&autoinstall_known_extensions=false&autoload_known_extensions=false"
	}
	db, err := sql.Open("duckdb", dsn)
	if err != nil {
		return nil, nil, err
	}
	conn, err := db.Conn(ctx)
	if err == nil && withPostgres {
		dir := os.Getenv("DUCKDB_EXTENSION_DIRECTORY")
		if dir == "" {
			err = errors.New("DuckDB PostgreSQL scanner directory is missing")
		} else {
			err = loadPostgresScanner(ctx, conn, filepath.Join(dir, postgresScannerName), postgresScannerSHA256)
		}
	}
	if err != nil {
		if conn != nil {
			conn.Close()
		}
		db.Close()
		return nil, nil, err
	}
	return db, conn, nil
}

func loadPostgresScanner(ctx context.Context, conn *sql.Conn, sourcePath, digest string) error {
	dir, err := os.MkdirTemp("", "addp-duckdb-scanner-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, postgresScannerName)
	if err := snapshotPostgresScanner(sourcePath, path, digest); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, "LOAD "+duckDBString(path)); err != nil {
		return fmt.Errorf("load verified DuckDB PostgreSQL scanner: %w", err)
	}
	_, err = conn.ExecContext(ctx, "SET allow_unsigned_extensions=false")
	return err
}

// LOAD takes a pathname. Hash the bytes written to a process-owned copy so
// replacing the packaged file cannot change what LOAD sees after verification.
func snapshotPostgresScanner(sourcePath, path, digest string) error {
	if len(digest) != sha256.Size*2 {
		return errors.New("DuckDB PostgreSQL scanner build digest is missing or invalid")
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	snapshot, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0400)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(snapshot, hash), source)
	if err := errors.Join(copyErr, snapshot.Close()); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != digest {
		return errors.New("DuckDB PostgreSQL scanner does not match the runtime build digest")
	}
	return nil
}
