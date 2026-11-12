package migration

import (
	"database/sql"
	"testing"

	"github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts/factstest"
	"github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestProductionResultIdentityForwardMigrationAgainstPostgres(t *testing.T) {
	testCoordinationForwardMigration(t, "000286_production_result_identity.up.sql", 286, "engine_access_initial_grant_outcomes", func(db *sql.DB) func() {
		human, tenant := seedInitializedMigrationTenant(t, db, "production_result_upgrade", "Production result upgrade")
		var issuer, issuerMember, humanMember, engine int64
		if err := db.QueryRow(`SELECT id FROM system.service_principals WHERE name='addp-transfer'`).Scan(&issuer); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE system.oauth_clients SET status='active',client_secret_hash='disposable_fixture_only' WHERE client_id='addp-transfer'`); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`INSERT INTO system.tenant_memberships(tenant_id,principal_id,status,source_type,joined_at) VALUES($1,$2,'active','bootstrap',now()) RETURNING id`, tenant, issuer).Scan(&issuerMember); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE tenant_id=$1 AND principal_id=$2`, tenant, human).Scan(&humanMember); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`INSERT INTO system.engines(tenant_id,name,engine_type,connection_info,identity_key,lifecycle_state,is_builtin) VALUES($1,'Production migration','postgresql','{}','{"database":"production-upgrade"}','active',false) RETURNING id`, tenant).Scan(&engine); err != nil {
			t.Fatal(err)
		}
		binding := factstest.Binding(plugin.TabularItemPath(uint(engine), "schema", "public", "result"))
		path, err := authorization.EncodeSharingTarget(binding.NativeTarget)
		if err != nil {
			t.Fatal(err)
		}
		execution, denied, request := uuid.New(), uuid.New(), uuid.New()
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if _, err = tx.Exec(`INSERT INTO system.engine_access_initial_grant_outcomes(tenant_id,producer_owner,execution_id,outcome,grant_request_id,issuer_principal_id,issuer_membership_id) VALUES($1,'transfer',$2,'issued',$3,$4,$5)`, tenant, execution, request, issuer, issuerMember); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO system.engine_access_grants(request_id,approval_mode,tenant_id,engine_id,catalog_path,item_fingerprint,boundary_hash,recipient_type,recipient_id,action,expiry_mode,operator_principal_id,operator_membership_id,operator_authorization_version,producer_owner,production_execution_id,issuer_principal_id,issuer_membership_id)
 SELECT $1,'task_production',$2,$3,$4::jsonb,$5,$6,'user',$7,'read','until_revoked',$7,$8,p.authorization_version,'transfer',$9,$10,$11 FROM system.principals p WHERE p.id=$7`, request, tenant, engine, string(path), binding.ItemFingerprint, binding.BoundaryHash, human, humanMember, execution, issuer, issuerMember); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO system.engine_access_grant_revocations(request_id,revoked_by_principal_id,revoked_by_membership_id,reason,revoked_request_ids) VALUES($1,$2,$3,'Preserved production withdrawal',jsonb_build_array($1::uuid))`, request, human, humanMember); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO system.engine_access_initial_grant_outcomes(tenant_id,producer_owner,execution_id,outcome,denial_code,issuer_principal_id,issuer_membership_id) VALUES($1,'transfer',$2,'denied','source_access_denied',$3,$4)`, tenant, denied, issuer, issuerMember); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		snapshot := func() string {
			t.Helper()
			var value string
			if err := db.QueryRow(`SELECT jsonb_build_object('outcomes',(SELECT jsonb_agg(to_jsonb(o)-'result_id' ORDER BY execution_id) FROM system.engine_access_initial_grant_outcomes o), 'grants',(SELECT jsonb_agg(to_jsonb(g)-'production_result_id' ORDER BY request_id) FROM system.engine_access_grants g),'revocations',(SELECT jsonb_agg(to_jsonb(r) ORDER BY request_id) FROM system.engine_access_grant_revocations r))::text`).Scan(&value); err != nil {
				t.Fatal(err)
			}
			return value
		}
		before := snapshot()
		return func() {
			if snapshot() != before {
				t.Fatal("migration rewrote production issuance, denial, or withdrawal history")
			}
			var mapped int
			if err := db.QueryRow(`SELECT count(*) FROM system.engine_access_initial_grant_outcomes WHERE result_id=execution_id`).Scan(&mapped); err != nil || mapped != 2 {
				t.Fatalf("historical results=%d err=%v", mapped, err)
			}
			var original uuid.UUID
			if err := db.QueryRow(`SELECT production_result_id FROM system.engine_access_grants WHERE request_id=$1`, request).Scan(&original); err != nil || original != execution {
				t.Fatal("original grant lost its result identity", original, err)
			}
			if _, err := db.Exec(`UPDATE system.engine_access_initial_grant_outcomes SET denial_code='production_ineligible' WHERE execution_id=$1`, denied); err == nil {
				t.Fatal("migration removed terminal immutability")
			}
		}
	})
}
