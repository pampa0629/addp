package migration

import (
	"context"
	"database/sql"
	"github.com/addp/system/internal/testsupport"
	"os"
	"testing"
	"time"
)

func TestQualityPlansForwardMigrationAgainstPostgres(t *testing.T) {
	testQualityPermissionsMigration(t, "000147_iam_quality_plans.up.sql", false)
}
func TestQualityRulesForwardMigrationAgainstPostgres(t *testing.T) {
	testQualityPermissionsMigration(t, "000148_iam_quality_rules.up.sql", true)
}
func testQualityPermissionsMigration(t *testing.T, target string, reusable bool) {
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
	if _, err = db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	before, after := migrationFilesBeforeAndThrough(t, target)
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	principal, tenant := seedInitializedMigrationTenant(t, db, "quality-plans", "Quality Plans")
	if _, err := db.Exec(`INSERT INTO system.role_assignments(principal_id,role_id,scope_type,tenant_id,status,valid_from,source_type)
 SELECT $1,id,'tenant',$2,'active',now(),'bootstrap' FROM system.roles WHERE tenant_id IS NULL AND role_key='tenant.governance_manager'`, principal, tenant); err != nil {
		t.Fatal(err)
	}
	var version int64
	if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", principal).Scan(&version); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{DSN: dsn, FS: after, Root: DefaultMigrationsRoot}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	var afterVersion int64
	if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", principal).Scan(&afterVersion); err != nil || afterVersion <= version {
		t.Fatalf("authorization version %d -> %d, err=%v", version, afterVersion, err)
	}
	var retiredActive, retiredBindings, newGrants int
	if err := db.QueryRow(`SELECT count(*) FROM system.permissions WHERE status='active' AND permission_key ~ '^quality\.(check_task|rule_application|data_validation)\.'`).Scan(&retiredActive); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions b JOIN system.permissions p ON p.id=b.permission_id WHERE p.permission_key ~ '^quality\.(check_task|rule_application|data_validation)\.'`).Scan(&retiredBindings); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions b JOIN system.permissions p ON p.id=b.permission_id JOIN system.roles r ON r.id=b.role_id WHERE r.role_key='tenant.governance_manager' AND p.permission_key LIKE 'quality.plan.%' AND p.status='active'`).Scan(&newGrants); err != nil {
		t.Fatal(err)
	}
	if retiredActive != 0 || retiredBindings != 0 || newGrants != 5 {
		t.Fatalf("active old=%d old grants=%d new grants=%d", retiredActive, retiredBindings, newGrants)
	}
	if reusable {
		var ruleGrants int
		if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions b JOIN system.permissions p ON p.id=b.permission_id JOIN system.roles r ON r.id=b.role_id WHERE r.role_key='tenant.governance_manager' AND p.permission_key LIKE 'quality.rule.%' AND p.status='active'`).Scan(&ruleGrants); err != nil {
			t.Fatal(err)
		}
		if ruleGrants != 4 {
			t.Fatalf("rule CRUD grants=%d", ruleGrants)
		}
		var ruleExecute int
		if err := db.QueryRow(`SELECT count(*) FROM system.permissions WHERE permission_key='quality.rule.execute'`).Scan(&ruleExecute); err != nil || ruleExecute != 0 {
			t.Fatalf("rule became executable: %d %v", ruleExecute, err)
		}
	}
}
