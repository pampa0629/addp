package migration

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestSourceSingleRecordTargetsForwardMigrationAgainstPostgres(t *testing.T) {
	testCoordinationForwardMigration(t, "000210_source_single_record_targets.up.sql", 210, "engine_access_grants", func(db *sql.DB) func() {
		principal, tenant := seedInitializedMigrationTenant(t, db, "single_targets", "Single targets")
		var engine, member, caller int64
		if err := db.QueryRow(`INSERT INTO system.engines(tenant_id,name,engine_type,connection_info,identity_key,lifecycle_state,is_builtin)
 VALUES($1,'Single targets fixture','postgresql','{}','{"database":"single-targets"}','active',false) RETURNING id`, tenant).Scan(&engine); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE tenant_id=$1 AND principal_id=$2`, tenant, principal).Scan(&member); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT id FROM system.service_principals WHERE name='addp-catalog'`).Scan(&caller); err != nil {
			t.Fatal(err)
		}
		paths := []plugin.EngineCatalogPath{
			plugin.TabularItemPath(uint(engine), "schema", "outdoor", "before_upgrade"),
			plugin.TabularItemPath(uint(engine), "database", "户外.db", " persons "),
			plugin.EngineCatalogBranchLeafPath(plugin.DynamicSchemaCatalogModel(), uint(engine), "database", "户外.db", "collection", "collection", " people "),
		}
		encodedPath := func(path plugin.EngineCatalogPath) string {
			t.Helper()
			data, err := json.Marshal(path)
			if err != nil {
				t.Fatal(err)
			}
			return string(data)
		}
		validIdentity := func(path plugin.EngineCatalogPath, engineID int64, fingerprint string, want bool) {
			t.Helper()
			var valid bool
			if err := db.QueryRow(`SELECT system.valid_source_single_identity($1::jsonb,$2,$3)`, encodedPath(path), engineID, fingerprint).Scan(&valid); err != nil || valid != want {
				t.Fatalf("identity valid=%t want=%t err=%v", valid, want, err)
			}
		}
		insertApproval := func(path plugin.EngineCatalogPath) {
			t.Helper()
			binding := factstest.Binding(path)
			if _, err := db.Exec(`INSERT INTO system.engine_access_approval_requirements(id,tenant_id,engine_id,catalog_path,item_fingerprint,mode,version,updated_at)
 VALUES($1,$2,$3,$4::jsonb,$5,'catalog',1,clock_timestamp())`, uuid.New(), tenant, engine, encodedPath(path), binding.ItemFingerprint); err != nil {
				t.Fatal("canonical target rejected by approval constraint", err)
			}
		}
		for i, path := range paths {
			validIdentity(path, engine, factstest.Binding(path).ItemFingerprint, i == 0)
		}
		insertAuthority := func(path plugin.EngineCatalogPath) {
			t.Helper()
			insertApproval(path)
			binding := factstest.Binding(path)
			id := uuid.New()
			if _, err := db.Exec(`INSERT INTO system.engine_access_fulfillment_outcomes
 (request_id,tenant_id,engine_id,caller_principal_id,catalog_path,binding,expiry_mode,outcome,recorded_at,deadline)
 SELECT $1,$2,$3,$4,$5::jsonb,jsonb_build_object('operator',jsonb_build_object('principal_id',p.id,
 'tenant_membership_id',$6::bigint,'authorization_version',p.authorization_version),'decision_id',($1::uuid)::text,
 'requirement_version',1,'recipient_type','user','recipient_id',p.id,'action','read','expiry_mode','until_revoked','expires_at',NULL,
 'item_fingerprint',$8::text,'boundary_hash',$9::text),
 'until_revoked','accepted',statement_timestamp(),statement_timestamp()+interval '5 minutes'
 FROM system.principals p WHERE p.id=$7`, id, tenant, engine, caller, encodedPath(path), member, principal, binding.ItemFingerprint, binding.BoundaryHash); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO system.engine_access_grants(request_id,approval_mode) VALUES($1,'catalog')`, id); err != nil {
				t.Fatal(err)
			}

			if _, err := db.Exec(`INSERT INTO system.engine_access_denies
 (deny_id,tenant_id,engine_id,catalog_path,item_fingerprint,recipient_type,recipient_id,action,expiry_mode,
 established_by_principal_id,established_by_membership_id,reason)
 VALUES($1,$2,$3,$4::jsonb,$5,'user',$6,'read','until_revoked',$6,$7,'Preserve pre-upgrade Deny')`, uuid.New(), tenant, engine, encodedPath(path), binding.ItemFingerprint, principal, member); err != nil {
				t.Fatal(err)
			}

		}
		insertAuthority(paths[0])

		snapshot := func() string {
			t.Helper()
			var value string
			if err := db.QueryRow(`SELECT jsonb_build_object(
 'checksums',(SELECT jsonb_agg(to_jsonb(c) ORDER BY version) FROM system.schema_migration_checksums c WHERE version < 210),
 'grants',(SELECT jsonb_agg(to_jsonb(g) ORDER BY request_id) FROM system.engine_access_grants g),
 'receipts',(SELECT jsonb_agg(to_jsonb(o) ORDER BY request_id) FROM system.engine_access_fulfillment_outcomes o),
 'approvals',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM system.engine_access_approval_requirements r),
 'denies',(SELECT jsonb_agg(to_jsonb(d) ORDER BY deny_id) FROM system.engine_access_denies d),
 'memberships',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM system.tenant_memberships m),
 'roles',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM system.roles r),
 'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM system.audit_logs a))::text`).Scan(&value); err != nil {
				t.Fatal(err)
			}
			return value
		}
		before := snapshot()
		return func() {
			if snapshot() != before {
				t.Fatal("single-target extension rewrote historical checksums, authorization, IAM or audits")
			}
			for i, path := range paths {
				fingerprint := factstest.Binding(path).ItemFingerprint
				validIdentity(path, engine, fingerprint, true)
				validIdentity(path, engine+1, fingerprint, false)
				validIdentity(path, engine, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false)
				for _, change := range []func(*plugin.EngineCatalogPath){
					func(p *plugin.EngineCatalogPath) { p.Segments[0].Name = "unexpected" },
					func(p *plugin.EngineCatalogPath) { p.Segments[0].Kind = "service" },
					func(p *plugin.EngineCatalogPath) { p.Segments[1].Term = "bucket" },
					func(p *plugin.EngineCatalogPath) { p.Segments[1].Kind = p.Segments[1].Term },
					func(p *plugin.EngineCatalogPath) { p.Segments[1].Name = "" },
					func(p *plugin.EngineCatalogPath) { p.Segments[2].Kind = "view" },
					func(p *plugin.EngineCatalogPath) { p.Segments[2].Name = "another_item" },
				} {
					other := path
					other.Segments = append([]plugin.EngineCatalogSegment(nil), path.Segments...)
					change(&other)
					validIdentity(other, engine, fingerprint, false)
				}
				if i > 0 {
					insertAuthority(path)
				}
			}
			invalid := paths[2]
			invalid.Segments = append([]plugin.EngineCatalogSegment(nil), invalid.Segments...)
			invalid.Segments[1].Term = "schema"
			validIdentity(invalid, engine, factstest.Binding(paths[2]).ItemFingerprint, false)
		}
	})
}
