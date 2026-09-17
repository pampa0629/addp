package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
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

func TestQualityStandardReferenceForwardMigrationAgainstPostgres(t *testing.T) {
	assertPublishedQualityReferenceMigration(t)
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
	if _, err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	before, after := migrationFilesBeforeAndThrough(t, "000150_iam_quality_domain_runtime.up.sql")
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	var recorded149 string
	if err := db.QueryRow(`SELECT sha256 FROM system.schema_migration_checksums WHERE version=149`).Scan(&recorded149); err != nil {
		t.Fatal(err)
	}
	_, tenant := seedInitializedMigrationTenant(t, db, "quality-domain", "Quality Domain")
	for name, role := range map[string]string{"addp-standard": "tenant.standard_runtime", "addp-quality": "tenant.quality_runtime"} {
		if _, err := db.Exec(`INSERT INTO system.tenant_memberships (tenant_id, principal_id, status, source_type, joined_at) SELECT $1, id, 'active', 'bootstrap', now() FROM system.service_principals WHERE name=$2`, tenant, name); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO system.role_assignments (principal_id, role_id, scope_type, tenant_id, status, valid_from, source_type) SELECT sp.id, r.id, 'tenant', $1, 'active', now(), 'bootstrap' FROM system.service_principals sp JOIN system.roles r ON r.role_key=$3 AND r.tenant_id IS NULL WHERE sp.name=$2`, tenant, name, role); err != nil {
			t.Fatal(err)
		}
	}
	versions := map[string]int64{}
	for _, name := range []string{"addp-standard", "addp-quality"} {
		var version int64
		if err := db.QueryRow(`SELECT p.authorization_version FROM system.principals p JOIN system.service_principals sp ON sp.id=p.id JOIN system.tenant_memberships m ON m.principal_id=p.id WHERE sp.name=$1 AND m.tenant_id=$2 AND m.status='active'`, name, tenant).Scan(&version); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		versions[name] = version
	}
	runner := &Runner{DSN: dsn, FS: after, Root: DefaultMigrationsRoot}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	afterVersions := map[string]int64{}
	for name, version := range versions {
		var got int64
		if err := db.QueryRow(`SELECT p.authorization_version FROM system.principals p JOIN system.service_principals sp ON sp.id=p.id JOIN system.tenant_memberships m ON m.principal_id=p.id WHERE sp.name=$1 AND m.tenant_id=$2`, name, tenant).Scan(&got); err != nil || got <= version {
			t.Fatalf("%s authorization version %d -> %d: %v", name, version, got, err)
		}
		afterVersions[name] = got
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}
	var preserved149 string
	if err := db.QueryRow(`SELECT sha256 FROM system.schema_migration_checksums WHERE version=149`).Scan(&preserved149); err != nil || preserved149 != recorded149 {
		t.Fatalf("published checksum changed: %s -> %s: %v", recorded149, preserved149, err)
	}
	for name, version := range afterVersions {
		var got int64
		if err := db.QueryRow(`SELECT p.authorization_version FROM system.principals p JOIN system.service_principals sp ON sp.id=p.id JOIN system.tenant_memberships m ON m.principal_id=p.id WHERE sp.name=$1 AND m.tenant_id=$2`, name, tenant).Scan(&got); err != nil || got != version {
			t.Fatalf("repeated migration changed %s version %d -> %d: %v", name, version, got, err)
		}
	}
	for role, permission := range map[string]string{"tenant.standard_runtime": "quality.standard_reference.update", "tenant.quality_runtime": "standard.domain.read"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE r.role_key=$1 AND r.tenant_id IS NULL AND p.permission_key=$2 AND p.status='active'`, role, permission).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s %s grants=%d: %v", role, permission, count, err)
		}
	}
}

func assertPublishedQualityReferenceMigration(t *testing.T) {
	t.Helper()
	raw, err := EmbeddedSQL.ReadFile("sql/000149_iam_quality_standard_reference_runtime.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	const published = "f3cab0f9f86a480e9709f4a4617e581192a86df03c5b81f59b38c0d6a751b281"
	if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != published {
		t.Fatalf("published migration 149 changed: got %s, want %s; corrections require a new forward migration", got, published)
	}
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
