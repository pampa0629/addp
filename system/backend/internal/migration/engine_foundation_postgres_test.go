package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
	_ "github.com/lib/pq"
)

func TestEngineFoundationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL 15+ database")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
		t.Fatalf("reset Engine foundation test schemas: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply System migrations: %v", err)
	}

	var tableName string
	if err := db.QueryRow(`SELECT to_regclass('system.engines')::text`).Scan(&tableName); err != nil {
		t.Fatalf("read Engine foundation table: %v", err)
	}
	if tableName != "system.engines" {
		t.Fatalf("Engine foundation table = %q, want system.engines", tableName)
	}

	if _, err := db.Exec(`
		INSERT INTO system.engines
			(id, tenant_id, name, engine_type, connection_info, lifecycle_state, is_builtin)
		VALUES (22, NULL, 'Restore Probe', 'postgresql', '{}'::json, 'active', false)
	`); err != nil {
		t.Fatalf("insert Engine with preserved ID: %v", err)
	}
	if _, err := db.Exec(`SELECT setval('system.engines_id_seq', (SELECT max(id) FROM system.engines))`); err != nil {
		t.Fatalf("advance Engine sequence after restore: %v", err)
	}
	var nextID int64
	if err := db.QueryRow(`
		INSERT INTO system.engines (name, engine_type, connection_info)
		VALUES ('Sequence Probe', 'postgresql', '{}'::json)
		RETURNING id
	`).Scan(&nextID); err != nil {
		t.Fatalf("insert Engine after preserved-ID restore: %v", err)
	}
	if nextID != 23 {
		t.Fatalf("Engine ID after restore = %d, want 23", nextID)
	}
}
