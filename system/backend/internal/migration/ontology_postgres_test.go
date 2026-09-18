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

func TestOntologyBackendForwardMigrationAgainstPostgres(t *testing.T) {
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
	before, after := migrationFilesBeforeAndThrough(t, "000153_iam_ontology_backend.up.sql")
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id=(SELECT id FROM system.service_principals WHERE name='addp-ontology')`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{DSN: dsn, FS: after, Root: DefaultMigrationsRoot}
	for range 2 {
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var assignments, permissions, userGrants int
	var current int64
	if err := db.QueryRow(`SELECT count(*) FROM system.role_assignments a JOIN system.roles r ON r.id=a.role_id JOIN system.service_principals s ON s.id=a.principal_id WHERE s.name='addp-ontology' AND r.role_key='platform.ontology_runtime' AND a.scope_type='platform' AND a.tenant_id IS NULL AND a.status='active'`).Scan(&assignments); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.permissions WHERE permission_key IN ('ontology.revision.read','ontology.revision.update') AND tenant_customizable AND NOT delegable AND allowed_scope_types=ARRAY['tenant']::text[]`).Scan(&permissions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id WHERE p.owner_module='ontology'`).Scan(&userGrants); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id=(SELECT id FROM system.service_principals WHERE name='addp-ontology')`).Scan(&current); err != nil {
		t.Fatal(err)
	}
	var keys string
	if err := db.QueryRow(`SELECT string_agg(p.permission_key,',' ORDER BY p.permission_key) FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE r.role_key='platform.ontology_runtime'`).Scan(&keys); err != nil {
		t.Fatal(err)
	}
	if assignments != 1 || permissions != 2 || userGrants != 0 || current != version+1 || keys != "system.runtime_registry.update" {
		t.Fatalf("assignments=%d permissions=%d implicit=%d version=%d/%d keys=%s", assignments, permissions, userGrants, current, version, keys)
	}
	_, semanticMigrations := migrationFilesBeforeAndThrough(t, "000154_iam_ontology_semantic_read.up.sql")
	runner.FS = semanticMigrations
	for range 2 {
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.permissions WHERE permission_key='ontology.semantic.read' AND tenant_customizable AND delegable AND allowed_scope_types=ARRAY['tenant']::text[]`).Scan(&permissions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id WHERE p.owner_module='ontology'`).Scan(&userGrants); err != nil {
		t.Fatal(err)
	}
	if permissions != 1 || userGrants != 0 {
		t.Fatalf("semantic permission=%d implicit grants=%d", permissions, userGrants)
	}
}
