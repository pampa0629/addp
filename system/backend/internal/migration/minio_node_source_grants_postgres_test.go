package migration

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
)

// The additive profile migration must not rewrite earlier grants, recipients,
// revocations or audit facts. It grants no data to any account by itself.
func TestMinIONodeSourceGrantsForwardMigrationAgainstPostgres(t *testing.T) {
	exerciseMinIONodeMigration(t, "000270_minio_node_source_grants.up.sql", false)
}
func TestRecursiveEngineRootForwardMigrationAgainstPostgres(t *testing.T) {
	exerciseMinIONodeMigration(t, "000295_recursive_engine_root.up.sql", true)
}
func exerciseMinIONodeMigration(t *testing.T, migration string, rootAllowed bool) {
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
	before, through := migrationFilesBeforeAndThrough(t, migration)
	run := func(r *Runner) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
		defer cancel()
		if err := r.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	run(&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot})
	principal, tenant := seedInitializedMigrationTenant(t, db, "minio_nodes", "MinIO node migration")
	var engine, member, version int64
	if err := db.QueryRow(`INSERT INTO system.engines(tenant_id,name,engine_type,connection_info,identity_key,lifecycle_state,is_builtin)
 VALUES($1,'MinIO node migration','minio','{}','{"endpoint":"minio-node-fixture"}','active',false) RETURNING id`, tenant).Scan(&engine); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT m.id,p.authorization_version FROM system.tenant_memberships m JOIN system.principals p ON p.id=m.principal_id WHERE m.principal_id=$1 AND m.tenant_id=$2`, principal, tenant).Scan(&member, &version); err != nil {
		t.Fatal(err)
	}
	// Preserve an already-issued precise metadata Grant and its withdrawal.
	originalPath := plugin.ObjectItemPath(uint(engine), "bucket", "old.csv")
	originalRaw, _ := json.Marshal(originalPath)
	metadataRaw, _ := json.Marshal(map[string]any{"target": originalPath, "layout": "single", "data_type": "table", "format": "csv", "composition_digest": strings.Repeat("a", 64)})
	originalID := uuid.New()
	if _, err := db.Exec(`INSERT INTO system.engine_access_grants(request_id,approval_mode,tenant_id,engine_id,catalog_path,item_fingerprint,boundary_hash,metadata_target,recipient_type,recipient_id,action,expiry_mode,operator_principal_id,operator_membership_id,operator_authorization_version,reason)
 VALUES($1,'independent',$2,$3,$4::jsonb,$5,$6,$7::jsonb,'user',$8,'metadata_read','until_revoked',$8,$9,$10,'Preexisting precise metadata Grant')`, originalID, tenant, engine, string(originalRaw), commonmodels.GenerateItemFingerprint(uint(engine), "bucket/old.csv"), strings.Repeat("b", 64), string(metadataRaw), principal, member, version); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.engine_access_grant_revocations(request_id,revoked_by_principal_id,revoked_by_membership_id,reason,revoked_request_ids) VALUES($1,$2,$3,'Preexisting withdrawal',jsonb_build_array(($1::uuid)::text))`, originalID, principal, member); err != nil {
		t.Fatal(err)
	}
	snapshot := func() string {
		t.Helper()
		var raw string
		if err := db.QueryRow(`SELECT jsonb_build_object('grants',(SELECT jsonb_agg(to_jsonb(g) ORDER BY request_id) FROM system.engine_access_grants g),'revocations',(SELECT jsonb_agg(to_jsonb(r) ORDER BY request_id) FROM system.engine_access_grant_revocations r),'members',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM system.tenant_memberships m),'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM system.audit_logs a))::text`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	previous := snapshot()
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	run(runner)
	run(runner)
	if snapshot() != previous {
		t.Fatal("migration changed existing authorization facts")
	}
	bucket := plugin.ObjectDirectoryPath(uint(engine), "bucket", "")
	prefix := plugin.ObjectDirectoryPath(uint(engine), "bucket", "a/ exact.prefix ")
	paths := []plugin.EngineCatalogPath{bucket, prefix}
	for _, path := range paths {
		encoded, _ := json.Marshal(path)
		for _, action := range []string{"metadata_read", "read"} {
			if _, err := db.Exec(`INSERT INTO system.engine_access_grants(request_id,approval_mode,tenant_id,engine_id,catalog_path,item_fingerprint,boundary_hash,node_recursive,recipient_type,recipient_id,action,expiry_mode,operator_principal_id,operator_membership_id,operator_authorization_version,reason)
 VALUES($1,'independent',$2,$3,$4::jsonb,'','',false,'user',$5,$6,'until_revoked',$5,$7,$8,'Migration scope fixture')`, uuid.New(), tenant, engine, string(encoded), principal, action, member, version); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, root := range []plugin.EngineCatalogPath{plugin.ObjectRootPath(uint(engine)), plugin.FileDirectoryPath(uint(engine), "bucket/a")} {
		raw, _ := json.Marshal(root)
		var valid bool
		if err := db.QueryRow(`SELECT system.valid_source_node_path($1::jsonb,$2)`, string(raw), engine).Scan(&valid); err != nil {
			t.Fatal(err)
		}
		if root.Segments[0].Term == "service" && valid != rootAllowed {
			t.Fatal("service root admitted")
		}
		if _, err := db.Exec(`INSERT INTO system.engine_access_grants(request_id,approval_mode,tenant_id,engine_id,catalog_path,item_fingerprint,boundary_hash,node_recursive,recipient_type,recipient_id,action,expiry_mode,operator_principal_id,operator_membership_id,operator_authorization_version,reason)
 VALUES($1,'independent',$2,$3,$4::jsonb,'','',true,'user',$5,'read','until_revoked',$5,$6,$7,'Must reject root or wrong engine profile')`, uuid.New(), tenant, engine, string(raw), principal, member, version); (err == nil) != (rootAllowed && root.Segments[0].Term == "service") {
			t.Fatal("root profile mismatch", err)
		}
	}
}
