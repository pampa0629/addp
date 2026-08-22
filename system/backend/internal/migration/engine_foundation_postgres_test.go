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

	var firstID int64
	if err := db.QueryRow(`
		INSERT INTO system.engines (
			name, engine_type, connection_info, identity_key, lifecycle_state, is_builtin
		) VALUES (
			'Permanent Identity Probe', 'postgresql',
			'{"host":"engine-a","port":5432,"database":"probe"}'::jsonb,
			'{"host":"engine-a","port":"5432","database":"probe"}'::jsonb,
			'active', false
		)
		RETURNING id
	`).Scan(&firstID); err != nil {
		t.Fatalf("insert first Engine: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.engines
		SET lifecycle_state = 'deleted', connection_info = '{}'::jsonb,
		    deleted_at = now(), version = version + 1
		WHERE id = $1
	`, firstID); err != nil {
		t.Fatalf("soft delete first Engine: %v", err)
	}
	var nextID int64
	if err := db.QueryRow(`
		INSERT INTO system.engines (name, engine_type, connection_info, identity_key)
		VALUES (
			'Sequence Probe', 'postgresql',
			'{"host":"engine-b","port":5432,"database":"probe"}'::jsonb,
			'{"host":"engine-b","port":"5432","database":"probe"}'::jsonb
		)
		RETURNING id
	`).Scan(&nextID); err != nil {
		t.Fatalf("insert Engine after soft deletion: %v", err)
	}
	if nextID != firstID+1 {
		t.Fatalf("Engine ID after soft deletion = %d, want %d", nextID, firstID+1)
	}
	var lifecycle string
	if err := db.QueryRow(`SELECT lifecycle_state FROM system.engines WHERE id = $1`, firstID).Scan(&lifecycle); err != nil {
		t.Fatalf("read soft-deleted Engine tombstone: %v", err)
	}
	if lifecycle != "deleted" {
		t.Fatalf("first Engine lifecycle = %q, want deleted", lifecycle)
	}
	if _, err := db.Exec(`
		INSERT INTO system.engines (name, engine_type, connection_info, identity_key)
		VALUES (
			'Duplicate Identity Probe', 'postgresql',
			'{"host":"engine-a","port":5432,"database":"probe"}'::jsonb,
			'{"host":"engine-a","port":"5432","database":"probe"}'::jsonb
		)
	`); err == nil {
		t.Fatal("soft-deleted Engine identity was reused")
	}
}
