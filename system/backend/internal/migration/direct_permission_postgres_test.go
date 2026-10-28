package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
)

// Replay the actual forward migration over both historical modes. The
// regular module gate discovers this test; no ad-hoc database is created.
func TestDirectPermissionForwardMigrationAgainstPostgres(t *testing.T) {
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
	before, through := migrationFilesBeforeAndThrough(t, "000248_direct_authorization_permissions.up.sql")
	run := func(r *Runner) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := r.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	run(&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot})
	principal, tenant := seedInitializedMigrationTenant(t, db, "direct_permit_migration", "Direct permission migration")
	var engine int64
	if err := db.QueryRow(`INSERT INTO system.engines(tenant_id,name,engine_type,connection_info,identity_key,lifecycle_state,is_builtin)
 VALUES($1,'Migration target','postgresql','{}','{"database":"permit-migration"}','active',false) RETURNING id`, tenant).Scan(&engine); err != nil {
		t.Fatal(err)
	}
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	for i, mode := range []string{"independent", "catalog"} {
		path := plugin.TabularItemPath(uint(engine), "schema", "outdoor", mode)
		raw, _ := json.Marshal(path)
		fp := factstest.Binding(path).ItemFingerprint
		if _, err := db.Exec(`INSERT INTO system.engine_access_approval_requirements(id,tenant_id,engine_id,catalog_path,item_fingerprint,mode,version,updated_at)
 VALUES($1,$2,$3,$4::jsonb,$5,$6,1,clock_timestamp())`, ids[i], tenant, engine, string(raw), fp, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE system.engine_access_approval_requirements SET version=2 WHERE id=$1`, ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO system.role_assignments(principal_id,role_id,scope_type,tenant_id,status,valid_from,source_type)
 SELECT $1,id,'tenant',$2,'active',now(),'bootstrap' FROM system.roles WHERE tenant_id IS NULL AND role_key='tenant.engine_access_delegation_administrator'`, principal, tenant); err != nil {
		t.Fatal(err)
	}
	var priorVersion int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id=$1`, principal).Scan(&priorVersion); err != nil {
		t.Fatal(err)
	}
	var family int64
	if err := db.QueryRow(`INSERT INTO system.refresh_token_families(principal_id,context_type,tenant_membership_id,issued_authorization_version,client_id,auth_type,audiences,scopes,authentication_methods,assurance_level,authenticated_at,expires_at)
 SELECT $1,'tenant',m.id,$3,'addp-web','first_party',ARRAY['addp.api'],ARRAY[]::text[],ARRAY['password'],'aal1',now(),now()+interval '1 hour' FROM system.tenant_memberships m WHERE m.principal_id=$1 AND m.tenant_id=$2 RETURNING id`, principal, tenant, priorVersion).Scan(&family); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var result string
		if err := db.QueryRow(`SELECT jsonb_build_object('assignments',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM system.role_assignments a),'members',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM system.tenant_memberships m),'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM system.audit_logs a))::text`).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	previous := snapshot()
	r := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	run(r)
	for i, id := range ids {
		var allowed bool
		var version int64
		if err := db.QueryRow(`SELECT allowed,version FROM system.engine_access_direct_permissions WHERE id=$1`, id).Scan(&allowed, &version); err != nil || allowed != (i == 0) || version != 2 {
			t.Fatalf("historical mapping %d: %t/%d %v", i, allowed, version, err)
		}
	}
	var legacy, grants, oldActive, newManagers int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM information_schema.tables WHERE table_schema='system' AND table_name='engine_access_approval_requirements'),(SELECT count(*) FROM system.engine_access_grants),(SELECT count(*) FROM system.permissions WHERE permission_key LIKE 'system.engine_access_approval_requirement.%' AND status='active'),(SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id JOIN system.roles r ON r.id=rp.role_id WHERE p.permission_key='system.engine_access_direct_permission.update' AND r.role_key='tenant.engine_access_delegation_administrator' AND NOT p.tenant_customizable)`).Scan(&legacy, &grants, &oldActive, &newManagers); err != nil || legacy != 0 || grants != 0 || oldActive != 0 || newManagers != 1 {
		t.Fatalf("legacy=%d grants=%d oldActive=%d managers=%d err=%v", legacy, grants, oldActive, newManagers, err)
	}
	var afterVersion int64
	var revoked bool
	if err := db.QueryRow(`SELECT p.authorization_version,f.revoked_at IS NOT NULL FROM system.principals p JOIN system.refresh_token_families f ON f.principal_id=p.id WHERE f.id=$1`, family).Scan(&afterVersion, &revoked); err != nil || afterVersion <= priorVersion || !revoked {
		t.Fatalf("old session remains usable: %d/%d %t %v", priorVersion, afterVersion, revoked, err)
	}
	if previous != snapshot() {
		t.Fatal("migration changed IAM assignments, memberships or generic audit")
	}
	run(r)
	var replayVersion int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id=$1`, principal).Scan(&replayVersion); err != nil || replayVersion != afterVersion {
		t.Fatal("ordinary restart replayed permission publication", err)
	}
	for _, query := range []string{`DELETE FROM system.engine_access_direct_permissions WHERE id=$1`, `UPDATE system.engine_access_direct_permissions SET allowed=false WHERE id=$1`, `UPDATE system.engine_access_direct_permissions SET item_fingerprint=repeat('f',64),version=version+1 WHERE id=$1`} {
		if _, err := db.Exec(query, ids[0]); err == nil {
			t.Fatal("immutable/version guard accepted", query)
		}
	}
}
