package migration

import (
	"database/sql"
	"testing"

	"github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestSourceGrantRelationsForwardMigrationAgainstPostgres(t *testing.T) {
	testCoordinationForwardMigration(t, "000202_source_grant_relations.up.sql", 202, "engine_access_grant_revocations", func(db *sql.DB) func() {
		principal, tenant := seedInitializedMigrationTenant(t, db, "relation_upgrade", "Relation upgrade")
		var engine, member, caller int64
		if err := db.QueryRow(`INSERT INTO system.engines(tenant_id,name,engine_type,connection_info,identity_key,lifecycle_state,is_builtin)
 VALUES($1,'Structure fixture','postgresql','{}','{"database":"relation-upgrade"}','active',false) RETURNING id`, tenant).Scan(&engine); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE tenant_id=$1 AND principal_id=$2`, tenant, principal).Scan(&member); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT id FROM system.service_principals WHERE name='addp-catalog'`).Scan(&caller); err != nil {
			t.Fatal(err)
		}
		path, err := authorization.EncodeSharingTarget(engineplugin.TabularItemPath(uint(engine), "schema", "outdoor", "relations"))
		if err != nil {
			t.Fatal(err)
		}
		seedReceipt := func(id uuid.UUID) {
			t.Helper()
			if _, err := db.Exec(`INSERT INTO system.engine_access_fulfillment_outcomes
 (request_id,tenant_id,engine_id,caller_principal_id,catalog_path,binding,expiry_mode,outcome,recorded_at,deadline)
 SELECT $1,$2,$3,$4,$5::jsonb,jsonb_build_object('operator',jsonb_build_object('principal_id',p.id,
 'tenant_membership_id',$6::bigint,'authorization_version',p.authorization_version),'decision_id',($1::uuid)::text,
 'requirement_version',1,'recipient_type','user','recipient_id',p.id,'action','read','expiry_mode','until_revoked','expires_at',NULL),
 'until_revoked','accepted',statement_timestamp(),statement_timestamp()+interval '5 minutes'
 FROM system.principals p WHERE p.id=$7`, id, tenant, engine, caller, string(path), member, principal); err != nil {
				t.Fatal(err)
			}
		}
		ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
		for _, id := range ids {
			seedReceipt(id)
			if _, err := db.Exec(`INSERT INTO system.engine_access_grants(request_id,approval_mode) VALUES($1,'catalog')`, id); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.Exec(`INSERT INTO system.engine_access_grant_revocations(request_id,revoked_by_principal_id,revoked_by_membership_id,reason)
 VALUES($1,$2,$3,'Preserved withdrawal')`, ids[2], principal, member); err != nil {
			t.Fatal(err)
		}
		snapshot := func() string {
			t.Helper()
			var value string
			if err := db.QueryRow(`SELECT jsonb_build_object('grants',(SELECT jsonb_agg(to_jsonb(g) ORDER BY request_id) FROM system.engine_access_grants g),
 'revocations',(SELECT jsonb_agg(to_jsonb(r)-'revoked_request_ids' ORDER BY request_id) FROM system.engine_access_grant_revocations r),
 'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM system.audit_logs a))::text`).Scan(&value); err != nil {
				t.Fatal(err)
			}
			return value
		}
		before := snapshot()
		return func() {
			if snapshot() != before {
				t.Fatal("upgrade rewrote or removed authorization history")
			}
			var set string
			if err := db.QueryRow(`SELECT revoked_request_ids::text FROM system.engine_access_grant_revocations WHERE request_id=$1`, ids[2]).Scan(&set); err != nil || set != `["`+ids[2].String()+`"]` {
				t.Fatalf("withdrawal set=%s %v", set, err)
			}
			duplicate := uuid.New()
			seedReceipt(duplicate)
			if _, err := db.Exec(`INSERT INTO system.engine_access_grants(request_id,approval_mode) VALUES($1,'catalog')`, duplicate); err == nil {
				t.Fatal("upgrade permits another effective duplicate")
			}
		}
	})
}
