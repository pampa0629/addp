package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestResourceObservationForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System IAM PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, e := sql.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	before, through := migrationFilesBeforeAndThrough(t, "000193_resource_observation_read.up.sql")
	if e := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); e != nil {
		t.Fatal(e)
	}
	// Migration-only fixtures exercise real role assignment/version triggers;
	// they issue no token and never bypass an application authorization request.
	holders := make(map[string]int64)
	versions := make(map[string]int64)
	for _, role := range []string{"platform.system_administrator", "platform.security_administrator"} {
		var principal int64
		if e := db.QueryRow(`WITH p AS (INSERT INTO system.principals(principal_type,status) VALUES ('user','active') RETURNING id)
 INSERT INTO system.role_assignments(principal_id,role_id,scope_type,source_type)
 SELECT p.id,r.id,'platform','bootstrap' FROM p JOIN system.roles r ON r.tenant_id IS NULL AND r.role_key=$1 RETURNING principal_id`, role).Scan(&principal); e != nil {
			t.Fatal(e)
		}
		holders[role] = principal
		var version int64
		if e := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id=$1`, principal).Scan(&version); e != nil {
			t.Fatal(e)
		}
		versions[role] = version
	}
	if e := (&Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}).Run(ctx); e != nil {
		t.Fatal(e)
	}
	for role, principal := range holders {
		var version int64
		if e := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id=$1`, principal).Scan(&version); e != nil {
			t.Fatal(e)
		}
		want := versions[role]
		if role == "platform.system_administrator" {
			want++
		}
		if version != want {
			t.Fatalf("role %s authorization_version=%d want=%d", role, version, want)
		}
	}
	var scopes string
	var customizable, delegable bool
	if e := db.QueryRow(`SELECT array_to_string(allowed_scope_types,','),tenant_customizable,delegable FROM system.permissions WHERE permission_key='monitor.resource_observation.read' AND status='active'`).Scan(&scopes, &customizable, &delegable); e != nil || scopes != "platform" || customizable || delegable {
		t.Fatal(scopes, customizable, delegable, e)
	}
	rows, e := db.Query(`SELECT r.role_key FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='monitor.resource_observation.read'`)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var role string
		if e := rows.Scan(&role); e != nil {
			t.Fatal(e)
		}
		if role != "platform.system_administrator" {
			t.Fatal("resource permission expanded", role)
		}
		n++
	}
	if rows.Err() != nil || n != 1 {
		t.Fatal("role count", n, rows.Err())
	}
}
