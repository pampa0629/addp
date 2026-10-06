package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestManagerProfileAuthorizationForwardMigrationAgainstPostgres(t *testing.T) {
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
		if _, err := db.Exec("DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE"); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	before, after := migrationFilesBeforeAndThrough(t, "000192_manager_profile_source_read_authorization.up.sql")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	userID, tenantID := seedInitializedMigrationTenant(t, db, "manager-profile-migration", "Manager Profile Migration")
	var engineID int64
	if err := db.QueryRow(`INSERT INTO system.engines (
		tenant_id,name,engine_type,connection_info,identity_key,lifecycle_state,is_builtin,created_at,updated_at)
		VALUES ($1,'Migration-only profile engine','postgresql',
		'{"host":"not-a-business-source","database":"migration"}'::jsonb,
		'{"host":"not-a-business-source","database":"migration"}'::jsonb,'active',false,now(),now()) RETURNING id`, tenantID).Scan(&engineID); err != nil {
		t.Fatal(err)
	}
	var authID int64
	if err := db.QueryRow(`INSERT INTO system.execution_authorizations (
		actor_principal_id,tenant_id,tenant_membership_id,issued_authorization_version,
		source_type,execution_id,audience,expires_at,created_at)
		SELECT p.id,m.tenant_id,m.id,p.authorization_version,'user',
		'19200000-0000-0000-0000-000000000001','develop',now()+interval '10 minutes',now()
		FROM system.principals p JOIN system.tenant_memberships m ON m.principal_id=p.id
		WHERE p.id=$1 AND m.tenant_id=$2 RETURNING id`, userID, tenantID).Scan(&authID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.execution_authorization_engine_accesses(authorization_id,engine_id,effects)
		VALUES ($1,$2,ARRAY['read'])`, authID, engineID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE system.execution_authorizations SET sealed_at=created_at WHERE id=$1`, authID); err != nil {
		t.Fatal(err)
	}
	var original string
	if err := db.QueryRow(`SELECT to_jsonb(e)::text FROM system.execution_authorizations e WHERE id=$1`, authID).Scan(&original); err != nil {
		t.Fatal(err)
	}
	versions := func() map[int64]int64 {
		t.Helper()
		rows, err := db.Query("SELECT id,authorization_version FROM system.principals")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		result := make(map[int64]int64)
		for rows.Next() {
			var id, version int64
			if err := rows.Scan(&id, &version); err != nil {
				t.Fatal(err)
			}
			result[id] = version
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return result
	}
	var runtimeID int64
	if err := db.QueryRow("SELECT id FROM system.service_principals WHERE name='addp-manager'").Scan(&runtimeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.tenant_memberships(tenant_id,principal_id,status,source_type,joined_at)
		VALUES ($1,$2,'active','bootstrap',now())`, tenantID, runtimeID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.role_assignments(principal_id,role_id,scope_type,tenant_id,status,valid_from,source_type)
		SELECT $1,id,'tenant',$2,'active',now(),'bootstrap' FROM system.roles
		WHERE tenant_id IS NULL AND role_key='tenant.manager_runtime'`, runtimeID, tenantID); err != nil {
		t.Fatal(err)
	}
	priorVersions := versions()
	counts := func() [4]int {
		t.Helper()
		var result [4]int
		for i, table := range []string{"permissions", "role_permissions", "role_assignments", "engine_access_grants"} {
			if err := db.QueryRow("SELECT count(*) FROM system." + table).Scan(&result[i]); err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	priorCounts := counts()
	runner := &Runner{DSN: dsn, FS: after, Root: DefaultMigrationsRoot}
	for range 2 {
		if err := runner.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var current string
	var missingScope bool
	if err := db.QueryRow(`SELECT (to_jsonb(e)-'source_read_scope')::text,source_read_scope IS NULL
		FROM system.execution_authorizations e WHERE id=$1`, authID).Scan(&current, &missingScope); err != nil || !missingScope || current != original {
		t.Fatalf("historical sealed authorization changed: scope_null=%v before=%s after=%s err=%v", missingScope, original, current, err)
	}
	currentCounts := counts()
	if currentCounts != [4]int{priorCounts[0], priorCounts[1] + 1, priorCounts[2], priorCounts[3]} {
		t.Fatalf("unexpected grants or permissions: before=%v after=%v", priorCounts, currentCounts)
	}
	for id, version := range versions() {
		want := priorVersions[id]
		if id == runtimeID {
			want++
		}
		if version != want {
			t.Fatalf("principal %d authorization_version=%d want=%d", id, version, want)
		}
	}
	var version int
	var dirty bool
	if err := db.QueryRow("SELECT version,dirty FROM system.schema_migrations").Scan(&version, &dirty); err != nil || version != 192 || dirty {
		t.Fatalf("migration state=(%d,%v) err=%v", version, dirty, err)
	}
}
