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
	exerciseEngineAccessRolePublication(t, "000194_engine_access_delegation_administrator.up.sql", "tenant.engine_access_delegation_administrator",
		"iam.tenant_membership.read,system.engine.read,system.engine_access_delegation.create,system.engine_access_delegation.read,system.engine_access_delegation.revoke")
}

func TestSourceDataAuthorizerRoleForwardMigrationAgainstPostgres(t *testing.T) {
	exerciseEngineAccessRolePublication(t, "000197_source_data_authorizer.up.sql", "tenant.source_data_authorizer",
		"iam.department.read,iam.project_group.read,iam.tenant_membership.read,system.engine.read,system.engine_access_approval_requirement.initialize,system.engine_access_approval_requirement.read,system.engine_access_grant.create,system.engine_access_grant.read,system.engine_access_grant.revoke,system.engine_catalog.read")
}

func TestEngineAuthorizationAdministratorForwardMigrationAgainstPostgres(t *testing.T) {
	exerciseEngineAccessRolePublication(t, "000198_engine_authorization_administrator.up.sql", "tenant.engine_access_delegation_administrator",
		"iam.department.read,iam.project_group.read,iam.tenant_membership.read,system.engine.read,system.engine_access_approval_requirement.initialize,system.engine_access_approval_requirement.read,system.engine_access_delegation.create,system.engine_access_delegation.read,system.engine_access_delegation.revoke,system.engine_access_grant.create,system.engine_access_grant.read,system.engine_access_grant.revoke,system.engine_catalog.read")
}

func TestEngineAuthorizationAdministratorAssignedSessionRefreshAgainstPostgres(t *testing.T) {
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
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
		t.Fatal(err)
	}
	before, through := migrationFilesBeforeAndThrough(t, "000198_engine_authorization_administrator.up.sql")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	user, tenant := seedInitializedMigrationTenant(t, db, "engine-admin-migration", "Engine admin migration")
	if _, err := db.Exec(`INSERT INTO system.role_assignments(principal_id,role_id,scope_type,tenant_id,status,valid_from,source_type)
	    SELECT $1,id,'tenant',$2,'active',now(),'bootstrap' FROM system.roles WHERE tenant_id IS NULL AND role_key='tenant.engine_access_delegation_administrator'`, user, tenant); err != nil {
		t.Fatal(err)
	}
	var membership, priorVersion, family int64
	if err := db.QueryRow(`SELECT m.id,p.authorization_version FROM system.tenant_memberships m JOIN system.principals p ON p.id=m.principal_id WHERE p.id=$1 AND m.tenant_id=$2`, user, tenant).Scan(&membership, &priorVersion); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO system.refresh_token_families(principal_id,context_type,tenant_membership_id,issued_authorization_version,client_id,auth_type,audiences,scopes,authentication_methods,assurance_level,authenticated_at,expires_at)
	    VALUES($1,'tenant',$2,$3,'addp-web','first_party',ARRAY['addp.api'],ARRAY[]::text[],ARRAY['password'],'aal1',now()-interval '1 minute',now()+interval '1 hour') RETURNING id`, user, membership, priorVersion).Scan(&family); err != nil {
		t.Fatal(err)
	}
	var priorAssignments int
	if err := db.QueryRow(`SELECT count(*) FROM system.role_assignments`).Scan(&priorAssignments); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	var currentVersion int64
	var reason string
	var assignments, delegations, grants int
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id=$1`, user).Scan(&currentVersion); err != nil {
		t.Fatal(err)
	}
	if currentVersion <= priorVersion {
		t.Fatal("changed administrator permissions did not advance authorization version")
	}
	if err := db.QueryRow(`SELECT revoked_reason FROM system.refresh_token_families WHERE id=$1 AND revoked_at IS NOT NULL`, family).Scan(&reason); err != nil || reason != "authorization_catalog_changed" {
		t.Fatalf("old session remains usable: %s %v", reason, err)
	}
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM system.role_assignments),(SELECT count(*) FROM system.engine_access_delegations),(SELECT count(*) FROM system.engine_access_grants)`).Scan(&assignments, &delegations, &grants); err != nil {
		t.Fatal(err)
	}
	if assignments != priorAssignments || delegations != 0 || grants != 0 {
		t.Fatalf("migration manufactured access: %d %d %d", assignments, delegations, grants)
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	var repeatedVersion int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id=$1`, user).Scan(&repeatedVersion); err != nil || repeatedVersion != currentVersion {
		t.Fatalf("second startup changed authorization: %d %v", repeatedVersion, err)
	}
}

func exerciseEngineAccessRolePublication(t *testing.T, migrationFile, roleKey, want string) {
	t.Helper()
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
	before, through := migrationFilesBeforeAndThrough(t, migrationFile)
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}

	// The new template must not change any previously published role or identity.
	snapshot := func() string {
		t.Helper()
		var value string
		if err := db.QueryRow(`SELECT jsonb_build_object(
		    'roles', (SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM system.roles r
		              WHERE r.role_key <> $1),
		    'bindings', (SELECT jsonb_agg(to_jsonb(b) ORDER BY b.role_id, b.permission_id)
		                 FROM system.role_permissions b JOIN system.roles r ON r.id = b.role_id
		                 WHERE r.role_key <> $1),
		    'assignments', (SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM system.role_assignments a),
		    'principals', (SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM system.principals p),
		    'sessions', (SELECT jsonb_agg(to_jsonb(f) ORDER BY f.id) FROM system.refresh_token_families f),
		    'delegations', (SELECT jsonb_agg(to_jsonb(d) ORDER BY d.id) FROM system.engine_access_delegations d),
		    'grants', (SELECT jsonb_agg(to_jsonb(g) ORDER BY g.request_id) FROM system.engine_access_grants g),
		    'requirements', (SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM system.engine_access_approval_requirements r)
		)::text`, roleKey).Scan(&value); err != nil {
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
	    WHERE tenant_id IS NULL AND role_key = $1
	      AND role_type = 'tenant_builtin' AND status = 'active' AND immutable
	      AND allowed_scope_types = ARRAY['tenant']::text[]
	      AND allowed_principal_types = ARRAY['user']::text[]
	      AND name_i18n_key = 'roles.' || $1 || '.name'
	      AND description_i18n_key = 'roles.' || $1 || '.description'`, roleKey).Scan(&roleCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_assignments a
	    JOIN system.roles r ON r.id = a.role_id
	    WHERE r.role_key = $1`, roleKey).Scan(&assignmentCount); err != nil {
		t.Fatal(err)
	}
	if roleCount != 1 || assignmentCount != 0 {
		t.Fatalf("published role count = %d, automatic assignments = %d; want 1, 0", roleCount, assignmentCount)
	}
	var permissions string
	if err := db.QueryRow(`SELECT string_agg(p.permission_key, ',' ORDER BY p.permission_key COLLATE "C")
	    FROM system.role_permissions b JOIN system.roles r ON r.id = b.role_id
	    JOIN system.permissions p ON p.id = b.permission_id
	    WHERE r.role_key = $1
	      AND b.source_type = 'product' AND p.status = 'active'`, roleKey).Scan(&permissions); err != nil {
		t.Fatal(err)
	}
	if permissions != want {
		t.Fatalf("%s permissions = %q, want %q", roleKey, permissions, want)
	}
	// A subsequent startup is a no-op, not a second publication or an assignment.
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if snapshot() != original {
		t.Fatal("restarting the migration runner changed existing authorization facts")
	}
}
