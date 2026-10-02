package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestCatalogSharingDecisionPermissionForwardMigrationAgainstPostgres(t *testing.T) {
	testApprovalPermissionForwardMigration(t, "000170_catalog_sharing_decision_permission.up.sql", 170, map[string][3]string{
		"catalog.sharing_decision.create": {"catalog", "create", "high"},
	})
}

func TestEngineApprovalRequirementPermissionForwardMigrationAgainstPostgres(t *testing.T) {
	testApprovalPermissionForwardMigration(t, "000174_engine_access_approval_requirement_permissions.up.sql", 174, map[string][3]string{
		"system.engine_access_approval_requirement.initialize": {"system", "initialize", "high"},
		"system.engine_access_approval_requirement.read":       {"system", "read", "low"},
	})
}

func testApprovalPermissionForwardMigration(t *testing.T, filename string, expectedVersion int64, permissions map[string][3]string) {
	t.Helper()
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
	before, through := migrationFilesBeforeAndThrough(t, filename)
	run := func(files *Runner) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := files.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	run(&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot})
	var beforeRoles, beforeAssignments, beforeVersions int64
	if err := db.QueryRow("SELECT count(*) FROM system.role_permissions").Scan(&beforeRoles); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM system.role_assignments").Scan(&beforeAssignments); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COALESCE(sum(authorization_version),0) FROM system.principals").Scan(&beforeVersions); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		run(&Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot})
	}
	for key, expected := range permissions {
		var owner, action, risk, scopes, status string
		var delegable, customizable bool
		if err := db.QueryRow(`SELECT owner_module, action, risk_level, array_to_string(allowed_scope_types, ','), status, delegable, tenant_customizable FROM system.permissions WHERE permission_key = $1`, key).Scan(&owner, &action, &risk, &scopes, &status, &delegable, &customizable); err != nil {
			t.Fatal(err)
		}
		if owner != expected[0] || action != expected[1] || risk != expected[2] || scopes != "tenant" || status != "active" || delegable || !customizable {
			t.Fatalf("unexpected permission %s: %s/%s/%s/%s/%s/%v/%v", key, owner, action, risk, scopes, status, delegable, customizable)
		}
	}
	var roles, assignments, versions, version int64
	var dirty bool
	if err := db.QueryRow("SELECT count(*) FROM system.role_permissions").Scan(&roles); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM system.role_assignments").Scan(&assignments); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COALESCE(sum(authorization_version),0) FROM system.principals").Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT version,dirty FROM system.schema_migrations").Scan(&version, &dirty); err != nil {
		t.Fatal(err)
	}
	if roles != beforeRoles || assignments != beforeAssignments || versions != beforeVersions || version != expectedVersion || dirty {
		t.Fatalf("migration changed grants or was not clean: roles=%d/%d assignments=%d/%d versions=%d/%d migration=%d/%v", roles, beforeRoles, assignments, beforeAssignments, versions, beforeVersions, version, dirty)
	}
}
