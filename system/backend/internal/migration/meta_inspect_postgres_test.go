package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestMetaInspectRuntimeForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("ADDP_SYSTEM_POSTGRES_TEST_DSN is not set")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	before, through := migrationFilesBeforeAndThrough(t, "000215_meta_inspect_transfer_runtime.up.sql")
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	principal, tenant := seedInitializedMigrationTenant(t, db, "inspect-runtime", "Inspect Runtime")
	unaffected, _ := seedInitializedMigrationTenant(t, db, "inspect-unaffected", "Inspect Unaffected")
	var customRole int64
	if err := db.QueryRow(`INSERT INTO system.roles
        (tenant_id,role_key,name,role_type,allowed_scope_types,allowed_principal_types,immutable,status,created_by_principal_id)
        VALUES ($1,'custom.infra_inspector','Inspector','tenant_custom',ARRAY['tenant'],ARRAY['user'],false,'active',$2)
        RETURNING id`, tenant, principal).Scan(&customRole); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.role_permissions (role_id,permission_id,source_type,created_by_principal_id)
        SELECT $1,id,'tenant',$2 FROM system.permissions WHERE permission_key IN ('meta.inspect.execute','manager.data_item.read')`, customRole, principal); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.role_assignments
        (principal_id,role_id,scope_type,tenant_id,status,valid_from,source_type)
        SELECT $1,id,'tenant',$2,'active',now(),'bootstrap' FROM system.roles
        WHERE id=$3 OR (tenant_id IS NULL AND role_key='tenant.data_steward')`, principal, tenant, customRole); err != nil {
		t.Fatal(err)
	}
	version := func(id int64) int64 {
		t.Helper()
		var value int64
		if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", id).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	old, stable := version(principal), version(unaffected)
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	current := version(principal)
	if current <= old || version(unaffected) != stable {
		t.Fatalf("affected version=%d->%d; unrelated subject must remain at %d", old, current, stable)
	}
	var retained, removed, facts, otherPermission int
	if err := db.QueryRow(`SELECT count(*) FILTER (WHERE role.tenant_id IS NULL AND role.role_key='tenant.transfer_runtime'),
        count(*) FILTER (WHERE NOT (role.tenant_id IS NULL AND role.role_key='tenant.transfer_runtime'))
        FROM system.role_permissions binding JOIN system.roles role ON role.id=binding.role_id
        JOIN system.permissions permission ON permission.id=binding.permission_id
        WHERE permission.permission_key='meta.inspect.execute'`).Scan(&retained, &removed); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.permissions WHERE permission_key='meta.inspect.execute'
        AND allowed_scope_types=ARRAY['tenant']::text[] AND NOT tenant_customizable AND NOT delegable AND status='active'`).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions binding
        JOIN system.permissions permission ON permission.id=binding.permission_id
        WHERE binding.role_id=$1 AND permission.permission_key='manager.data_item.read'`, customRole).Scan(&otherPermission); err != nil {
		t.Fatal(err)
	}
	if retained != 1 || removed != 0 || facts != 1 || otherPermission != 1 {
		t.Fatalf("runtime=%d others=%d permission=%d preserved=%d", retained, removed, facts, otherPermission)
	}
	if _, err := db.Exec(`INSERT INTO system.role_permissions (role_id,permission_id,source_type,created_by_principal_id)
        SELECT $1,id,'tenant',$2 FROM system.permissions WHERE permission_key='meta.inspect.execute'`, customRole, principal); err == nil {
		t.Fatal("custom role regained the private runtime permission")
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if version(principal) != current {
		t.Fatal("migration rerun advanced the same authorization again")
	}
}
