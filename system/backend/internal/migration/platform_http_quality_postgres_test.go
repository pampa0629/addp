package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestPlatformHTTPQualityForwardMigrationAgainstPostgres(t *testing.T) {
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
	before, through := migrationFilesBeforeAndThrough(t, "000227_platform_http_quality_permissions.up.sql")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	var principal, prior, family int64
	if err := db.QueryRow("INSERT INTO system.principals(principal_type) VALUES('user') RETURNING id").Scan(&principal); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO system.users(id,display_name) VALUES($1,'Platform HTTP Administrator')", principal); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.role_assignments(principal_id,role_id,scope_type,status,valid_from,source_type)
 SELECT $1,id,'platform','active',statement_timestamp(),'bootstrap' FROM system.roles WHERE role_key='platform.system_administrator' AND tenant_id IS NULL`, principal); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", principal).Scan(&prior); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO system.refresh_token_families(principal_id,context_type,issued_authorization_version,client_id,auth_type,audiences,scopes,authentication_methods,assurance_level,authenticated_at,expires_at)
 VALUES($1,'platform',$2,'addp-web','first_party',ARRAY['addp.api'],ARRAY[]::text[],ARRAY['password','totp'],'aal2',now()-interval '1 minute',now()+interval '1 hour') RETURNING id`, principal, prior).Scan(&family); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var value string
		err := db.QueryRow(`SELECT jsonb_build_object(
   'roles',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM system.roles r),
   'bindings',(SELECT jsonb_agg(to_jsonb(b) ORDER BY role_id,permission_id) FROM system.role_permissions b JOIN system.roles r ON r.id=b.role_id WHERE r.role_key<>'platform.system_administrator'),
   'principals',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM system.principals p WHERE id<>$1),
   'assignments',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM system.role_assignments a),
   'clients',(SELECT jsonb_agg(to_jsonb(c) ORDER BY client_id) FROM system.oauth_clients c),
   'permissions',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM system.permissions p WHERE permission_key NOT IN ('monitor.http_observation.read','monitor.platform_http_rule.create','monitor.platform_http_rule.delete','monitor.platform_http_rule.read','monitor.platform_http_rule.update'))
  )::text`, principal).Scan(&value)
		if err != nil {
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
		t.Fatal("publication changed unrelated roles, permissions, identity, assignments or OAuth clients")
	}
	var n int
	for _, query := range []string{
		`SELECT count(*) FROM system.permissions p JOIN system.role_permissions rp ON rp.permission_id=p.id JOIN system.roles r ON r.id=rp.role_id WHERE p.permission_key IN ('monitor.http_observation.read','monitor.platform_http_rule.create','monitor.platform_http_rule.delete','monitor.platform_http_rule.read','monitor.platform_http_rule.update') AND p.status='active' AND p.allowed_scope_types=ARRAY['platform']::text[] AND NOT p.delegable AND NOT p.tenant_customizable AND r.role_key='platform.system_administrator' AND r.tenant_id IS NULL`,
		`SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key IN ('monitor.http_observation.read','monitor.platform_http_rule.create','monitor.platform_http_rule.delete','monitor.platform_http_rule.read','monitor.platform_http_rule.update')`,
	} {
		if err := db.QueryRow(query).Scan(&n); err != nil || n != 5 {
			t.Fatal("new permissions missing or expanded to another role", n, err)
		}
	}
	var current, repeated int64
	var reason string
	if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", principal).Scan(&current); err != nil || current <= prior {
		t.Fatal("authorization version not advanced", err)
	}
	if err := db.QueryRow("SELECT revoked_reason FROM system.refresh_token_families WHERE id=$1 AND revoked_at IS NOT NULL", family).Scan(&reason); err != nil || reason != "authorization_catalog_changed" {
		t.Fatal("old refresh family usable", reason, err)
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", principal).Scan(&repeated); err != nil || repeated != current {
		t.Fatal("repeated migration changed version", err)
	}
}
