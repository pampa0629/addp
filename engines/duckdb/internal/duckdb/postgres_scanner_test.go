package duckdb

import (
	"crypto/sha256"
	"fmt"
	commonmodels "github.com/addp/common/models"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestPostgresScannerRejectsMismatchedFileBeforeLoad(t *testing.T) {
	work := t.TempDir()
	t.Setenv("TMPDIR", work)
	dir := t.TempDir()
	source := filepath.Join(dir, postgresScannerName)
	if err := os.WriteFile(source, []byte("replaced artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	db, conn, err := openQueryConnection(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer conn.Close()
	err = loadPostgresScanner(t.Context(), conn, source, strings.Repeat("0", 64))
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected a digest mismatch, got %v", err)
	}
	entries, err := os.ReadDir(work)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary scanner cleanup: entries=%v err=%v", entries, err)
	}
}

func TestScannerSnapshotSurvivesSourceReplacement(t *testing.T) {
	source := filepath.Join(t.TempDir(), postgresScannerName)
	path := filepath.Join(t.TempDir(), postgresScannerName)
	original := []byte("build artifact")
	if err := os.WriteFile(source, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := snapshotPostgresScanner(source, path, fmt.Sprintf("%x", sha256.Sum256(original))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("different artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(original) {
		t.Fatalf("load path after replacement: %q, %v", got, err)
	}
}

func TestPinnedPostgresScannerSession(t *testing.T) {
	if os.Getenv("ADDP_TEST_PINNED_DUCKDB_SCANNER") != "1" {
		t.Skip("requires the native scanner build gate")
	}
	db, conn, err := openQueryConnection(t.Context(), true)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer conn.Close()
	var unsigned, namedOnly, automaticInstall, automaticLoad bool
	if err := conn.QueryRowContext(t.Context(), `SELECT current_setting('allow_unsigned_extensions'),
		current_setting('pg_only_named_table'), current_setting('autoinstall_known_extensions'),
		current_setting('autoload_known_extensions')`).Scan(&unsigned, &namedOnly, &automaticInstall, &automaticLoad); err != nil {
		t.Fatal(err)
	}
	if unsigned || namedOnly || automaticInstall || automaticLoad {
		t.Fatalf("unexpected session settings: unsigned=%t namedOnly=%t autoInstall=%t autoLoad=%t", unsigned, namedOnly, automaticInstall, automaticLoad)
	}
	port, err := strconv.Atoi(os.Getenv("ADDP_TEST_DUCKDB_SCANNER_PORT"))
	if err != nil {
		t.Fatal(err)
	}
	engine := commonmodels.Engine{Name: "fixture", EngineType: "postgresql", ConnectionInfo: commonmodels.ConnectionInfo{
		"host": "127.0.0.1", "port": port, "database": "addp_test", "username": "scanner_reader", "password": "fixture_reader",
	}}
	if err := MountPostgres(t.Context(), conn, engine); err != nil {
		t.Fatal(err)
	}
	checkRows := func(want string) {
		t.Helper()
		var got string
		err := conn.QueryRowContext(t.Context(), "SELECT string_agg(id::VARCHAR, ',' ORDER BY id) FROM fixture.public.parcels").Scan(&got)
		if err != nil || got != want {
			t.Fatalf("parent table scan: got %q, want %q, err=%v", got, want, err)
		}
	}
	checkRows("1,2")
	if _, err := conn.ExecContext(t.Context(), "SET pg_only_named_table=true"); err != nil {
		t.Fatal(err)
	}
	checkRows("1")
}
