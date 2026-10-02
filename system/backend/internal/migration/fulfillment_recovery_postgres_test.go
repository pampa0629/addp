package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestFulfillmentRecoveryPermissionForwardMigrationAgainstPostgres(t *testing.T) {
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
	before, through := migrationFilesBeforeAndThrough(t, "000176_engine_access_fulfillment_recovery.up.sql")
	run := func(files *Runner) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := files.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	run(&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot})
	userID, tenantID := seedInitializedMigrationTenant(t, db, "fulfillment-recovery", "Fulfillment Recovery")
	var serviceID int64
	if err := db.QueryRow("SELECT id FROM system.service_principals WHERE name='addp-catalog'").Scan(&serviceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.tenant_memberships
		(tenant_id, principal_id, status, source_type, joined_at, created_by_principal_id)
		VALUES ($1,$2,'active','bootstrap',now(),$3)`, tenantID, serviceID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.role_assignments
		(principal_id,role_id,scope_type,tenant_id,status,valid_from,source_type)
		SELECT $1,id,'tenant',$2,'active',now(),'bootstrap' FROM system.roles
		WHERE tenant_id IS NULL AND role_key='tenant.catalog_runtime'`, serviceID, tenantID); err != nil {
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
	serviceBefore, userBefore := version(serviceID), version(userID)
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	run(runner)
	run(runner)
	if serviceAfter, userAfter := version(serviceID), version(userID); serviceAfter != serviceBefore+1 || userAfter != userBefore {
		t.Fatalf("authorization versions: runtime %d -> %d, unrelated user %d -> %d", serviceBefore, serviceAfter, userBefore, userAfter)
	}
	var count int64
	if err := db.QueryRow(`SELECT count(*) FROM system.permissions WHERE permission_key='system.engine_access_fulfillment.execute'
		AND owner_module='system' AND action='execute' AND risk_level='high' AND NOT delegable AND NOT tenant_customizable
		AND allowed_scope_types=ARRAY['tenant']::text[] AND status='active'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("permission count=%d err=%v", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions b JOIN system.permissions p ON p.id=b.permission_id
		WHERE p.permission_key='system.engine_access_fulfillment.execute'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unexpected role grants=%d err=%v", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions b JOIN system.roles r ON r.id=b.role_id
		JOIN system.permissions p ON p.id=b.permission_id WHERE p.permission_key='system.engine_access_fulfillment.execute'
		AND r.tenant_id IS NULL AND r.role_key='tenant.catalog_runtime'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("runtime role grant=%d err=%v", count, err)
	}
	var migrationVersion int64
	var dirty bool
	if err := db.QueryRow("SELECT version,dirty FROM system.schema_migrations").Scan(&migrationVersion, &dirty); err != nil || migrationVersion != 176 || dirty {
		t.Fatalf("migration=%d/%v err=%v", migrationVersion, dirty, err)
	}
}
