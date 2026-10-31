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

// The approved development reset removes old source rules and preserves IAM.
func TestDirectPermissionRetirementAgainstPostgres(t *testing.T) {
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
	before, through := migrationFilesBeforeAndThrough(t, "000257_retire_direct_authorization_permissions.up.sql")
	run := func(r *Runner) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := r.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	run(&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot})
	principal, tenant := seedInitializedMigrationTenant(t, db, "direct_retirement", "Direct permission migration")
	var engine int64
	if err := db.QueryRow(`INSERT INTO system.engines(tenant_id,name,engine_type,connection_info,identity_key,lifecycle_state,is_builtin)
 VALUES($1,'Migration target','postgresql','{}','{"database":"permit-migration"}','active',false) RETURNING id`, tenant).Scan(&engine); err != nil {
		t.Fatal(err)
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

	var member int64
	if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE principal_id=$1 AND tenant_id=$2`, principal, tenant).Scan(&member); err != nil {
		t.Fatal(err)
	}
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	for i, id := range ids {
		path := plugin.TabularItemPath(uint(engine), "schema", "outdoor", id.String())
		raw, _ := json.Marshal(path)
		binding := factstest.Binding(path)
		if _, err := db.Exec(`INSERT INTO system.engine_access_direct_permissions(id,tenant_id,engine_id,catalog_path,item_fingerprint,allowed,version,updated_at) VALUES($1,$2,$3,$4::jsonb,$5,true,1,now())`, uuid.New(), tenant, engine, string(raw), binding.ItemFingerprint); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO system.engine_access_grants(request_id,approval_mode,tenant_id,engine_id,catalog_path,item_fingerprint,boundary_hash,recipient_type,recipient_id,action,expiry_mode,operator_principal_id,operator_membership_id,operator_authorization_version,reason,direct_permission_source,direct_permission_version)
 VALUES($1,'independent',$2,$3,$4::jsonb,$5,$6,'user',$7,'read','until_revoked',$7,$8,$9,'Migration fixture','explicit',1)`, id, tenant, engine, string(raw), binding.ItemFingerprint, binding.BoundaryHash, principal, member, priorVersion); err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			if _, err := db.Exec(`INSERT INTO system.engine_access_grant_revocations(request_id,revoked_by_principal_id,revoked_by_membership_id,reason,revoked_request_ids) VALUES($1,$2,$3,'Preserved withdrawal',jsonb_build_array(($1::uuid)::text))`, id, principal, member); err != nil {
				t.Fatal(err)
			}
		}
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
	var tables, columns, permissions int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM information_schema.tables WHERE table_schema='system' AND table_name='engine_access_direct_permissions'),(SELECT count(*) FROM information_schema.columns WHERE table_schema='system' AND table_name='engine_access_grants' AND column_name IN ('direct_permission_source','direct_permission_version')),(SELECT count(*) FROM system.permissions WHERE status='active' AND permission_key LIKE 'system.engine_access_direct_permission.%')`).Scan(&tables, &columns, &permissions); err != nil || tables != 0 || columns != 0 || permissions != 0 {
		t.Fatalf("retired state %d/%d/%d %v", tables, columns, permissions, err)
	}

	for _, table := range []string{"engine_access_grants", "engine_access_grant_revocations", "engine_access_denies", "engine_access_deny_releases", "engine_access_fulfillment_outcomes"} {
		var count int
		if err := db.QueryRow("SELECT count(*) FROM system." + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("old source rules remain in %s: %d %v", table, count, err)
		}
	}
	var afterVersion int64
	var revoked bool
	if err := db.QueryRow(`SELECT p.authorization_version,f.revoked_at IS NOT NULL FROM system.principals p JOIN system.refresh_token_families f ON f.principal_id=p.id WHERE f.id=$1`, family).Scan(&afterVersion, &revoked); err != nil || afterVersion <= priorVersion || !revoked {
		t.Fatalf("session was not invalidated %d/%d %t %v", priorVersion, afterVersion, revoked, err)
	}
	if snapshot() != previous {
		t.Fatal("retirement changed audit or IAM assignments")
	}
	run(r)
	var replayVersion int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id=$1`, principal).Scan(&replayVersion); err != nil || replayVersion != afterVersion {
		t.Fatal("migration replay changed authorization version", err)
	}
	if snapshot() != previous {
		t.Fatal("replay changed history")
	}
}
