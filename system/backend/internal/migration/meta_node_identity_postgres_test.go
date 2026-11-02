package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestMetaNodeIdentityForwardMigrationAgainstPostgres(t *testing.T) {
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
		t.Helper()
		if _, err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	before, through := migrationFilesBeforeAndThrough(t, "000265_meta_node_identity_read.up.sql")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	var roleGrantsBefore, sourceGrantsBefore int
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions`).Scan(&roleGrantsBefore); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.engine_access_grants`).Scan(&sourceGrantsBefore); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := (&Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var owner, action, risk, scope, role string
	var delegable, customizable bool
	if err := db.QueryRow(`SELECT owner_module,action,risk_level,delegable,tenant_customizable,array_to_string(allowed_scope_types,',')
FROM system.permissions WHERE permission_key='meta.node_identity.read'`).Scan(&owner, &action, &risk, &delegable, &customizable, &scope); err != nil || owner != "meta" || action != "read" || risk != "low" || delegable || customizable || scope != "tenant" {
		t.Fatalf("invalid machine permission: %s/%s/%s/%v/%v/%s err=%v", owner, action, risk, delegable, customizable, scope, err)
	}
	if err := db.QueryRow(`SELECT string_agg(r.role_key,',' ORDER BY r.role_key) FROM system.roles r
JOIN system.role_permissions rp ON rp.role_id=r.id JOIN system.permissions p ON p.id=rp.permission_id
WHERE p.permission_key='meta.node_identity.read'`).Scan(&role); err != nil || role != "tenant.system_runtime" {
		t.Fatalf("node facts escaped fixed runtime role: %s %v", role, err)
	}
	var roleGrantsAfter, sourceGrantsAfter int
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions`).Scan(&roleGrantsAfter); err != nil || roleGrantsAfter != roleGrantsBefore+1 {
		t.Fatalf("unexpected role grants: before=%d after=%d err=%v", roleGrantsBefore, roleGrantsAfter, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.engine_access_grants`).Scan(&sourceGrantsAfter); err != nil || sourceGrantsAfter != sourceGrantsBefore {
		t.Fatalf("node fact publication created data access: before=%d after=%d err=%v", sourceGrantsBefore, sourceGrantsAfter, err)
	}
}
