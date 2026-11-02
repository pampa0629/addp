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

// Exercise the real forward transition, including immutable old rows. Runtime
// source Deny is removed; it is never converted into a Grant or a withdrawal.
func TestNodeSourceGrantsForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires standard System PostgreSQL gate")
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
	before, through := migrationFilesBeforeAndThrough(t, "000266_node_source_grants_retire_denies.up.sql")
	run := func(r *Runner) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := r.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	run(&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot})
	principal, tenant := seedInitializedMigrationTenant(t, db, "node_retirement", "Node source migration")
	var engine, member, version int64
	if err := db.QueryRow(`INSERT INTO system.engines(tenant_id,name,engine_type,connection_info,identity_key,lifecycle_state,is_builtin)
 VALUES($1,'Node migration','postgresql','{}','{"database":"node-migration"}','active',false) RETURNING id`, tenant).Scan(&engine); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT m.id,p.authorization_version FROM system.tenant_memberships m JOIN system.principals p ON p.id=m.principal_id WHERE m.principal_id=$1 AND m.tenant_id=$2`, principal, tenant).Scan(&member, &version); err != nil {
		t.Fatal(err)
	}
	path := plugin.TabularItemPath(uint(engine), "schema", "outdoor", "people")
	raw, _ := json.Marshal(path)
	binding := factstest.Binding(path)
	grant, deny := uuid.New(), uuid.New()
	if _, err := db.Exec(`INSERT INTO system.engine_access_grants(request_id,approval_mode,tenant_id,engine_id,catalog_path,item_fingerprint,boundary_hash,recipient_type,recipient_id,action,expiry_mode,operator_principal_id,operator_membership_id,operator_authorization_version,reason)
 VALUES($1,'independent',$2,$3,$4::jsonb,$5,$6,'user',$7,'read','until_revoked',$7,$8,$9,'Original precise grant')`, grant, tenant, engine, string(raw), binding.ItemFingerprint, binding.BoundaryHash, principal, member, version); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.engine_access_denies(deny_id,tenant_id,engine_id,catalog_path,item_fingerprint,recipient_type,recipient_id,action,expiry_mode,established_by_principal_id,established_by_membership_id,reason)
 VALUES($1,$2,$3,$4::jsonb,$5,'user',$6,'read','until_revoked',$6,$7,'Retired source deny')`, deny, tenant, engine, string(raw), binding.ItemFingerprint, principal, member); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.engine_access_deny_releases(deny_id,released_by_principal_id,released_by_membership_id,reason) VALUES($1,$2,$3,'Retired release')`, deny, principal, member); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var result string
		if err := db.QueryRow(`SELECT jsonb_build_object('grants',(SELECT jsonb_agg(to_jsonb(g) ORDER BY request_id) FROM system.engine_access_grants g),'withdrawals',(SELECT jsonb_agg(to_jsonb(r) ORDER BY request_id) FROM system.engine_access_grant_revocations r),'members',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM system.tenant_memberships m),'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM system.audit_logs a))::text`).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	previous := snapshot()
	r := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	run(r)
	// The new nullable discriminator does not rewrite the old precise Grant.
	var unchanged bool
	if err := db.QueryRow(`SELECT request_id=$1 AND node_recursive IS NULL AND catalog_path=$2::jsonb AND item_fingerprint=$3 AND boundary_hash=$4 FROM system.engine_access_grants`, grant, string(raw), binding.ItemFingerprint, binding.BoundaryHash).Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("precise grant changed", err)
	}
	var retired, active int
	if err := db.QueryRow(`SELECT (SELECT count(*) FROM information_schema.tables WHERE table_schema='system' AND table_name IN ('engine_access_denies','engine_access_deny_releases')),(SELECT count(*) FROM system.permissions WHERE permission_key LIKE 'system.engine_access_deny.%' AND (status<>'disabled' OR tenant_customizable))`).Scan(&retired, &active); err != nil || retired != 0 || active != 0 {
		t.Fatalf("retired state %d/%d %v", retired, active, err)
	}
	var routines int
	if err := db.QueryRow(`SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='system' AND p.proname IN ('guard_engine_access_deny_insert','preserve_engine_access_deny','guard_engine_access_deny_release_insert','preserve_engine_access_deny_release')`).Scan(&routines); err != nil || routines != 0 {
		t.Fatal("retired routines remain", err)
	}
	// Compare precise-row history without the one new null discriminator.
	var original string
	if err := db.QueryRow(`SELECT jsonb_build_object('grants',(SELECT jsonb_agg(to_jsonb(g)-'node_recursive' ORDER BY request_id) FROM system.engine_access_grants g),'withdrawals',(SELECT jsonb_agg(to_jsonb(r) ORDER BY request_id) FROM system.engine_access_grant_revocations r),'members',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM system.tenant_memberships m),'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM system.audit_logs a))::text`).Scan(&original); err != nil || original != previous {
		t.Fatal("migration changed source or IAM history", err)
	}
	root, _ := json.Marshal(plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), uint(engine)))
	schema, _ := json.Marshal(plugin.TabularNamespacePath(uint(engine), "schema", "outdoor"))
	neighbor, _ := json.Marshal(plugin.TabularItemPath(uint(engine), "schema", "outdoor2", "people"))
	var direct, recursive, child, leak bool
	if err := db.QueryRow(`SELECT system.source_node_covers($1::jsonb,false,$2::jsonb),system.source_node_covers($1::jsonb,true,$2::jsonb),system.source_node_covers($3::jsonb,false,$2::jsonb),system.source_node_covers($3::jsonb,true,$4::jsonb)`, string(root), string(raw), string(schema), string(neighbor)).Scan(&direct, &recursive, &child, &leak); err != nil || direct || !recursive || !child || leak {
		t.Fatalf("typed coverage %v/%v/%v/%v %v", direct, recursive, child, leak, err)
	}
	after := snapshot()
	run(r)
	if snapshot() != after {
		t.Fatal("replay changed immutable history")
	}
}
