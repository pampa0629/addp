package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestOntologyRuntimeForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("ADDP_SYSTEM_POSTGRES_TEST_DSN is required")
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
	before, after := migrationFilesBeforeAndThrough(t, "000152_iam_ontology_runtime.up.sql")
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	_, tenantID := seedInitializedMigrationTenant(t, db, "ontology-upgrade", "Ontology Upgrade")
	runner := &Runner{DSN: dsn, FS: after, Root: DefaultMigrationsRoot}
	for range 2 {
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var memberships, assignments, userGrants, clients int
	var permissions string
	if err := db.QueryRow(`SELECT count(*) FROM system.tenant_memberships m
		JOIN system.service_principals s ON s.id=m.principal_id
		WHERE s.name='addp-ontology' AND m.tenant_id=$1 AND m.status='active'`, tenantID).Scan(&memberships); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_assignments a
		JOIN system.service_principals s ON s.id=a.principal_id JOIN system.roles r ON r.id=a.role_id
		WHERE s.name='addp-ontology' AND r.role_key='tenant.ontology_runtime'
		AND a.scope_type='tenant' AND a.tenant_id=$1 AND a.status='active'`, tenantID).Scan(&assignments); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT string_agg(p.permission_key, ',' ORDER BY p.permission_key)
		FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id
		JOIN system.roles r ON r.id=rp.role_id WHERE r.role_key='tenant.ontology_runtime'`).Scan(&permissions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp
		JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='ontology.revision.publish'`).Scan(&userGrants); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.oauth_clients c
		JOIN system.service_principals s ON s.id=c.service_principal_id
		WHERE c.client_id='addp-ontology' AND s.name='addp-ontology' AND s.owner_scope='platform'
		AND c.status='disabled' AND c.client_secret_hash IS NULL`).Scan(&clients); err != nil {
		t.Fatal(err)
	}
	if memberships != 1 || assignments != 1 || clients != 1 || userGrants != 0 || permissions != "system.execution_authorization.execute" {
		t.Fatalf("ontology upgrade memberships=%d assignments=%d clients=%d implicit_grants=%d permissions=%q",
			memberships, assignments, clients, userGrants, permissions)
	}
}
