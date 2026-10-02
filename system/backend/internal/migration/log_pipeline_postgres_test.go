package migration

import (
	"context"
	"database/sql"
	"github.com/addp/system/internal/testsupport"
	"os"
	"testing"
	"time"
)

func TestPlatformLogPipelineForwardMigrationAgainstPostgres(t *testing.T) {
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
	before, through := migrationFilesBeforeAndThrough(t, "000173_platform_log_pipeline.up.sql")
	run := func(r *Runner) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := r.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	run(&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot})
	run(&Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot})
	run(&Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot})
	rows, err := db.Query(`SELECT r.role_key,p.permission_key,array_to_string(p.allowed_scope_types,','),p.delegable,p.tenant_customizable FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key LIKE 'monitor.log_%' OR p.permission_key='audit.event.create' ORDER BY r.role_key,p.permission_key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	expected := map[string]bool{
		"platform.system_administrator:monitor.log_pipeline.read":       true,
		"platform.system_administrator:monitor.log_pipeline.update":     true,
		"platform.system_administrator:monitor.log_notification.read":   true,
		"platform.system_administrator:monitor.log_notification.update": true,
		"platform.log_observer_runtime:monitor.log_observation.create":  true,
		"platform.monitor_runtime:audit.event.create":                   true,
	}
	for rows.Next() {
		var role, permission, scope string
		var delegable, customizable bool
		if err := rows.Scan(&role, &permission, &scope, &delegable, &customizable); err != nil {
			t.Fatal(err)
		}
		key := role + ":" + permission
		if !expected[key] || scope != "platform" || delegable || customizable {
			t.Fatalf("unexpected grant %s %s %v %v", key, scope, delegable, customizable)
		}
		delete(expected, key)
	}
	if rows.Err() != nil || len(expected) != 0 {
		t.Fatalf("missing grants %#v / %v", expected, rows.Err())
	}
	var name, owner, clientStatus, scope, principalType string
	var secret sql.NullString
	err = db.QueryRow(`SELECT s.name,s.owner_scope,c.status,c.client_secret_hash,a.scope_type,p.principal_type FROM system.oauth_clients c JOIN system.service_principals s ON s.id=c.service_principal_id JOIN system.principals p ON p.id=s.id JOIN system.role_assignments a ON a.principal_id=s.id JOIN system.roles r ON r.id=a.role_id WHERE c.client_id='addp-log-observer' AND r.role_key='platform.log_observer_runtime' AND a.status='active'`).Scan(&name, &owner, &clientStatus, &secret, &scope, &principalType)
	if err != nil {
		t.Fatal(err)
	}
	if name != "addp-log-observer" || owner != "platform" || clientStatus != "disabled" || secret.Valid || scope != "platform" || principalType != "service_principal" {
		t.Fatalf("unexpected observer bootstrap: %s/%s/%s/%v/%s/%s", name, owner, clientStatus, secret.Valid, scope, principalType)
	}
}
