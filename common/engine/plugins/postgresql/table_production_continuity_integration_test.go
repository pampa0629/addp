package postgresql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lib/pq"
)

// This is a native concurrency/protocol characterization, not an issuance
// implementation. In particular, a live-check followed by an independent
// commit must NOT be mistaken for a cross-database atomic guarantee.
// The existing PrepareTableWrite prefix registers it in the full Common PG gate.
func TestIntegrationPostgresPrepareTableWriteProductionContinuity(t *testing.T) {
	db, _, _ := openPostgresPrepareIntegration(t, false)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	schema := fmt.Sprintf("addp_production_continuity_%d", time.Now().UnixNano())
	qualified := func(table string) string { return pq.QuoteIdentifier(schema) + "." + pq.QuoteIdentifier(table) }
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		// Explicitly owned objects only; no database creation or CASCADE cleanup.
		for _, table := range []string{"guarded", "unexpected", "replaced", "cancelled", "disconnected", "control_marker"} {
			if _, err := db.ExecContext(cleanup, "DROP TABLE IF EXISTS "+qualified(table)+" RESTRICT"); err != nil {
				t.Errorf("cleanup continuity table %s: %v", table, err)
			}
		}
		if _, err := db.ExecContext(cleanup, "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" RESTRICT"); err != nil {
			t.Errorf("cleanup continuity schema: %v", err)
		}
		var remaining bool
		if err := db.QueryRowContext(cleanup, "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname=$1)", schema).Scan(&remaining); err != nil || remaining {
			t.Errorf("continuity cleanup not proven: remaining=%v error=%v", remaining, err)
		}
	})
	create := func(t *testing.T, table string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, "CREATE TABLE "+qualified(table)+" (id bigint); INSERT INTO "+qualified(table)+" VALUES (7)"); err != nil {
			t.Fatal(err)
		}
	}
	lock := func(t *testing.T, tx *sql.Tx, table string) int64 {
		t.Helper()
		if _, err := tx.ExecContext(ctx, "LOCK TABLE "+qualified(table)+" IN ACCESS SHARE MODE"); err != nil {
			t.Fatal(err)
		}
		var oid int64
		if err := tx.QueryRowContext(ctx, "SELECT $1::regclass::oid", qualified(table)).Scan(&oid); err != nil {
			t.Fatal(err)
		}
		return oid
	}
	peerDDL := func(t *testing.T, statement string) error {
		t.Helper()
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, "SET LOCAL lock_timeout='500ms'"); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
		return tx.Commit()
	}

	t.Run("live guard blocks external DDL but permits row writes", func(t *testing.T) {
		create(t, "guarded")
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		oid := lock(t, tx, "guarded")
		for _, statement := range []string{
			"DROP TABLE " + qualified("guarded"),
			"TRUNCATE TABLE " + qualified("guarded"),
			"ALTER TABLE " + qualified("guarded") + " ADD COLUMN hidden bigint",
			"ALTER TABLE " + qualified("guarded") + " RENAME TO unexpected",
		} {
			var pgErr *pq.Error
			if err := peerDDL(t, statement); !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
				t.Fatalf("DDL not blocked by the actual lock: statement=%s error=%v", statement, err)
			}
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO "+qualified("guarded")+" VALUES (8); UPDATE "+qualified("guarded")+" SET id=9 WHERE id=8; DELETE FROM "+qualified("guarded")+" WHERE id=9"); err != nil {
			t.Fatalf("row-only write unexpectedly blocked: %v", err)
		}
		if current := lock(t, tx, "guarded"); current != oid {
			t.Fatalf("guarded relation changed: old=%d current=%d", oid, current)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := peerDDL(t, "TRUNCATE TABLE "+qualified("guarded")); err != nil {
			t.Fatalf("DDL still blocked after normal guard release: %v", err)
		}
	})

	t.Run("same name and shape before guard do not prove same object", func(t *testing.T) {
		create(t, "replaced")
		var original int64
		if err := db.QueryRowContext(ctx, "SELECT $1::regclass::oid", qualified("replaced")).Scan(&original); err != nil {
			t.Fatal(err)
		}
		if err := peerDDL(t, "DROP TABLE "+qualified("replaced")+"; CREATE TABLE "+qualified("replaced")+" (id bigint); INSERT INTO "+qualified("replaced")+" VALUES (99)"); err != nil {
			t.Fatal(err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if current := lock(t, tx, "replaced"); current == original {
			t.Fatal("replacement reused the observed relation OID in this test")
		}
		// This demonstrates a mismatch, not perpetual uniqueness of an OID.
	})

	t.Run("cancellation invalidates transaction and releases guard", func(t *testing.T) {
		create(t, "cancelled")
		guardCtx, stop := context.WithCancel(ctx)
		defer stop()
		tx, err := db.BeginTx(guardCtx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		lock(t, tx, "cancelled")
		stop()
		if err := tx.Commit(); err == nil {
			t.Fatal("cancelled guard reported a successful commit")
		}
		if err := peerDDL(t, "TRUNCATE TABLE "+qualified("cancelled")); err != nil {
			t.Fatalf("cancelled guard retained its lock: %v", err)
		}
	})

	t.Run("disconnect releases lock without fencing independent commit", func(t *testing.T) {
		create(t, "disconnected")
		create(t, "control_marker")
		guard, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer guard.Rollback()
		marker := schema + "_guard"
		var application string
		if err := guard.QueryRowContext(ctx, "SELECT pg_catalog.set_config('application_name', $1, true)", marker).Scan(&application); err != nil || application != marker {
			t.Fatalf("set test-owned backend identity: %v", err)
		}
		var pid int
		if err := guard.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
			t.Fatal(err)
		}
		lastConfirmed := lock(t, guard, "disconnected")
		control, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer control.Rollback()
		// Kill only this test's exact dedicated backend in this test database.
		// This does not stop a service, database, container or unrelated session.
		var terminated bool
		if err := db.QueryRowContext(ctx, `SELECT pg_terminate_backend(pid) FROM pg_catalog.pg_stat_activity
 WHERE pid=$1 AND datname=current_database() AND application_name=$2 AND backend_type='client backend'`, pid, marker).Scan(&terminated); err != nil || !terminated {
			t.Fatalf("terminate exact owned guard: terminated=%v error=%v", terminated, err)
		}
		if err := peerDDL(t, "DROP TABLE "+qualified("disconnected")+"; CREATE TABLE "+qualified("disconnected")+" (id bigint); INSERT INTO "+qualified("disconnected")+" VALUES (99)"); err != nil {
			t.Fatalf("disconnected guard retained DDL protection: %v", err)
		}
		// An intentionally naive control-side marker proves the counterexample.
		// It is NOT a real System command, Grant, authorization API or safe protocol.
		if _, err := control.ExecContext(ctx, "INSERT INTO "+qualified("control_marker")+" VALUES ($1)", lastConfirmed); err != nil {
			t.Fatal(err)
		}
		if err := control.Commit(); err != nil {
			t.Fatalf("independent control transaction unexpectedly fenced: %v", err)
		}
		if err := guard.Commit(); err == nil {
			t.Fatal("terminated guard falsely reported successful completion")
		}
		var current, recorded int64
		if err := db.QueryRowContext(ctx, "SELECT $1::regclass::oid", qualified("disconnected")).Scan(&current); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRowContext(ctx, "SELECT id FROM "+qualified("control_marker")+" WHERE id=$1", lastConfirmed).Scan(&recorded); err != nil || current == recorded || recorded != lastConfirmed {
			t.Fatalf("counterexample not reproduced: current=%d recorded=%d error=%v", current, recorded, err)
		}
		var backends int
		if err := db.QueryRowContext(ctx, "SELECT count(*) FROM pg_catalog.pg_stat_activity WHERE application_name=$1 AND datname=current_database()", marker).Scan(&backends); err != nil || backends != 0 {
			t.Fatalf("test-owned guard backend remains: count=%d error=%v", backends, err)
		}
		t.Log("counterexample reproduced: a stale successful lock observation does not fence an independent commit; automatic issuance remains disabled")
	})
}
