package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestEngineAccessDelegationRoleForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN to a disposable PostgreSQL database")
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	before, through := migrationFilesBeforeAndThrough(t, "000194_engine_access_delegation_administrator.up.sql")
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}

	// The new template must not change any previously published role or identity.
	snapshot := func() string {
		t.Helper()
		var value string
		if err := db.QueryRow(`SELECT jsonb_build_object(
		    'roles', (SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM system.roles r
		              WHERE r.role_key <> 'tenant.engine_access_delegation_administrator'),
		    'bindings', (SELECT jsonb_agg(to_jsonb(b) ORDER BY b.role_id, b.permission_id)
		                 FROM system.role_permissions b JOIN system.roles r ON r.id = b.role_id
		                 WHERE r.role_key <> 'tenant.engine_access_delegation_administrator'),
		    'assignments', (SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM system.role_assignments a),
		    'principals', (SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM system.principals p),
		    'sessions', (SELECT jsonb_agg(to_jsonb(f) ORDER BY f.id) FROM system.refresh_token_families f)
		)::text`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	original := snapshot()
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if snapshot() != original {
		t.Fatal("publishing the dedicated role changed existing roles, assignments, identities or sessions")
	}

	var roleCount, assignmentCount int
	if err := db.QueryRow(`SELECT count(*) FROM system.roles
	    WHERE tenant_id IS NULL AND role_key = 'tenant.engine_access_delegation_administrator'
	      AND role_type = 'tenant_builtin' AND status = 'active' AND immutable
	      AND allowed_scope_types = ARRAY['tenant']::text[]
	      AND allowed_principal_types = ARRAY['user']::text[]
	      AND name_i18n_key = 'roles.tenant.engine_access_delegation_administrator.name'
	      AND description_i18n_key = 'roles.tenant.engine_access_delegation_administrator.description'`).Scan(&roleCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_assignments a
	    JOIN system.roles r ON r.id = a.role_id
	    WHERE r.role_key = 'tenant.engine_access_delegation_administrator'`).Scan(&assignmentCount); err != nil {
		t.Fatal(err)
	}
	if roleCount != 1 || assignmentCount != 0 {
		t.Fatalf("published role count = %d, automatic assignments = %d; want 1, 0", roleCount, assignmentCount)
	}
	var permissions string
	if err := db.QueryRow(`SELECT string_agg(p.permission_key, ',' ORDER BY p.permission_key COLLATE "C")
	    FROM system.role_permissions b JOIN system.roles r ON r.id = b.role_id
	    JOIN system.permissions p ON p.id = b.permission_id
	    WHERE r.role_key = 'tenant.engine_access_delegation_administrator'
	      AND b.source_type = 'product' AND p.status = 'active'`).Scan(&permissions); err != nil {
		t.Fatal(err)
	}
	const want = "iam.tenant_membership.read,system.engine.read,system.engine_access_delegation.create,system.engine_access_delegation.read,system.engine_access_delegation.revoke"
	if permissions != want {
		t.Fatalf("delegation administrator permissions = %q, want %q", permissions, want)
	}
	// A subsequent startup is a no-op, not a second publication or an assignment.
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if snapshot() != original {
		t.Fatal("restarting the migration runner changed existing authorization facts")
	}
}
