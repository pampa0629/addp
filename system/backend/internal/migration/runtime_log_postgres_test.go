package migration

import (
	"context"
	"database/sql"
	"github.com/addp/system/internal/testsupport"
	"os"
	"testing"
	"time"
)

func TestRuntimeLogPermissionForwardMigrationAgainstPostgres(t *testing.T) {
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
	before, through := migrationFilesBeforeAndThrough(t, "000171_platform_module_runtime_log_permission.up.sql")
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
	var scopes, risk string
	var delegable, customizable bool
	if err = db.QueryRow(`SELECT array_to_string(allowed_scope_types,','),risk_level,delegable,tenant_customizable FROM system.permissions WHERE permission_key='platform.module_log.read'`).Scan(&scopes, &risk, &delegable, &customizable); err != nil {
		t.Fatal(err)
	}
	if scopes != "platform" || risk != "high" || delegable || customizable {
		t.Fatalf("unexpected permission %s/%s/%v/%v", scopes, risk, delegable, customizable)
	}
	rows, err := db.Query(`SELECT role.role_key FROM system.role_permissions rp JOIN system.roles role ON role.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='platform.module_log.read'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			t.Fatal(err)
		}
		if key != "platform.system_administrator" {
			t.Fatal(key)
		}
		count++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("role grants=%d", count)
	}
}
