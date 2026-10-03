package migration

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestFulfillmentBasisForwardMigrationAgainstPostgres(t *testing.T) {
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
	before, through := migrationFilesBeforeAndThrough(t, "000180_catalog_fulfillment_basis_runtime.up.sql")
	run := func(runner *Runner) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	run(&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot})
	user, tenant := seedInitializedMigrationTenant(t, db, "fulfillment-basis", "Fulfillment Basis")
	var oldVersion int64
	if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", user).Scan(&oldVersion); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	run(runner)
	run(runner)
	var n int64
	for _, query := range []string{
		`SELECT count(*) FROM system.oauth_clients WHERE client_id='addp-system' AND client_secret_hash IS NULL AND status='disabled'`,
		`SELECT count(*) FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE r.role_key='tenant.system_runtime' AND p.permission_key='catalog.sharing_fulfillment.read' AND NOT p.tenant_customizable AND NOT p.delegable`,
		`SELECT count(*) FROM system.tenant_memberships m JOIN system.service_principals s ON s.id=m.principal_id WHERE s.name='addp-system' AND m.tenant_id=$1 AND m.status='active'`,
		`SELECT count(*) FROM system.role_assignments ra JOIN system.roles r ON r.id=ra.role_id JOIN system.service_principals s ON s.id=ra.principal_id WHERE s.name='addp-system' AND r.role_key='tenant.system_runtime' AND ra.tenant_id=$1 AND ra.status='active'`,
	} {
		var err error
		if strings.Contains(query, "$1") {
			err = db.QueryRow(query, tenant).Scan(&n)
		} else {
			err = db.QueryRow(query).Scan(&n)
		}
		if err != nil || n != 1 {
			t.Fatalf("runtime migration count=%d %v query=%s", n, err, query)
		}
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id JOIN system.roles r ON r.id=rp.role_id WHERE p.permission_key='catalog.sharing_fulfillment.read' AND r.role_key<>'tenant.system_runtime'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("implicit human permission=%d %v", n, err)
	}
	if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", user).Scan(&n); err != nil || n != oldVersion {
		t.Fatalf("unrelated User changed=%d %v", n, err)
	}
	var dirty bool
	if err := db.QueryRow("SELECT version,dirty FROM system.schema_migrations").Scan(&n, &dirty); err != nil || n != 180 || dirty {
		t.Fatalf("migration=%d dirty=%v %v", n, dirty, err)
	}
}
