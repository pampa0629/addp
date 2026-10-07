package migration

import (
	"database/sql"
	"testing"

	"github.com/addp/common/authorization"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/google/uuid"
)

func TestUnifiedSourceGrantsForwardMigrationAgainstPostgres(t *testing.T) {
	testCoordinationForwardMigration(t, "000195_unified_source_grants.up.sql", 195, "engine_access_grants", func(db *sql.DB) func() {
		principalID, tenantID := seedInitializedMigrationTenant(t, db, "grant_projection", "Grant projection")
		var engineID, memberID, callerID int64
		if err := db.QueryRow(`INSERT INTO system.engines (tenant_id,name,engine_type,connection_info,identity_key,lifecycle_state,is_builtin)
			VALUES ($1,'No source connection','postgresql','{}','{"database":"grant-projection"}','active',false) RETURNING id`, tenantID).Scan(&engineID); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE tenant_id=$1 AND principal_id=$2`, tenantID, principalID).Scan(&memberID); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT id FROM system.service_principals WHERE name='addp-catalog'`).Scan(&callerID); err != nil {
			t.Fatal(err)
		}
		path, err := authorization.EncodeSharingTarget(engineplugin.TabularItemPath(uint(engineID), "schema", "public", "preserved"))
		if err != nil {
			t.Fatal(err)
		}
		id := uuid.New()
		// Seed the pre-upgrade immutable protocol, not a synthetic independent
		// approval. The test never connects a business engine.
		if _, err := db.Exec(`INSERT INTO system.engine_access_fulfillment_outcomes
			(request_id,tenant_id,engine_id,caller_principal_id,catalog_path,binding,expiry_mode,grant_expires_at,outcome,recorded_at,deadline)
			SELECT $1::uuid,$2,$3,$4,$5::jsonb,jsonb_build_object(
				'operator',jsonb_build_object('principal_id',p.id,'tenant_membership_id',$6::bigint,'authorization_version',p.authorization_version),
				'decision_id',($1::uuid)::text,'requirement_version',1,'recipient_type','user','recipient_id',p.id,
				'action','read','expiry_mode','until_revoked','expires_at',NULL),
				'until_revoked',NULL,'accepted',statement_timestamp(),statement_timestamp()+interval '5 minutes'
			FROM system.principals p WHERE p.id=$7`, id, tenantID, engineID, callerID, string(path), memberID, principalID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO system.engine_access_grants(request_id,granted_at) VALUES($1,clock_timestamp())`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO system.engine_access_grant_revocations
			(request_id,revoked_by_principal_id,revoked_by_membership_id,revoked_at,reason)
			VALUES($1,$2,$3,clock_timestamp(),'Preserved withdrawal')`, id, principalID, memberID); err != nil {
			t.Fatal(err)
		}
		snapshot := func() string {
			t.Helper()
			var value string
			if err := db.QueryRow(`SELECT jsonb_build_object(
				'issuance',(SELECT jsonb_build_object('request_id',request_id,'granted_at',granted_at) FROM system.engine_access_grants WHERE request_id=$1),
				'receipt',(SELECT to_jsonb(o) FROM system.engine_access_fulfillment_outcomes o WHERE request_id=$1),
				'revocation',(SELECT to_jsonb(r) FROM system.engine_access_grant_revocations r WHERE request_id=$1),
				'audits',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM system.audit_logs a))::text`, id).Scan(&value); err != nil {
				t.Fatal(err)
			}
			return value
		}
		original := snapshot()
		return func() {
			if snapshot() != original {
				t.Fatal("migration changed issuance, receipt, withdrawal or audits")
			}
			var matches bool
			if err := db.QueryRow(`SELECT g.approval_mode='catalog' AND g.catalog_request_id=o.request_id
				AND g.tenant_id=o.tenant_id AND g.engine_id=o.engine_id AND g.catalog_path=o.catalog_path
				AND g.recipient_type=o.binding->>'recipient_type' AND g.recipient_id=(o.binding->>'recipient_id')::bigint
				AND g.action=o.binding->>'action' AND g.expiry_mode=o.expiry_mode AND g.expires_at IS NOT DISTINCT FROM o.grant_expires_at
				AND g.requirement_version=(o.binding->>'requirement_version')::bigint
				AND g.operator_principal_id=(o.binding->'operator'->>'principal_id')::bigint
				AND g.operator_membership_id=(o.binding->'operator'->>'tenant_membership_id')::bigint
				AND g.operator_authorization_version=(o.binding->'operator'->>'authorization_version')::bigint AND g.reason IS NULL
				FROM system.engine_access_grants g JOIN system.engine_access_fulfillment_outcomes o ON o.request_id=g.catalog_request_id
				WHERE g.request_id=$1`, id).Scan(&matches); err != nil || !matches {
				t.Fatalf("canonical projection mismatch: %t %v", matches, err)
			}
			if _, err := db.Exec(`UPDATE system.engine_access_grants SET recipient_id=recipient_id+1 WHERE request_id=$1`, id); err == nil {
				t.Fatal("canonical source rule is mutable")
			}
			if _, err := db.Exec(`DELETE FROM system.engine_access_grants WHERE request_id=$1`, id); err == nil {
				t.Fatal("canonical source rule can be deleted")
			}
		}
	})
}
