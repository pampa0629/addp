package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestMetaAuthorizationFactsForwardMigrationAgainstPostgres(t *testing.T) {
	testAuthorizationFactsMigration(t, func(db *sql.DB) func() {
		user, tenant := seedInitializedMigrationTenant(t, db, "authorization-facts", "Authorization facts")
		// This tenant is created after the earlier runtime provisioning migration.
		// Model its existing System service assignment before adding the permission.
		if _, err := db.Exec(`
			INSERT INTO system.tenant_memberships
			    (tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id)
			SELECT $1, id, 'active', 'bootstrap', now(), $2
			FROM system.service_principals WHERE name='addp-system';
		`, tenant, user); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`
			INSERT INTO system.role_assignments
			    (principal_id, role_id, scope_type, tenant_id, status, valid_from, source_type)
			SELECT s.id, r.id, 'tenant', $1, 'active', now(), 'bootstrap'
			FROM system.service_principals s CROSS JOIN system.roles r
			WHERE s.name='addp-system' AND r.tenant_id IS NULL AND r.role_key='tenant.system_runtime'
		`, tenant); err != nil {
			t.Fatal(err)
		}
		var userVersion, serviceVersion int64
		if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", user).Scan(&userVersion); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow("SELECT p.authorization_version FROM system.principals p JOIN system.service_principals s ON s.id=p.id WHERE s.name='addp-system'").Scan(&serviceVersion); err != nil {
			t.Fatal(err)
		}
		var family int64
		if err := db.QueryRow(`
			INSERT INTO system.refresh_token_families
			    (protocol_request_id, principal_id, context_type, tenant_membership_id,
			     issued_authorization_version, client_id, auth_type, audiences, scopes,
			     authentication_methods, assurance_level, authenticated_at, expires_at)
			SELECT '32000000-0000-0000-0000-000000000205', s.id, 'tenant', m.id,
			       $2, 'addp-system', 'oauth', ARRAY['addp.api'], ARRAY['addp.api'],
			       ARRAY['service_secret'], 'not_applicable', now(), now()+interval '1 hour'
			FROM system.service_principals s JOIN system.tenant_memberships m ON m.principal_id=s.id
			WHERE s.name='addp-system' AND m.tenant_id=$1 RETURNING id
		`, tenant, serviceVersion).Scan(&family); err != nil {
			t.Fatal(err)
		}
		return func() {
			var n int64
			for _, query := range []string{
				`SELECT count(*) FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='meta.authorization_facts.read' AND r.role_key='tenant.system_runtime' AND p.allowed_scope_types=ARRAY['tenant']::text[] AND NOT p.delegable AND NOT p.tenant_customizable`,
			} {
				if err := db.QueryRow(query).Scan(&n); err != nil || n != 1 {
					t.Fatalf("runtime fact permission=%d err=%v", n, err)
				}
			}
			if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='meta.authorization_facts.read' AND r.role_key<>'tenant.system_runtime'`).Scan(&n); err != nil || n != 0 {
				t.Fatal("facts permission spread beyond System runtime", err)
			}
			if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", user).Scan(&n); err != nil || n != userVersion {
				t.Fatal("unrelated human authority changed", err)
			}
			if err := db.QueryRow("SELECT p.authorization_version FROM system.principals p JOIN system.service_principals s ON s.id=p.id WHERE s.name='addp-system'").Scan(&n); err != nil || n <= serviceVersion {
				t.Fatal("assigned service authority was not invalidated", err)
			}
			if err := db.QueryRow("SELECT count(*) FROM system.refresh_token_families WHERE id=$1 AND revoked_at IS NOT NULL AND revoked_reason='authorization_catalog_changed'", family).Scan(&n); err != nil || n != 1 {
				t.Fatal("existing affected token family was not revoked", err)
			}
			if err := db.QueryRow("SELECT count(*) FROM system.engine_access_grants").Scan(&n); err != nil || n != 0 {
				t.Fatal("fact projection created content grants", err)
			}
		}
	})
}

func testAuthorizationFactsMigration(t *testing.T, seed func(*sql.DB) func()) {
	t.Helper()
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
	reset := func() {
		if _, err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	before, through := migrationFilesBeforeAndThrough(t, "000205_meta_authorization_facts.up.sql")
	run := func(runner *Runner) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	run(&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot})
	verify := seed(db)
	var countBefore int64
	if err := db.QueryRow("SELECT count(*) FROM system.permissions").Scan(&countBefore); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	run(runner)
	run(runner)
	var version, countAfter int64
	var dirty bool
	if err := db.QueryRow("SELECT version,dirty FROM system.schema_migrations").Scan(&version, &dirty); err != nil || version != 205 || dirty {
		t.Fatal("unexpected migration state", err)
	}
	if err := db.QueryRow("SELECT count(*) FROM system.permissions").Scan(&countAfter); err != nil || countAfter != countBefore+1 {
		t.Fatal("runtime permission was not published exactly once", err)
	}
	verify()
}
