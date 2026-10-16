package migration

import (
	"context"
	"database/sql"
	"github.com/addp/system/internal/testsupport"
	"os"
	"testing"
	"time"
)

func TestStandardPlatformUnitsForwardMigrationAgainstPostgres(t *testing.T) {
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
	before, through := migrationFilesBeforeAndThrough(t, "000212_standard_platform_units.up.sql")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	var principal, prior, family int64
	if err := db.QueryRow("INSERT INTO system.principals(principal_type) VALUES('user') RETURNING id").Scan(&principal); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO system.users(id,display_name) VALUES($1,'Unit directory administrator')", principal); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.role_assignments(principal_id,role_id,scope_type,status,valid_from,source_type) SELECT $1,id,'platform','active',statement_timestamp(),'bootstrap' FROM system.roles WHERE role_key='platform.system_administrator' AND tenant_id IS NULL`, principal); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", principal).Scan(&prior); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO system.refresh_token_families(principal_id,context_type,issued_authorization_version,client_id,auth_type,audiences,scopes,authentication_methods,assurance_level,authenticated_at,expires_at) VALUES($1,'platform',$2,'addp-web','first_party',ARRAY['addp.api'],ARRAY[]::text[],ARRAY['password','totp'],'aal2',now()-interval '1 minute',now()+interval '1 hour') RETURNING id`, principal, prior).Scan(&family); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE r.tenant_id IS NULL AND r.role_key='platform.system_administrator' AND p.permission_key IN ('standard.unit.create','standard.unit.delete','standard.unit.read','standard.unit.update') AND p.allowed_scope_types=ARRAY['platform','tenant','department','project_group']::text[]`).Scan(&count); err != nil || count != 4 {
		t.Fatalf("platform publication: count=%d err=%v", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE r.role_key IN ('platform.audit_administrator','platform.security_administrator') AND p.permission_key LIKE 'standard.unit.%'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("expanded another platform role", count, err)
	}
	var current int64
	var revoked bool
	if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", principal).Scan(&current); err != nil || current <= prior {
		t.Fatal("authorization version not advanced", current, prior, err)
	}
	if err := db.QueryRow("SELECT revoked_at IS NOT NULL FROM system.refresh_token_families WHERE id=$1", family).Scan(&revoked); err != nil || !revoked {
		t.Fatal("old family was not revoked", err)
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	var repeated int64
	if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", principal).Scan(&repeated); err != nil || repeated != current {
		t.Fatal("repeat migration changed authorization", err)
	}
}
