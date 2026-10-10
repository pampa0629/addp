package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestPlatformOntologyInspectionForwardMigrationAgainstPostgres(t *testing.T) {
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
		if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	before, through := migrationFilesBeforeAndThrough(t, "000203_platform_ontology_inspection.up.sql")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	var principal, prior int64
	if err := db.QueryRow(`INSERT INTO system.principals(principal_type) VALUES('user') RETURNING id`).Scan(&principal); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.users(id,display_name) VALUES($1,'Ontology Inspection Administrator')`, principal); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.role_assignments(principal_id,role_id,scope_type,status,valid_from,source_type)
SELECT $1,id,'platform','active',statement_timestamp(),'bootstrap' FROM system.roles WHERE role_key='platform.system_administrator' AND tenant_id IS NULL`, principal); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id=$1`, principal).Scan(&prior); err != nil {
		t.Fatal(err)
	}
	var assignments int
	if err := db.QueryRow(`SELECT count(*) FROM system.role_assignments`).Scan(&assignments); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	var role, scopes, risk string
	var delegable, customizable bool
	if err := db.QueryRow(`SELECT r.role_key,array_to_string(p.allowed_scope_types,','),p.risk_level,p.delegable,p.tenant_customizable
FROM system.permissions p JOIN system.role_permissions rp ON rp.permission_id=p.id JOIN system.roles r ON r.id=rp.role_id
WHERE p.permission_key='ontology.platform_definition.read'`).Scan(&role, &scopes, &risk, &delegable, &customizable); err != nil {
		t.Fatal(err)
	}
	if role != "platform.system_administrator" || scopes != "platform" || risk != "low" || delegable || customizable {
		t.Fatal("wrong inspection permission", role, scopes, risk)
	}
	var bindings, afterAssignments int
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='ontology.platform_definition.read'`).Scan(&bindings); err != nil || bindings != 1 {
		t.Fatal("expanded wrong roles", bindings, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM system.role_assignments`).Scan(&afterAssignments); err != nil || afterAssignments != assignments {
		t.Fatal("added assignments", err)
	}
	var version int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id=$1`, principal).Scan(&version); err != nil || version <= prior {
		t.Fatal("old token version remains", version, err)
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	var repeated int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id=$1`, principal).Scan(&repeated); err != nil || repeated != version {
		t.Fatal("migration rerun changed version", repeated, err)
	}
}
