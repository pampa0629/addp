package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestEngineManagementReaderForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
		t.Fatal(err)
	}
	before, through := migrationFilesBeforeAndThrough(t, "000299_engine_management_reader.up.sql")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	seedInitializedMigrationTenant(t, db, "engine-reader-migration", "Engine reader migration")
	snapshot := func() string {
		t.Helper()
		var value string
		if err := db.QueryRow(`SELECT jsonb_build_object(
   'roles',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM system.roles r WHERE role_key <> 'tenant.engine_management_reader'),
   'bindings',(SELECT jsonb_agg(to_jsonb(b) ORDER BY b.role_id,b.permission_id) FROM system.role_permissions b JOIN system.roles r ON r.id=b.role_id WHERE r.role_key <> 'tenant.engine_management_reader'),
   'permissions',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM system.permissions p),
   'assignments',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM system.role_assignments a),
   'principals',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM system.principals p),
   'sessions',(SELECT jsonb_agg(to_jsonb(f) ORDER BY id) FROM system.refresh_token_families f),
   'delegations',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM system.engine_access_delegations d),
   'grants',(SELECT jsonb_agg(to_jsonb(g) ORDER BY request_id) FROM system.engine_access_grants g)
  )::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	original := snapshot()
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	for range 2 {
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if snapshot() != original {
			t.Fatal("reader publication or restart changed existing authorization facts")
		}
	}
	var roles, assignments int
	if err := db.QueryRow(`SELECT count(*) FROM system.roles WHERE tenant_id IS NULL AND role_key='tenant.engine_management_reader'
  AND role_type='tenant_builtin' AND immutable AND status='active'
  AND allowed_scope_types=ARRAY['tenant']::text[] AND allowed_principal_types=ARRAY['user']::text[]
  AND name_i18n_key='roles.tenant.engine_management_reader.name' AND description_i18n_key='roles.tenant.engine_management_reader.description'`).Scan(&roles); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id WHERE r.role_key='tenant.engine_management_reader'`).Scan(&assignments); err != nil {
		t.Fatal(err)
	}
	var permissions string
	if err := db.QueryRow(`SELECT string_agg(p.permission_key,',' ORDER BY p.permission_key) FROM system.role_permissions b
 JOIN system.roles r ON r.id=b.role_id JOIN system.permissions p ON p.id=b.permission_id
 WHERE r.role_key='tenant.engine_management_reader' AND b.source_type='product' AND p.status='active'`).Scan(&permissions); err != nil {
		t.Fatal(err)
	}
	if roles != 1 || assignments != 0 || permissions != "system.engine.read" {
		t.Fatalf("role/assignments/permissions=%d/%d/%s", roles, assignments, permissions)
	}
}
