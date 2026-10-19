package migration

import (
	"context"
	"database/sql"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestTenantFunctionalRolesForwardMigrationAgainstPostgres(t *testing.T) {
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
	before, through := migrationFilesBeforeAndThrough(t, "000223_tenant_functional_roles.up.sql")
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	user := createMigrationUser(t, db, "Functional role member")
	var tenant, member, department, child, group, custom, family, prior int64
	insert := func(query string, target *int64, args ...any) {
		t.Helper()
		if err := db.QueryRow(query, args...).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	execute := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	insert("INSERT INTO system.tenants(code,name) VALUES('functional_roles','Functional roles') RETURNING id", &tenant)
	insert("INSERT INTO system.tenant_memberships(tenant_id,principal_id,source_type,joined_at) VALUES($1,$2,'bootstrap',now()) RETURNING id", &member, tenant, user)
	insert("INSERT INTO system.departments(tenant_id,code,name) VALUES($1,'parent','Parent') RETURNING id", &department, tenant)
	insert("INSERT INTO system.departments(tenant_id,parent_id,code,name) VALUES($1,$2,'child','Child') RETURNING id", &child, tenant, department)
	insert("INSERT INTO system.project_groups(tenant_id,code,name) VALUES($1,'project','Project') RETURNING id", &group, tenant)
	execute("INSERT INTO system.department_memberships(tenant_id,department_id,tenant_membership_id,membership_type) VALUES($1,$2,$3,'primary')", tenant, child, member)
	execute("INSERT INTO system.project_group_memberships(tenant_id,project_group_id,tenant_membership_id) VALUES($1,$2,$3)", tenant, group, member)
	execute("INSERT INTO system.role_assignments(principal_id,role_id,scope_type,tenant_id,source_type) SELECT $1,id,'tenant',$2,'bootstrap' FROM system.roles WHERE role_key='tenant.data_viewer'", user, tenant)
	execute("INSERT INTO system.role_assignments(principal_id,role_id,scope_type,tenant_id,department_id,source_type) SELECT $1,id,'department',$2,$3,'bootstrap' FROM system.roles WHERE role_key='tenant.data_viewer'", user, tenant, child)
	execute("INSERT INTO system.role_assignments(principal_id,role_id,scope_type,tenant_id,project_group_id,source_type) SELECT $1,id,'project_group',$2,$3,'bootstrap' FROM system.roles WHERE role_key='tenant.data_viewer'", user, tenant, group)
	insert("INSERT INTO system.roles(tenant_id,role_key,name,role_type,allowed_scope_types,allowed_principal_types,immutable,created_by_principal_id) VALUES($1,'custom.organization_only','Organization only','tenant_custom',ARRAY['department'],ARRAY['user'],false,$2) RETURNING id", &custom, tenant, user)
	execute("INSERT INTO system.role_permissions(role_id,permission_id,source_type,created_by_principal_id) SELECT $1,id,'tenant',$2 FROM system.permissions WHERE permission_key='manager.data_item.read'", custom, user)
	execute("INSERT INTO system.role_assignments(principal_id,role_id,scope_type,tenant_id,department_id,source_type) VALUES($1,$2,'department',$3,$4,'bootstrap')", user, custom, tenant, child)
	insert("SELECT authorization_version FROM system.principals WHERE id=$1", &prior, user)
	insert(`INSERT INTO system.refresh_token_families(principal_id,context_type,tenant_membership_id,issued_authorization_version,client_id,auth_type,audiences,scopes,authentication_methods,assurance_level,authenticated_at,expires_at) VALUES($1,'tenant',$2,$3,'addp-web','first_party',ARRAY['addp.api'],ARRAY[]::text[],ARRAY['password'],'aal1',now(),now()+interval '1 hour') RETURNING id`, &family, user, member, prior)
	snapshot := func() string {
		t.Helper()
		var value string
		err := db.QueryRow(`SELECT jsonb_build_object('departments',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM system.departments d),'groups',(SELECT jsonb_agg(to_jsonb(g) ORDER BY id) FROM system.project_groups g),'department_members',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM system.department_memberships m),'group_members',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM system.project_group_memberships m),'tenant_members',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM system.tenant_memberships m))::text`).Scan(&value)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	organizationBefore := snapshot()
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if actual := snapshot(); actual != organizationBefore {
		t.Fatal("organization identities, hierarchy or membership changed")
	}
	var count int
	checkCount := func(query string, want int, args ...any) {
		t.Helper()
		if err := db.QueryRow(query, args...).Scan(&count); err != nil || count != want {
			t.Fatalf("count=%d want=%d err=%v SQL=%s", count, want, err, query)
		}
	}
	checkCount("SELECT count(*) FROM system.role_assignments WHERE principal_id=$1", 1, user)
	checkCount("SELECT count(*) FROM system.audit_logs WHERE event_name='iam.tenant_role_assignment.retired' AND details->'assignment'->>'principal_id'=$1", 3, strconv.FormatInt(user, 10))
	checkCount("SELECT count(*) FROM system.roles WHERE id=$1 AND status='disabled'", 1, custom)
	checkCount("SELECT count(*) FROM system.role_permissions WHERE role_id=$1", 1, custom)
	checkCount("SELECT count(*) FROM information_schema.columns WHERE table_schema='system' AND table_name='role_assignments' AND column_name IN ('department_id','project_group_id')", 0)
	checkCount("SELECT count(*) FROM system.permissions WHERE allowed_scope_types && ARRAY['department','project_group']::text[]", 0)
	checkCount("SELECT count(*) FROM system.roles WHERE allowed_scope_types && ARRAY['department','project_group']::text[]", 0)
	checkCount("SELECT count(*) FROM system.refresh_token_families WHERE id=$1 AND revoked_at IS NOT NULL", 1, family)
	var current int64
	insert("SELECT authorization_version FROM system.principals WHERE id=$1", &current, user)
	if current <= prior {
		t.Fatal("retired authorization did not advance version")
	}
	for _, scope := range []string{"department", "project_group"} {
		if _, err := db.Exec("INSERT INTO system.role_assignments(principal_id,role_id,scope_type,tenant_id,source_type) SELECT $1,id,$2,$3,'bootstrap' FROM system.roles WHERE role_key='tenant.data_viewer'", user, scope, tenant); err == nil {
			t.Fatalf("retired scope %s accepted", scope)
		}
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	var repeated int64
	insert("SELECT authorization_version FROM system.principals WHERE id=$1", &repeated, user)
	if repeated != current {
		t.Fatal("repeat migration changed authorization")
	}
	checkCount("SELECT count(*) FROM system.audit_logs WHERE event_name='iam.tenant_role_assignment.retired'", 3)
}
