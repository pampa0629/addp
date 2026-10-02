package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestFulfillmentHandlingPermissionForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reset := func() {
		t.Helper()
		if _, err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	before, through := migrationFilesBeforeAndThrough(t, "000178_engine_access_fulfillment_handling.up.sql")
	run := func(runner *Runner) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	run(&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot})
	userID, _ := seedInitializedMigrationTenant(t, db, "fulfillment-handling", "Fulfillment Handling")
	var previousVersion, previousAssignments int64
	if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", userID).Scan(&previousVersion); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM system.role_assignments").Scan(&previousAssignments); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	run(runner)
	run(runner)
	var count int64
	if err := db.QueryRow(`SELECT count(*) FROM system.permissions WHERE permission_key='system.engine_access_fulfillment.create' AND owner_module='system' AND action='create'
		AND risk_level='high' AND NOT delegable AND tenant_customizable AND allowed_scope_types=ARRAY['tenant']::text[] AND status='active'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("independent permission=%d %v", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='system.engine_access_fulfillment.create'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("implicit role permission=%d %v", count, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM system.role_assignments").Scan(&count); err != nil || count != previousAssignments {
		t.Fatalf("implicit assignments=%d %v", count, err)
	}
	if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", userID).Scan(&count); err != nil || count != previousVersion {
		t.Fatalf("unrelated authorization version=%d %v", count, err)
	}
	var dirty bool
	if err := db.QueryRow("SELECT version,dirty FROM system.schema_migrations").Scan(&count, &dirty); err != nil || count != 178 || dirty {
		t.Fatalf("migration=%d dirty=%v %v", count, dirty, err)
	}
}
