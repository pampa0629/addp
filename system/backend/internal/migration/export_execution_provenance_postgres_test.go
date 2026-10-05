package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestExportExecutionProvenanceMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires System PostgreSQL gate")
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
	before, after := migrationFilesBeforeAndThrough(t, "000187_export_execution_provenance.up.sql")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{DSN: dsn, FS: after, Root: DefaultMigrationsRoot}
	for range 2 {
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, owner := range []string{"manager", "develop"} {
		key := owner + ".export_provenance.read"
		var allowed bool
		if err := db.QueryRow(`SELECT owner_module=$2 AND action='read' AND risk_level='low' AND NOT delegable AND NOT tenant_customizable AND allowed_scope_types=ARRAY['tenant']::text[] AND status='active' FROM system.permissions WHERE permission_key=$1`, key, owner).Scan(&allowed); err != nil || !allowed {
			t.Fatalf("invalid permission %s err=%v", key, err)
		}
		rows, err := db.Query(`SELECT r.role_key FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key=$1 ORDER BY r.role_key`, key)
		if err != nil {
			t.Fatal(err)
		}
		var roles []string
		for rows.Next() {
			var role string
			if err := rows.Scan(&role); err != nil {
				t.Fatal(err)
			}
			roles = append(roles, role)
		}
		err = rows.Err()
		rows.Close()
		if err != nil || len(roles) != 1 || roles[0] != "tenant.transfer_runtime" {
			t.Fatalf("unexpected provenance grants: %v err=%v", roles, err)
		}
	}
	var registryRead bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE r.tenant_id IS NULL AND r.role_key='platform.transfer_runtime' AND p.permission_key='system.runtime_registry.read')`).Scan(&registryRead); err != nil || !registryRead {
		t.Fatalf("registry grant missing: err=%v", err)
	}
}
