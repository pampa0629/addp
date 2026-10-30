package migration

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestAgentSessionOrganizationForwardMigrationAgainstPostgres(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	before, through := migrationFilesBeforeAndThrough(t, "000254_agent_session_organization.up.sql")
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	query := func(statement string, args ...any) int64 {
		t.Helper()
		var value int64
		if err := db.QueryRow(statement, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	execute := func(statement string, args ...any) {
		t.Helper()
		if _, err := db.Exec(statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	tenant := query("INSERT INTO system.tenants(code,name) VALUES('agent_sessions','Agent sessions') RETURNING id")
	ai := createMigrationUser(t, db, "Agent organization member")
	viewer := createMigrationUser(t, db, "Unrelated viewer")
	members := map[int64]int64{}
	for _, user := range []int64{ai, viewer} {
		members[user] = query("INSERT INTO system.tenant_memberships(tenant_id,principal_id,source_type,joined_at) VALUES($1,$2,'bootstrap',now()) RETURNING id", tenant, user)
	}
	for user, role := range map[int64]string{ai: "tenant.ai_user", viewer: "tenant.data_viewer"} {
		execute("INSERT INTO system.role_assignments(principal_id,role_id,scope_type,tenant_id,source_type) SELECT $1,id,'tenant',$2,'bootstrap' FROM system.roles WHERE role_key=$3 AND tenant_id IS NULL", user, tenant, role)
	}
	families := map[int64]int64{}
	versions := map[int64]int64{}
	for _, user := range []int64{ai, viewer} {
		versions[user] = query("SELECT authorization_version FROM system.principals WHERE id=$1", user)
		families[user] = query(`INSERT INTO system.refresh_token_families(principal_id,context_type,tenant_membership_id,issued_authorization_version,client_id,auth_type,audiences,scopes,authentication_methods,assurance_level,authenticated_at,expires_at) VALUES($1,'tenant',$2,$3,'addp-web','first_party',ARRAY['addp.api'],ARRAY[]::text[],ARRAY['password'],'aal1',now(),now()+interval '1 hour') RETURNING id`, user, members[user], versions[user])
	}
	grantsBefore := query("SELECT count(*) FROM system.role_permissions")
	assignmentsBefore := query("SELECT count(*) FROM system.role_assignments")
	for range 2 {
		if err := (&Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.Query("SELECT r.role_key FROM system.role_permissions rp JOIN system.roles r ON r.id=rp.role_id JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='agent.session.update' ORDER BY r.role_key")
	if err != nil {
		t.Fatal(err)
	}
	var roles []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		roles = append(roles, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if !reflect.DeepEqual(roles, []string{"tenant.ai_user"}) {
		t.Fatalf("unexpected roles: %v", roles)
	}
	if query("SELECT count(*) FROM system.permissions WHERE permission_key='agent.session.update' AND delegable=false AND tenant_customizable AND allowed_scope_types=ARRAY['tenant']::text[]") != 1 {
		t.Fatal("permission boundary changed")
	}
	if query("SELECT count(*) FROM system.role_permissions") != grantsBefore+1 || query("SELECT count(*) FROM system.role_assignments") != assignmentsBefore {
		t.Fatal("unexpected permission or assignment changes")
	}
	if query("SELECT authorization_version FROM system.principals WHERE id=$1", ai) != versions[ai]+1 || query("SELECT authorization_version FROM system.principals WHERE id=$1", viewer) != versions[viewer] {
		t.Fatal("authorization version invalidation escaped AI role")
	}
	if query("SELECT count(*) FROM system.refresh_token_families WHERE id=$1 AND revoked_at IS NOT NULL", families[ai]) != 1 || query("SELECT count(*) FROM system.refresh_token_families WHERE id=$1 AND revoked_at IS NULL", families[viewer]) != 1 {
		t.Fatal("session invalidation escaped AI role")
	}
}
