package migration

import (
	"context"
	"database/sql"
	"github.com/addp/system/internal/testsupport"
	"os"
	"testing"
	"time"
)

func TestAdministratorGovernanceForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_SYSTEM_POSTGRES_TEST_DSN")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	before, after := migrationFilesBeforeAndThrough(t, "000222_platform_administrator_governance.up.sql")
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, role := range []string{"system", "security", "audit"} {
		id := createMigrationUser(t, db, "Governance "+role)
		ids[role] = id
		if _, err := db.Exec(`INSERT INTO system.role_assignments(principal_id,role_id,scope_type,source_type) SELECT $1,id,'platform','bootstrap' FROM system.roles WHERE role_key=$2`, id, "platform."+role+"_administrator"); err != nil {
			t.Fatal(err)
		}
	}
	pending := createPrivilegedChangeRequest(t, db, "platform_identity_suspend", ids["system"], 0, ids["security"])
	approved := createPrivilegedChangeRequest(t, db, "platform_identity_suspend", ids["audit"], 0, ids["security"])
	approvePrivilegedChangeRequest(t, db, approved, ids["system"])
	runner := &Runner{DSN: dsn, FS: after, Root: DefaultMigrationsRoot}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{pending, approved} {
		assertPrivilegedChangeStatus(t, db, id, "cancelled")
	}
	var oldGrants, newPermissions int
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key LIKE 'iam.platform_identity_change.%'`).Scan(&oldGrants); err != nil {
		t.Fatal(err)
	}
	if oldGrants != 0 {
		t.Fatal("old grants survived publication")
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.permissions WHERE permission_key LIKE 'iam.platform_administrator_change.%' AND status='active'`).Scan(&newPermissions); err != nil {
		t.Fatal(err)
	}
	if newPermissions != 4 {
		t.Fatalf("new permission count=%d", newPermissions)
	}
	for _, tc := range []struct {
		role  string
		count int
	}{{"system", 4}, {"security", 2}, {"audit", 3}} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE r.role_key=$1 AND p.permission_key LIKE 'iam.platform_administrator_change.%'`, "platform."+tc.role+"_administrator").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != tc.count {
			t.Fatalf("%s governance grants=%d expected=%d", tc.role, count, tc.count)
		}
	}
	var auditWrites int
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE r.role_key='platform.audit_administrator' AND p.action<>'read' AND p.permission_key NOT IN('audit.event.export','iam.platform_administrator_change.approve','iam.platform_administrator_change.reject')`).Scan(&auditWrites); err != nil {
		t.Fatal(err)
	}
	if auditWrites != 0 {
		t.Fatal("audit administrator acquired direct management writes")
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{pending, approved} {
		assertPrivilegedChangeStatus(t, db, id, "cancelled")
	}
}
