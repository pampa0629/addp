package migration

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestSourceDataItemBoundaryForwardMigrationAgainstPostgres(t *testing.T) {
	testCoordinationForwardMigration(t, "000207_source_dataitem_boundary_bindings.up.sql", 207, "engine_access_grants", func(db *sql.DB) func() {
		principal, tenant := seedInitializedMigrationTenant(t, db, "boundary_upgrade", "Boundary upgrade")
		var engine, member, caller int64
		if err := db.QueryRow(`INSERT INTO system.engines(tenant_id,name,engine_type,connection_info,identity_key,lifecycle_state,is_builtin)
 VALUES($1,'Boundary fixture','postgresql','{}','{"database":"boundary-upgrade"}','active',false) RETURNING id`, tenant).Scan(&engine); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE tenant_id=$1 AND principal_id=$2`, tenant, principal).Scan(&member); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT id FROM system.service_principals WHERE name='addp-catalog'`).Scan(&caller); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO system.engine_access_delegations(tenant_id,engine_id,tenant_membership_id,expires_at,granted_by_principal_id,grant_reason)
 VALUES($1,$2,$3,clock_timestamp()+interval '1 hour',$4,'Preserve this management delegation')`, tenant, engine, member, principal); err != nil {
			t.Fatal(err)
		}
		path := plugin.TabularItemPath(uint(engine), "schema", "outdoor", "boundary_upgrade")
		encoded, err := authorization.EncodeSharingTarget(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO system.engine_access_approval_requirements(id,tenant_id,engine_id,catalog_path,mode,version,updated_at)
 VALUES($4,$1,$2,$3::jsonb,'catalog',1,clock_timestamp())`, tenant, engine, string(encoded), uuid.New()); err != nil {
			t.Fatal(err)
		}
		id := uuid.New()
		if _, err := db.Exec(`INSERT INTO system.engine_access_fulfillment_outcomes
 (request_id,tenant_id,engine_id,caller_principal_id,catalog_path,binding,expiry_mode,outcome,recorded_at,deadline)
 SELECT $1,$2,$3,$4,$5::jsonb,jsonb_build_object('operator',jsonb_build_object('principal_id',p.id,
 'tenant_membership_id',$6::bigint,'authorization_version',p.authorization_version),'decision_id',($1::uuid)::text,
 'requirement_version',1,'recipient_type','user','recipient_id',p.id,'action','read','expiry_mode','until_revoked','expires_at',NULL),
 'until_revoked','accepted',statement_timestamp(),statement_timestamp()+interval '5 minutes' FROM system.principals p WHERE p.id=$7`, id, tenant, engine, caller, string(encoded), member, principal); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO system.engine_access_grants(request_id,approval_mode) VALUES($1,'catalog')`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO system.engine_access_grant_revocations(request_id,revoked_by_principal_id,revoked_by_membership_id,reason,revoked_request_ids) VALUES($1,$2,$3,'Disposable old native grant',jsonb_build_array($1::uuid))`, id, principal, member); err != nil {
			t.Fatal(err)
		}
		snapshot := func() string {
			t.Helper()
			var value string
			if err := db.QueryRow(`SELECT jsonb_build_object('memberships',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM system.tenant_memberships m),
 'roles',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM system.roles r),
 'delegations',(SELECT jsonb_agg(to_jsonb(d) ORDER BY id) FROM system.engine_access_delegations d),
 'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM system.audit_logs a))::text`).Scan(&value); err != nil {
				t.Fatal(err)
			}
			return value
		}
		before := snapshot()
		return func() {
			if snapshot() != before {
				t.Fatal("boundary migration changed IAM, delegations or generic audits")
			}
			for _, table := range []string{"engine_access_grants", "engine_access_grant_revocations", "engine_access_fulfillment_outcomes", "engine_access_approval_requirements", "engine_access_denies", "engine_access_deny_releases"} {
				var count int64
				if err := db.QueryRow("SELECT count(*) FROM system." + table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("old %s survived: %d %v", table, count, err)
				}
			}
			binding := factstest.Binding(path)
			// A native path by itself can never initialize new authority.
			if _, err := db.Exec(`INSERT INTO system.engine_access_approval_requirements(id,tenant_id,engine_id,catalog_path,mode,version,updated_at) VALUES($4,$1,$2,$3::jsonb,'catalog',1,clock_timestamp())`, tenant, engine, string(encoded), uuid.New()); err == nil {
				t.Fatal("native-only requirement accepted")
			}
			var valid bool
			if err := db.QueryRow(`SELECT system.valid_source_single_identity($1::jsonb,$2,$3)`, string(encoded), engine, binding.ItemFingerprint).Scan(&valid); err != nil || valid {
				t.Fatalf("historical 207 schema-kind behavior changed; correction belongs in 208: %v", err)
			}
			collision := plugin.TabularItemPath(uint(engine), "schema", "outdoor.boundary", "upgrade")
			other, _ := json.Marshal(collision)
			if err := db.QueryRow(`SELECT system.valid_source_single_identity($1::jsonb,$2,$3)`, string(other), engine, binding.ItemFingerprint).Scan(&valid); err != nil || valid {
				t.Fatal("mismatched identity accepted")
			}
		}
	})
}
