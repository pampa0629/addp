package migration

import (
	"database/sql"
	"testing"
)

func TestMonitorCDCRiskForwardMigrationAgainstPostgres(t *testing.T) {
	testAuthorizationFactsMigration(t, "000292_monitor_cdc_risk_observation.up.sql", 292, 1, func(db *sql.DB) func() {
		var principal, priorVersion, family int64
		if err := db.QueryRow(`SELECT p.id,p.authorization_version FROM system.principals p JOIN system.service_principals s ON s.id=p.id WHERE s.name='addp-monitor' AND s.owner_scope='platform'`).Scan(&principal, &priorVersion); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`INSERT INTO system.refresh_token_families(protocol_request_id,principal_id,context_type,issued_authorization_version,client_id,auth_type,audiences,scopes,authentication_methods,assurance_level,authenticated_at,expires_at)
            VALUES('29200000-0000-4000-8000-000000000001',$1,'platform',$2,'addp-monitor','oauth',ARRAY['addp.api'],ARRAY['addp.api'],ARRAY['service_secret'],'not_applicable',now(),now()+interval '5 minutes') RETURNING id`, principal, priorVersion).Scan(&family); err != nil {
			t.Fatal(err)
		}
		snapshot := func() string {
			t.Helper()
			var value string
			if err := db.QueryRow(`SELECT jsonb_build_object(
                'roles',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM system.roles r),
                'bindings',(SELECT jsonb_agg(to_jsonb(b) ORDER BY b.role_id,b.permission_id) FROM system.role_permissions b JOIN system.roles r ON r.id=b.role_id WHERE r.role_key<>'platform.monitor_runtime'),
                'principals',(SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM system.principals p WHERE p.id<>$1),
                'assignments',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM system.role_assignments a),
                'clients',(SELECT jsonb_agg(to_jsonb(c) ORDER BY c.client_id) FROM system.oauth_clients c),
                'families',(SELECT jsonb_agg(to_jsonb(f) ORDER BY f.id) FROM system.refresh_token_families f WHERE f.id<>$2)
                )::text`, principal, family).Scan(&value); err != nil {
				t.Fatal(err)
			}
			return value
		}
		original := snapshot()
		return func() {
			if snapshot() != original {
				t.Fatal("CDC migration changed unrelated roles, principals, assignments, clients or tokens")
			}
			var count int
			if err := db.QueryRow(`SELECT count(*) FROM system.permissions p JOIN system.role_permissions b ON b.permission_id=p.id JOIN system.roles r ON r.id=b.role_id
                WHERE p.permission_key='transfer.cdc_risk_observation.read' AND p.owner_module='transfer' AND p.action='read' AND p.risk_level='low'
                AND p.allowed_scope_types=ARRAY['platform']::text[] AND NOT p.tenant_customizable AND NOT p.delegable AND p.status='active'
                AND r.tenant_id IS NULL AND r.role_key='platform.monitor_runtime'`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("missing exact CDC runtime permission: %d %v", count, err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions b JOIN system.permissions p ON p.id=b.permission_id WHERE p.permission_key='transfer.cdc_risk_observation.read'`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("CDC permission escaped machine role: %d %v", count, err)
			}
			var version int64
			if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id=$1`, principal).Scan(&version); err != nil || version != priorVersion+1 {
				t.Fatalf("service authority/repeated startup mismatch: %d %v", version, err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM system.refresh_token_families WHERE id=$1 AND revoked_at IS NOT NULL AND revoked_reason='authorization_catalog_changed'`, family).Scan(&count); err != nil || count != 1 {
				t.Fatalf("old Monitor token not revoked: %d %v", count, err)
			}
		}
	})
}
