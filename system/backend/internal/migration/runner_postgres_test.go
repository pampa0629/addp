package migration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRunnerAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_IAM_MIGRATION_TEST_DSN")
	if dsn == "" {
		t.Skip("set ADDP_IAM_MIGRATION_TEST_DSN to a disposable PostgreSQL 15+ database")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE`); err != nil {
		t.Fatalf("reset test schema: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	runner := NewRunner(dsn)
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	var version int
	var dirty bool
	if err := db.QueryRow(`SELECT version, dirty FROM system.schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version != 11 || dirty {
		t.Fatalf("migration state = (%d, %t), want (11, false)", version, dirty)
	}

	assertIAMCatalogSeed(t, db)
	assertIdentityTenantConstraints(t, db)
	assertFederationOrganizationConstraints(t, db)
	assertAuthorizationGovernanceConstraints(t, db)
	assertSessionTokenFamilyConstraints(t, db)
	assertOAuthFositeStorageConstraints(t, db)
	assertAuditContextConstraints(t, db)
	assertInvitationEnrollmentConstraints(t, db)
	assertMFABootstrapConstraints(t, db)
	assertForeignKeyColumnsIndexed(t, db)

	if _, err := db.Exec(`UPDATE system.schema_migrations SET dirty = true`); err != nil {
		t.Fatalf("mark migration dirty: %v", err)
	}
	if err := runner.Run(ctx); err == nil || !strings.Contains(err.Error(), "is dirty") {
		t.Fatalf("Run() error = %v, want dirty-state rejection", err)
	}
	if _, err := db.Exec(`UPDATE system.schema_migrations SET version = 12, dirty = false`); err != nil {
		t.Fatalf("set newer migration version: %v", err)
	}
	if err := runner.Run(ctx); err == nil || !strings.Contains(err.Error(), "newer than embedded") {
		t.Fatalf("Run() error = %v, want newer-version rejection", err)
	}

	if _, err := db.Exec(`DROP SCHEMA system CASCADE; CREATE SCHEMA system; CREATE TABLE system.users (id bigint PRIMARY KEY)`); err != nil {
		t.Fatalf("prepare legacy schema: %v", err)
	}
	if err := runner.Run(ctx); err == nil || !strings.Contains(err.Error(), "legacy system IAM schema") {
		t.Fatalf("Run() error = %v, want legacy-schema rejection", err)
	}
}

func assertMFABootstrapConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	principalID := createMigrationUser(t, db, "MFA Bootstrap User")
	var credentialID int64
	if err := db.QueryRow(`
		INSERT INTO system.mfa_credentials
		    (user_id, method, secret_ciphertext, secret_nonce, key_version)
		VALUES ($1, 'totp', decode(repeat('ab', 32), 'hex'), decode(repeat('cd', 12), 'hex'), 1)
		RETURNING id
	`, principalID).Scan(&credentialID); err != nil {
		t.Fatalf("create MFA credential: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.mfa_credentials
		    (user_id, method, secret_ciphertext, secret_nonce, key_version)
		VALUES ($1, 'totp', decode(repeat('ef', 32), 'hex'), decode(repeat('01', 12), 'hex'), 1)
	`, principalID); err == nil {
		t.Fatal("duplicate TOTP credential succeeded")
	}
	if _, err := db.Exec(`UPDATE system.mfa_credentials SET last_accepted_counter = 100 WHERE id = $1`, credentialID); err != nil {
		t.Fatalf("record MFA counter: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.mfa_credentials SET last_accepted_counter = 100 WHERE id = $1`, credentialID); err == nil {
		t.Fatal("replayed MFA counter succeeded")
	}

	var challengeID int64
	if err := db.QueryRow(`
		INSERT INTO system.mfa_challenges
		    (token_hash, principal_id, issued_authorization_version,
		     authentication_methods, authenticated_at, expires_at)
		VALUES ($1, $2, 1, ARRAY['password'], now(), now() + interval '5 minutes')
		RETURNING id
	`, tokenHash('d'), principalID).Scan(&challengeID); err != nil {
		t.Fatalf("create MFA challenge: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.mfa_challenges SET consumed_at = now() WHERE id = $1`, challengeID); err != nil {
		t.Fatalf("consume MFA challenge: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.mfa_challenges SET consumed_at = now() WHERE id = $1`, challengeID); err == nil {
		t.Fatal("MFA challenge was consumed twice")
	}

	if _, err := db.Exec(`
		INSERT INTO system.iam_bootstrap_state
		    (status, secret_hash, prepared_at, expires_at)
		VALUES ('prepared', $1, now(), now() + interval '1 hour')
	`, tokenHash('e')); err != nil {
		t.Fatalf("prepare IAM bootstrap: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.iam_bootstrap_state
		    (status, secret_hash, prepared_at, expires_at)
		VALUES ('prepared', $1, now(), now() + interval '1 hour')
	`, tokenHash('f')); err == nil {
		t.Fatal("second IAM bootstrap state succeeded")
	}
	if _, err := db.Exec(`
		UPDATE system.iam_bootstrap_state
		SET status = 'completed', secret_hash = NULL, completed_at = now()
	`); err != nil {
		t.Fatalf("complete IAM bootstrap: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.iam_bootstrap_state SET status = 'prepared', secret_hash = $1, completed_at = NULL`, tokenHash('a')); err == nil {
		t.Fatal("completed IAM bootstrap reopened")
	}
	if _, err := db.Exec(`DELETE FROM system.mfa_credentials WHERE id = $1`, credentialID); err == nil {
		t.Fatal("MFA credential physical delete succeeded")
	}
	if _, err := db.Exec(`DELETE FROM system.mfa_challenges WHERE id = $1`, challengeID); err == nil {
		t.Fatal("MFA challenge physical delete succeeded")
	}
	if _, err := db.Exec(`DELETE FROM system.iam_bootstrap_state`); err == nil {
		t.Fatal("IAM bootstrap state physical delete succeeded")
	}
}

func assertInvitationEnrollmentConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	var tenantID, creatorPrincipalID int64
	if err := db.QueryRow(`SELECT id FROM system.tenants WHERE code = 'migration-test'`).Scan(&tenantID); err != nil {
		t.Fatalf("find invitation test tenant: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.users WHERE display_name = 'Migration User'`).Scan(&creatorPrincipalID); err != nil {
		t.Fatalf("find invitation creator: %v", err)
	}
	invitedPrincipalID := createMigrationUser(t, db, "Invitation User")

	var invitationID int64
	if err := db.QueryRow(`
		INSERT INTO system.tenant_invitations
		    (tenant_id, email, normalized_email, secret_hash, expires_at, created_by_principal_id)
		VALUES ($1, 'Invitee@Example.Test', 'invitee@example.test', $2, now() + interval '7 days', $3)
		RETURNING id
	`, tenantID, tokenHash('a'), creatorPrincipalID).Scan(&invitationID); err != nil {
		t.Fatalf("create tenant invitation: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.tenant_invitations
		    (tenant_id, email, normalized_email, secret_hash, expires_at, created_by_principal_id)
		VALUES ($1, 'INVITEE@example.test', 'invitee@example.test', $2, now() + interval '7 days', $3)
	`, tenantID, tokenHash('b'), creatorPrincipalID); err == nil {
		t.Fatal("duplicate pending invitation for tenant and normalized email succeeded")
	}
	if _, err := db.Exec(`
		INSERT INTO system.tenant_invitations
		    (tenant_id, email, normalized_email, secret_hash, expires_at, created_by_principal_id)
		VALUES ($1, 'bad@example.test', 'bad@example.test', 'not-a-hash', now() + interval '7 days', $2)
	`, tenantID, creatorPrincipalID); err == nil {
		t.Fatal("tenant invitation accepted a non-SHA256 secret hash")
	}

	var authorizationVersion int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id = $1`, invitedPrincipalID).Scan(&authorizationVersion); err != nil {
		t.Fatalf("read invited principal authorization version: %v", err)
	}
	var ticketID int64
	if err := db.QueryRow(`
		INSERT INTO system.enrollment_tickets
		    (token_hash, principal_id, invitation_id, issued_authorization_version, authenticated_at, expires_at)
		VALUES ($1, $2, $3, $4, now(), now() + interval '5 minutes')
		RETURNING id
	`, tokenHash('c'), invitedPrincipalID, invitationID, authorizationVersion).Scan(&ticketID); err != nil {
		t.Fatalf("create enrollment ticket: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.enrollment_tickets SET principal_id = $1, consumed_at = now() WHERE id = $2`, creatorPrincipalID, ticketID); err == nil {
		t.Fatal("enrollment ticket principal binding update succeeded")
	}
	if _, err := db.Exec(`UPDATE system.enrollment_tickets SET consumed_at = now() WHERE id = $1`, ticketID); err != nil {
		t.Fatalf("consume enrollment ticket: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.enrollment_tickets SET consumed_at = now() WHERE id = $1`, ticketID); err == nil {
		t.Fatal("enrollment ticket was consumed twice")
	}

	if _, err := db.Exec(`
		UPDATE system.tenant_invitations
		SET status = 'accepted', accepted_at = now(), accepted_by_principal_id = $1
		WHERE id = $2
	`, invitedPrincipalID, invitationID); err != nil {
		t.Fatalf("accept tenant invitation: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.tenant_invitations SET status = 'pending', accepted_at = NULL, accepted_by_principal_id = NULL WHERE id = $1`, invitationID); err == nil {
		t.Fatal("accepted tenant invitation returned to pending")
	}
	if _, err := db.Exec(`
		INSERT INTO system.tenant_memberships
		    (tenant_id, principal_id, source_type, source_ref, joined_at, created_by_principal_id)
		VALUES ($1, $2, 'invitation', $3, now(), $4)
	`, tenantID, invitedPrincipalID, fmt.Sprint(invitationID), creatorPrincipalID); err != nil {
		t.Fatalf("create invitation-sourced tenant membership: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM system.tenant_invitations WHERE id = $1`, invitationID); err == nil {
		t.Fatal("tenant invitation physical delete succeeded")
	}
	if _, err := db.Exec(`DELETE FROM system.enrollment_tickets WHERE id = $1`, ticketID); err == nil {
		t.Fatal("enrollment ticket physical delete succeeded")
	}
	if _, err := db.Exec(`TRUNCATE system.enrollment_tickets`); err == nil {
		t.Fatal("enrollment ticket truncate succeeded")
	}
}

func assertAuditContextConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	var tenantID, userPrincipalID int64
	if err := db.QueryRow(`SELECT id FROM system.tenants WHERE code = 'migration-test'`).Scan(&tenantID); err != nil {
		t.Fatalf("find audit test tenant: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.users WHERE display_name = 'Migration User'`).Scan(&userPrincipalID); err != nil {
		t.Fatalf("find audit test user: %v", err)
	}

	var auditLogID int64
	if err := db.QueryRow(`
		INSERT INTO system.audit_logs
		    (principal_id, principal_type, context_type, tenant_id,
		     event_name, result, risk_level, module_name,
		     http_method, resource_path, http_status, request_id, ip_address,
		     entity_type, entity_id, details)
		VALUES
		    ($1, 'user', 'tenant', $2,
		     'iam.tenant_membership.suspended', 'succeeded', 'high', 'system',
		     'POST', '/api/v1/system/tenant-memberships/1/suspend', 200,
		     'audit-request-1', '127.0.0.1', 'tenant_membership', '1',
		     '{"reason":"security review","authorization_version":3}'::jsonb)
		RETURNING id
	`, userPrincipalID, tenantID).Scan(&auditLogID); err != nil {
		t.Fatalf("create tenant audit event: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (event_name, result, risk_level, module_name, entity_type, entity_id, details)
		VALUES
		    ('oauth.token.failed', 'failed', 'medium', 'system',
		     'oauth_security_event', 'oauth.token.failed',
		     '{"client_id":"addp-cli","grant_type":"refresh_token","error_code":"invalid_grant"}'::jsonb)
	`); err != nil {
		t.Fatalf("create no-context OAuth audit event: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (principal_id, principal_type, event_name, result, risk_level, module_name)
		VALUES ($1, 'service_principal', 'iam.identity.updated', 'succeeded', 'low', 'system')
	`, userPrincipalID); err == nil {
		t.Fatal("audit event accepted a mismatched principal type")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (context_type, event_name, result, risk_level, module_name)
		VALUES ('tenant', 'iam.identity.updated', 'succeeded', 'low', 'system')
	`); err == nil {
		t.Fatal("tenant audit context succeeded without tenant")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (context_type, tenant_id, event_name, result, risk_level, module_name)
		VALUES ('platform', $1, 'iam.identity.updated', 'succeeded', 'low', 'system')
	`, tenantID); err == nil {
		t.Fatal("platform audit context accepted a tenant")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (tenant_id, event_name, result, risk_level, module_name)
		VALUES ($1, 'iam.identity.updated', 'succeeded', 'low', 'system')
	`, tenantID); err == nil {
		t.Fatal("no-context audit event accepted a tenant")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (event_name, result, risk_level, module_name, details)
		VALUES
		    ('oauth.token.failed', 'failed', 'high', 'system',
		     '{"oauth":{"refresh_token":"secret"}}'::jsonb)
	`); err == nil {
		t.Fatal("audit details accepted a nested refresh token")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (event_name, result, risk_level, module_name, details)
		VALUES ('iam.identity.updated', 'succeeded', 'low', 'system', '[]'::jsonb)
	`); err == nil {
		t.Fatal("audit details accepted a non-object JSON value")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (event_name, result, risk_level, module_name,
		     entity_type, entity_id, details)
		VALUES
		    ('oauth.token.failed', 'failed', 'medium', 'system',
		     'oauth_security_event', 'oauth.token.failed',
		     '{"client_id":"addp-cli","request_secret":"secret"}'::jsonb)
	`); err == nil {
		t.Fatal("OAuth audit details accepted a field outside the whitelist")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (event_name, result, risk_level, module_name, entity_type, entity_id)
		VALUES
		    ('oauth.token.failed', 'failed', 'medium', 'system',
		     'oauth_security_event', 'oauth.token.issued')
	`); err == nil {
		t.Fatal("OAuth audit event accepted mismatched event and entity identifiers")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (event_name, result, risk_level, module_name, entity_type, entity_id)
		VALUES
		    ('oauth.token.refresh_reuse_detected', 'failed', 'medium', 'system',
		     'oauth_security_event', 'oauth.token.refresh_reuse_detected')
	`); err == nil {
		t.Fatal("refresh token reuse audit event accepted a non-high risk level")
	}
	if _, err := db.Exec(`
		INSERT INTO system.audit_logs
		    (event_name, result, risk_level, module_name, http_method)
		VALUES ('iam.identity.updated', 'succeeded', 'low', 'system', 'POST')
	`); err == nil {
		t.Fatal("audit event accepted a partial HTTP context")
	}

	if _, err := db.Exec(`UPDATE system.audit_logs SET result = 'failed' WHERE id = $1`, auditLogID); err == nil {
		t.Fatal("audit log update succeeded")
	}
	if _, err := db.Exec(`DELETE FROM system.audit_logs WHERE id = $1`, auditLogID); err == nil {
		t.Fatal("audit log delete succeeded")
	}
	if _, err := db.Exec(`TRUNCATE system.audit_logs`); err == nil {
		t.Fatal("audit log truncate succeeded")
	}

	var columns string
	if err := db.QueryRow(`
		SELECT string_agg(column_name, ',' ORDER BY ordinal_position)
		FROM information_schema.columns
		WHERE table_schema = 'system' AND table_name = 'audit_logs'
	`).Scan(&columns); err != nil {
		t.Fatalf("read audit log columns: %v", err)
	}
	wantColumns := "id,principal_id,principal_type,context_type,tenant_id,event_name,result,risk_level,module_name,http_method,resource_path,http_status,request_id,ip_address,user_agent,entity_type,entity_id,details,created_at"
	if columns != wantColumns {
		t.Fatalf("audit log columns = %q, want %q", columns, wantColumns)
	}

	var auditTableCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM information_schema.tables
		WHERE table_schema = 'system'
		  AND table_type = 'BASE TABLE'
		  AND table_name LIKE '%audit%'
	`).Scan(&auditTableCount); err != nil {
		t.Fatalf("count audit tables: %v", err)
	}
	if auditTableCount != 1 {
		t.Fatalf("system audit table count = %d, want 1", auditTableCount)
	}

	var indexes string
	if err := db.QueryRow(`
		SELECT string_agg(indexname, ',' ORDER BY indexname)
		FROM pg_indexes
		WHERE schemaname = 'system'
		  AND tablename = 'audit_logs'
		  AND indexname <> 'audit_logs_pkey'
	`).Scan(&indexes); err != nil {
		t.Fatalf("read audit log indexes: %v", err)
	}
	wantIndexes := "idx_audit_logs_created_at,idx_audit_logs_entity,idx_audit_logs_event_created_at,idx_audit_logs_high_risk_created_at,idx_audit_logs_principal_created_at,idx_audit_logs_request_id,idx_audit_logs_tenant_created_at"
	if indexes != wantIndexes {
		t.Fatalf("audit log indexes = %q, want %q", indexes, wantIndexes)
	}

	var publicMutationPrivilegeCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM information_schema.role_table_grants
		WHERE table_schema = 'system'
		  AND table_name = 'audit_logs'
		  AND grantee = 'PUBLIC'
		  AND privilege_type IN ('UPDATE', 'DELETE', 'TRUNCATE')
	`).Scan(&publicMutationPrivilegeCount); err != nil {
		t.Fatalf("read public audit privileges: %v", err)
	}
	if publicMutationPrivilegeCount != 0 {
		t.Fatalf("public audit mutation privilege count = %d, want 0", publicMutationPrivilegeCount)
	}
}

func assertIAMCatalogSeed(t *testing.T, db *sql.DB) {
	t.Helper()

	assertTableCount(t, db, "system.permissions", 243)
	assertTableCount(t, db, "system.roles", 18)
	assertTableCount(t, db, "system.role_permissions", 278)
	assertTableCount(t, db, "system.role_conflicts", 3)
	assertTableCount(t, db, "system.oauth_clients", 1)
	assertTableCount(t, db, "system.principals", 0)
	assertTableCount(t, db, "system.tenants", 0)
	assertTableCount(t, db, "system.role_assignments", 0)

	var ownerCount, systemPermissionCount int
	if err := db.QueryRow(`SELECT count(DISTINCT owner_module), count(*) FILTER (WHERE owner_module = 'system') FROM system.permissions`).Scan(&ownerCount, &systemPermissionCount); err != nil {
		t.Fatalf("read seeded Permission owners: %v", err)
	}
	if ownerCount != 15 || systemPermissionCount != 105 {
		t.Fatalf("seeded Permission owners = %d and System Permissions = %d, want 15 and 105", ownerCount, systemPermissionCount)
	}

	var invalidRoleCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.roles
		WHERE tenant_id IS NOT NULL
		   OR role_type NOT IN ('platform_builtin', 'tenant_builtin')
		   OR NOT immutable
		   OR status <> 'active'
		   OR created_by_principal_id IS NOT NULL
	`).Scan(&invalidRoleCount); err != nil {
		t.Fatalf("validate seeded builtin Roles: %v", err)
	}
	if invalidRoleCount != 0 {
		t.Fatalf("invalid seeded builtin Role count = %d", invalidRoleCount)
	}

	var invalidRolePermissionCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM system.role_permissions role_permission
		JOIN system.permissions permission ON permission.id = role_permission.permission_id
		WHERE role_permission.source_type <> 'product'
		   OR role_permission.created_by_principal_id IS NOT NULL
		   OR permission.status <> 'active'
	`).Scan(&invalidRolePermissionCount); err != nil {
		t.Fatalf("validate seeded Role Permissions: %v", err)
	}
	if invalidRolePermissionCount != 0 {
		t.Fatalf("invalid seeded Role Permission count = %d", invalidRolePermissionCount)
	}

	var conflicts string
	if err := db.QueryRow(`
		SELECT string_agg(low_role.role_key || ':' || high_role.role_key, ',' ORDER BY low_role.role_key, high_role.role_key)
		FROM system.role_conflicts conflict
		JOIN system.roles low_role ON low_role.id = conflict.role_id_low
		JOIN system.roles high_role ON high_role.id = conflict.role_id_high
		WHERE conflict.reason = 'platform_three_administrators_separation_of_duties'
	`).Scan(&conflicts); err != nil {
		t.Fatalf("read platform administrator conflicts: %v", err)
	}
	wantConflicts := "platform.audit_administrator:platform.security_administrator," +
		"platform.audit_administrator:platform.system_administrator," +
		"platform.security_administrator:platform.system_administrator"
	if conflicts != wantConflicts {
		t.Fatalf("platform administrator conflicts = %q, want %q", conflicts, wantConflicts)
	}

	var validClientCount, firstPartyClientCount int
	if err := db.QueryRow(`
		SELECT
		    count(*) FILTER (
		        WHERE client_id = 'addp-cli'
		          AND display_name = 'ADDP CLI'
		          AND client_type = 'public'
		          AND client_secret_hash IS NULL
		          AND redirect_uris = ARRAY['http://127.0.0.1/callback']::text[]
		          AND grant_types = ARRAY['authorization_code', 'refresh_token', 'urn:ietf:params:oauth:grant-type:device_code']::text[]
		          AND response_types = ARRAY['code']::text[]
		          AND allowed_scopes = ARRAY['addp.api']::text[]
		          AND allowed_audiences = ARRAY['addp.api']::text[]
		          AND token_endpoint_auth_method = 'none'
		          AND status = 'active'
		    ),
		    count(*) FILTER (WHERE client_id = 'addp-web')
		FROM system.oauth_clients
	`).Scan(&validClientCount, &firstPartyClientCount); err != nil {
		t.Fatalf("validate seeded OAuth Client: %v", err)
	}
	if validClientCount != 1 || firstPartyClientCount != 0 {
		t.Fatalf("seeded OAuth Client counts = valid:%d addp-web:%d, want 1 and 0", validClientCount, firstPartyClientCount)
	}
}

func assertTableCount(t *testing.T, db *sql.DB, tableName string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT count(*) FROM ` + tableName).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", tableName, err)
	}
	if got != want {
		t.Fatalf("%s count = %d, want %d", tableName, got, want)
	}
}

func assertSessionTokenFamilyConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	var tenantID, tenantUserID, tenantMembershipID, otherMembershipID, authorizationVersion int64
	if err := db.QueryRow(`SELECT id FROM system.tenants WHERE code = 'migration-test'`).Scan(&tenantID); err != nil {
		t.Fatalf("find session test tenant: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.users WHERE display_name = 'Migration User'`).Scan(&tenantUserID); err != nil {
		t.Fatalf("find session test user: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE tenant_id = $1 AND principal_id = $2`, tenantID, tenantUserID).Scan(&tenantMembershipID); err != nil {
		t.Fatalf("find session test membership: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE tenant_id = $1 AND principal_id <> $2 LIMIT 1`, tenantID, tenantUserID).Scan(&otherMembershipID); err != nil {
		t.Fatalf("find other tenant membership: %v", err)
	}
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id = $1`, tenantUserID).Scan(&authorizationVersion); err != nil {
		t.Fatalf("read session test authorization version: %v", err)
	}

	var selectionTicketID int64
	if err := db.QueryRow(`
		INSERT INTO system.context_selection_tickets
		    (token_hash, principal_id, issued_authorization_version, authentication_methods, assurance_level, authenticated_at, expires_at)
		VALUES ($1, $2, $3, ARRAY['password'], 'aal1', now() - interval '1 minute', now() + interval '5 minutes')
		RETURNING id
	`, tokenHash('a'), tenantUserID, authorizationVersion).Scan(&selectionTicketID); err != nil {
		t.Fatalf("create context selection ticket: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.context_selection_ticket_options (ticket_id, context_type, tenant_membership_id)
		VALUES ($1, 'tenant', $2)
	`, selectionTicketID, tenantMembershipID); err != nil {
		t.Fatalf("create tenant context option: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.context_selection_ticket_options (ticket_id, context_type, tenant_membership_id)
		VALUES ($1, 'tenant', $2)
	`, selectionTicketID, otherMembershipID); err == nil {
		t.Fatal("context option accepted another principal's tenant membership")
	}
	if _, err := db.Exec(`UPDATE system.context_selection_tickets SET consumed_at = now() WHERE id = $1`, selectionTicketID); err != nil {
		t.Fatalf("consume context selection ticket: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.context_selection_tickets SET consumed_at = now() WHERE id = $1`, selectionTicketID); err == nil {
		t.Fatal("context selection ticket was consumed twice")
	}

	platformUserID := createMigrationUser(t, db, "Session Platform User")
	var platformRoleID int64
	if err := db.QueryRow(`SELECT id FROM system.roles WHERE role_key = 'platform.test_administrator'`).Scan(&platformRoleID); err != nil {
		t.Fatalf("find platform role for session test: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.role_assignments (principal_id, role_id, scope_type, source_type)
		VALUES ($1, $2, 'platform', 'bootstrap')
	`, platformUserID, platformRoleID); err != nil {
		t.Fatalf("bootstrap platform role for session test: %v", err)
	}
	var platformVersion int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id = $1`, platformUserID).Scan(&platformVersion); err != nil {
		t.Fatalf("read platform session authorization version: %v", err)
	}
	var lowAALTicketID int64
	if err := db.QueryRow(`
		INSERT INTO system.context_selection_tickets
		    (token_hash, principal_id, issued_authorization_version, authentication_methods, assurance_level, authenticated_at, expires_at)
		VALUES ($1, $2, $3, ARRAY['password'], 'aal1', now() - interval '1 minute', now() + interval '5 minutes')
		RETURNING id
	`, tokenHash('b'), platformUserID, platformVersion).Scan(&lowAALTicketID); err != nil {
		t.Fatalf("create low-AAL platform ticket: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.context_selection_ticket_options (ticket_id, context_type) VALUES ($1, 'platform')`, lowAALTicketID); err == nil {
		t.Fatal("platform context option accepted aal1")
	}
	var platformTicketID int64
	if err := db.QueryRow(`
		INSERT INTO system.context_selection_tickets
		    (token_hash, principal_id, issued_authorization_version, authentication_methods, assurance_level, authenticated_at, expires_at)
		VALUES ($1, $2, $3, ARRAY['password', 'totp'], 'aal2', now() - interval '1 minute', now() + interval '5 minutes')
		RETURNING id
	`, tokenHash('c'), platformUserID, platformVersion).Scan(&platformTicketID); err != nil {
		t.Fatalf("create platform context ticket: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.context_selection_ticket_options (ticket_id, context_type) VALUES ($1, 'platform')`, platformTicketID); err != nil {
		t.Fatalf("create platform context option: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.context_selection_tickets SET step_up_expires_at = now() WHERE id = $1`, platformTicketID); err == nil {
		t.Fatal("context selection ticket allowed step-up facts to change")
	}
	if _, err := db.Exec(`
		INSERT INTO system.context_selection_tickets
		    (token_hash, principal_id, issued_authorization_version, authentication_methods,
		     assurance_level, authenticated_at, step_up_expires_at, expires_at)
		VALUES
		    ($1, $2, $3, ARRAY['password', 'totp'], 'aal2', now() - interval '1 minute',
		     now() - interval '2 minutes', now() + interval '5 minutes')
	`, tokenHash('d'), platformUserID, platformVersion); err == nil {
		t.Fatal("context selection ticket accepted step-up expiry before authentication")
	}

	var familyID int64
	if err := db.QueryRow(`
		INSERT INTO system.refresh_token_families
		    (principal_id, context_type, tenant_membership_id, issued_authorization_version,
		     client_id, auth_type, audiences, scopes, authentication_methods, assurance_level,
		     authenticated_at, expires_at)
		VALUES
		    ($1, 'tenant', $2, $3, 'addp-web', 'first_party', ARRAY['addp.api'], ARRAY[]::text[],
		     ARRAY['password'], 'aal1', now() - interval '1 minute', now() + interval '1 hour')
		RETURNING id
	`, tenantUserID, tenantMembershipID, authorizationVersion).Scan(&familyID); err != nil {
		t.Fatalf("create first-party tenant family: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.refresh_token_families
		    (principal_id, context_type, tenant_membership_id, issued_authorization_version,
		     client_id, auth_type, audiences, scopes, authentication_methods, assurance_level,
		     authenticated_at, expires_at)
		VALUES
		    ($1, 'tenant', $2, $3, 'addp-web', 'first_party', ARRAY['addp.api'], ARRAY[]::text[],
		     ARRAY['password'], 'aal1', now() - interval '1 minute', now() + interval '1 hour')
	`, tenantUserID, tenantMembershipID, authorizationVersion-1); err == nil {
		t.Fatal("token family accepted a stale authorization version")
	}
	if _, err := db.Exec(`UPDATE system.refresh_token_families SET expires_at = expires_at + interval '1 hour' WHERE id = $1`, familyID); err == nil {
		t.Fatal("token family final expiry update succeeded")
	}

	var accessTokenID int64
	if err := db.QueryRow(`
		INSERT INTO system.access_tokens (token_hash, family_id, expires_at)
		VALUES ($1, $2, now() + interval '15 minutes')
		RETURNING id
	`, tokenHash('d'), familyID).Scan(&accessTokenID); err != nil {
		t.Fatalf("create access token: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.access_tokens (token_hash, family_id, expires_at) VALUES ($1, $2, now() + interval '15 minutes')`, tokenHash('e'), familyID); err == nil {
		t.Fatal("family accepted two active access tokens")
	}

	var refreshTokenID int64
	if err := db.QueryRow(`
		INSERT INTO system.refresh_tokens (token_hash, family_id, issued_access_token_id, expires_at)
		VALUES ($1, $2, $3, now() + interval '59 minutes')
		RETURNING id
	`, tokenHash('f'), familyID, accessTokenID).Scan(&refreshTokenID); err != nil {
		t.Fatalf("create refresh token: %v", err)
	}

	var delegatedTokenID int64
	if err := db.QueryRow(`
		INSERT INTO system.delegated_access_tokens
		    (token_hash, source_access_token_id, audience, scopes, agent_run_id, tool_call_id, expires_at)
		VALUES ($1, $2, 'develop', ARRAY['workflow.run'], 'run-session-test', 'call-session-test', now() + interval '2 minutes')
		RETURNING id
	`, tokenHash('1'), accessTokenID).Scan(&delegatedTokenID); err != nil {
		t.Fatalf("create delegated access token: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.delegated_access_tokens
		    (token_hash, source_access_token_id, audience, scopes, agent_run_id, tool_call_id, expires_at)
		VALUES ($1, $2, 'develop', ARRAY['workflow.run'], 'run-too-long', 'call-too-long', now() + interval '20 minutes')
	`, tokenHash('2'), accessTokenID); err == nil {
		t.Fatal("delegated token expiry exceeded its source access token")
	}

	var resourceTicketID int64
	if err := db.QueryRow(`
		INSERT INTO system.resource_access_tickets (token_hash, family_id, owner, expires_at)
		VALUES ($1, $2, 'manager', now() + interval '15 minutes')
		RETURNING id
	`, tokenHash('3'), familyID).Scan(&resourceTicketID); err != nil {
		t.Fatalf("create resource access ticket: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.resource_access_tickets (token_hash, family_id, owner, expires_at) VALUES ($1, $2, 'manager', now() + interval '10 minutes')`, tokenHash('4'), familyID); err == nil {
		t.Fatal("family accepted two active resource tickets for one owner")
	}

	if _, err := db.Exec(`UPDATE system.access_tokens SET revoked_at = now() WHERE id = $1`, accessTokenID); err != nil {
		t.Fatalf("revoke source access token: %v", err)
	}
	assertTimestampSet(t, db, `SELECT revoked_at FROM system.delegated_access_tokens WHERE id = $1`, delegatedTokenID, "delegated token source revocation")
	if _, err := db.Exec(`UPDATE system.refresh_tokens SET used_at = now() WHERE id = $1`, refreshTokenID); err != nil {
		t.Fatalf("mark refresh token used: %v", err)
	}

	var replacementAccessTokenID, replacementRefreshTokenID int64
	if err := db.QueryRow(`
		INSERT INTO system.access_tokens (token_hash, family_id, expires_at)
		VALUES ($1, $2, now() + interval '15 minutes')
		RETURNING id
	`, tokenHash('5'), familyID).Scan(&replacementAccessTokenID); err != nil {
		t.Fatalf("create replacement access token: %v", err)
	}
	if err := db.QueryRow(`
		INSERT INTO system.refresh_tokens
		    (token_hash, family_id, issued_access_token_id, parent_token_id, expires_at)
		VALUES ($1, $2, $3, $4, now() + interval '59 minutes')
		RETURNING id
	`, tokenHash('6'), familyID, replacementAccessTokenID, refreshTokenID).Scan(&replacementRefreshTokenID); err != nil {
		t.Fatalf("create replacement refresh token: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.refresh_tokens SET replaced_by_token_id = $1 WHERE id = $2`, replacementRefreshTokenID, refreshTokenID); err != nil {
		t.Fatalf("link refresh token replacement: %v", err)
	}

	protocolRequestID := "11111111-1111-4111-8111-111111111111"
	var oauthFamilyID int64
	if err := db.QueryRow(`
		INSERT INTO system.refresh_token_families
		    (protocol_request_id, principal_id, context_type, tenant_membership_id, issued_authorization_version,
		     client_id, auth_type, audiences, scopes, authentication_methods, assurance_level,
		     authenticated_at, expires_at)
		VALUES
		    ($1, $2, 'tenant', $3, $4, 'addp-cli', 'oauth', ARRAY['addp.api'], ARRAY['addp.api'],
		     ARRAY['password'], 'aal1', now() - interval '1 minute', now() + interval '1 hour')
		RETURNING id
	`, protocolRequestID, tenantUserID, tenantMembershipID, authorizationVersion).Scan(&oauthFamilyID); err != nil {
		t.Fatalf("create OAuth family: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.resource_access_tickets (token_hash, family_id, owner, expires_at) VALUES ($1, $2, 'manager', now() + interval '10 minutes')`, tokenHash('7'), oauthFamilyID); err == nil {
		t.Fatal("OAuth family issued a browser resource ticket")
	}

	if _, err := db.Exec(`UPDATE system.refresh_token_families SET revoked_at = now(), revoked_reason = 'session_test' WHERE id = $1`, familyID); err != nil {
		t.Fatalf("revoke token family: %v", err)
	}
	assertTimestampSet(t, db, `SELECT revoked_at FROM system.access_tokens WHERE id = $1`, replacementAccessTokenID, "family access-token revocation")
	assertTimestampSet(t, db, `SELECT revoked_at FROM system.refresh_tokens WHERE id = $1`, replacementRefreshTokenID, "family refresh-token revocation")
	assertTimestampSet(t, db, `SELECT revoked_at FROM system.resource_access_tickets WHERE id = $1`, resourceTicketID, "family resource-ticket revocation")
}

func assertOAuthFositeStorageConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	if _, err := db.Exec(`
		INSERT INTO system.oauth_clients
		    (client_id, display_name, client_type, client_secret_hash, redirect_uris, grant_types,
		     response_types, allowed_scopes, allowed_audiences, token_endpoint_auth_method)
		VALUES
		    ('bad-public', 'Bad Public Client', 'public', 'secret-hash', ARRAY['http://127.0.0.1/callback'],
		     ARRAY['authorization_code'], ARRAY['code'], ARRAY['addp.api'], ARRAY['addp.api'], 'none')
	`); err == nil {
		t.Fatal("public OAuth client accepted a client secret")
	}

	var tenantID, tenantUserID, tenantMembershipID, authorizationVersion int64
	if err := db.QueryRow(`SELECT id FROM system.tenants WHERE code = 'migration-test'`).Scan(&tenantID); err != nil {
		t.Fatalf("find OAuth test tenant: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.users WHERE display_name = 'Migration User'`).Scan(&tenantUserID); err != nil {
		t.Fatalf("find OAuth test user: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE tenant_id = $1 AND principal_id = $2`, tenantID, tenantUserID).Scan(&tenantMembershipID); err != nil {
		t.Fatalf("find OAuth test membership: %v", err)
	}
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id = $1`, tenantUserID).Scan(&authorizationVersion); err != nil {
		t.Fatalf("read OAuth test authorization version: %v", err)
	}

	authorizationRequestID := "22222222-2222-4222-8222-222222222222"
	if _, err := db.Exec(`
		INSERT INTO system.oauth_authorization_requests
		    (id, request_secret_hash, client_id, redirect_uri, response_types, response_mode,
		     requested_scopes, requested_audiences, requested_at, expires_at)
		VALUES
		    ($1, $2, 'addp-cli', 'http://127.0.0.1:49152/callback', ARRAY['code'], 'query',
		     ARRAY['unregistered.scope'], ARRAY['addp.api'], now() - interval '1 second', now() + interval '5 minutes')
	`, "21111111-1111-4111-8111-111111111111", tokenHash('8')); err == nil {
		t.Fatal("authorization request accepted an unregistered scope")
	}
	if _, err := db.Exec(`
		INSERT INTO system.oauth_authorization_requests
		    (id, request_secret_hash, client_id, redirect_uri, response_types, response_mode,
		     requested_scopes, requested_audiences, requested_at, expires_at)
		VALUES
		    ($1, $2, 'addp-cli', 'http://127.0.0.1:49152/callback', ARRAY['code'], 'query',
		     ARRAY['addp.api'], ARRAY['addp.api'], now() - interval '1 second', now() + interval '5 minutes')
	`, authorizationRequestID, tokenHash('8')); err != nil {
		t.Fatalf("create authorization request: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.oauth_pkce_sessions
		    (authorization_request_id, code_challenge, code_challenge_method, expires_at)
		VALUES ($1, 'pkce-s256-challenge', 'S256', now() + interval '4 minutes')
	`, authorizationRequestID); err != nil {
		t.Fatalf("create PKCE session: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_authorization_requests
		SET status = 'approved',
		    principal_id = $2,
		    context_type = 'tenant',
		    tenant_membership_id = $3,
		    issued_authorization_version = $4,
		    granted_scopes = ARRAY['addp.api'],
		    granted_audiences = ARRAY['addp.api'],
		    authentication_methods = ARRAY['password'],
		    assurance_level = 'aal1',
		    authenticated_at = now() - interval '1 minute',
		    completed_at = now()
		WHERE id = $1
	`, authorizationRequestID, tenantUserID, tenantMembershipID, authorizationVersion-1); err == nil {
		t.Fatal("authorization request accepted a stale authorization version")
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_authorization_requests
		SET status = 'approved',
		    principal_id = $2,
		    context_type = 'tenant',
		    tenant_membership_id = $3,
		    issued_authorization_version = $4,
		    granted_scopes = ARRAY['addp.api'],
		    granted_audiences = ARRAY['addp.api'],
		    authentication_methods = ARRAY['password'],
		    assurance_level = 'aal1',
		    authenticated_at = now() - interval '1 minute',
		    completed_at = now()
		WHERE id = $1
	`, authorizationRequestID, tenantUserID, tenantMembershipID, authorizationVersion); err != nil {
		t.Fatalf("approve authorization request: %v", err)
	}

	authorizationCodeHash := tokenHash('9')
	var authorizationCodeID int64
	if err := db.QueryRow(`
		INSERT INTO system.oauth_authorization_codes (code_hash, authorization_request_id, expires_at)
		VALUES ($1, $2, now() + interval '4 minutes')
		RETURNING id
	`, authorizationCodeHash, authorizationRequestID).Scan(&authorizationCodeID); err != nil {
		t.Fatalf("create authorization code: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_pkce_sessions
		SET authorization_code_hash = $2, verified_at = now(), consumed_at = now()
		WHERE authorization_request_id = $1
	`, authorizationRequestID, authorizationCodeHash); err != nil {
		t.Fatalf("consume PKCE session: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.oauth_authorization_codes SET invalidated_at = now() WHERE id = $1`, authorizationCodeID); err != nil {
		t.Fatalf("invalidate authorization code: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.oauth_authorization_codes SET invalidated_at = now() WHERE id = $1`, authorizationCodeID); err == nil {
		t.Fatal("authorization code was invalidated twice")
	}
	assertTimestampSet(t, db, `SELECT invalidated_at FROM system.oauth_authorization_codes WHERE id = $1`, authorizationCodeID, "authorization code invalidation")
	if _, err := db.Exec(`UPDATE system.oauth_authorization_requests SET status = 'cancelled' WHERE id = $1`, authorizationRequestID); err == nil {
		t.Fatal("terminal authorization request changed state")
	}

	if _, err := db.Exec(`
		INSERT INTO system.oauth_clients
		    (client_id, display_name, client_type, redirect_uris, grant_types, response_types,
		     allowed_scopes, allowed_audiences, token_endpoint_auth_method)
		VALUES
		    ('oidc-test', 'OIDC Storage Test', 'public', ARRAY['http://127.0.0.1/callback'],
		     ARRAY['authorization_code'], ARRAY['code'], ARRAY['openid'], ARRAY['addp.api'], 'none')
	`); err != nil {
		t.Fatalf("create OIDC storage test client: %v", err)
	}
	oidcRequestID := "33333333-3333-4333-8333-333333333333"
	if _, err := db.Exec(`
		INSERT INTO system.oauth_authorization_requests
		    (id, request_secret_hash, client_id, redirect_uri, response_types, response_mode,
		     requested_scopes, requested_audiences, requested_at, expires_at)
		VALUES
		    ($1, $2, 'oidc-test', 'http://127.0.0.1:49153/callback', ARRAY['code'], 'query',
		     ARRAY['openid'], ARRAY['addp.api'], now() - interval '1 second', now() + interval '5 minutes')
	`, oidcRequestID, tokenHash('a')); err != nil {
		t.Fatalf("create OIDC authorization request: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.oauth_oidc_sessions
		    (authorization_request_id, nonce, requested_at, expires_at)
		SELECT id, 'oidc-nonce', requested_at, now() + interval '4 minutes'
		FROM system.oauth_authorization_requests
		WHERE id = $1
	`, oidcRequestID); err != nil {
		t.Fatalf("create OIDC session: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_authorization_requests
		SET status = 'approved',
		    principal_id = $2,
		    context_type = 'tenant',
		    tenant_membership_id = $3,
		    issued_authorization_version = $4,
		    granted_scopes = ARRAY['openid'],
		    granted_audiences = ARRAY['addp.api'],
		    authentication_methods = ARRAY['password'],
		    assurance_level = 'aal1',
		    authenticated_at = now() - interval '1 minute',
		    completed_at = now()
		WHERE id = $1
	`, oidcRequestID, tenantUserID, tenantMembershipID, authorizationVersion); err != nil {
		t.Fatalf("approve OIDC authorization request: %v", err)
	}
	oidcAuthorizationCodeHash := tokenHash('d')
	if _, err := db.Exec(`
		INSERT INTO system.oauth_authorization_codes (code_hash, authorization_request_id, expires_at)
		VALUES ($1, $2, now() + interval '4 minutes')
	`, oidcAuthorizationCodeHash, oidcRequestID); err != nil {
		t.Fatalf("create OIDC authorization code: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_oidc_sessions
		SET authorization_code_hash = $2, subject = 'subject-1', authenticated_at = now() - interval '1 minute', acr = 'aal1',
		    amr = ARRAY['password'], extra_claims_schema_version = 1, extra_claims = '[]'::jsonb
		WHERE authorization_request_id = $1
	`, oidcRequestID, oidcAuthorizationCodeHash); err == nil {
		t.Fatal("OIDC session accepted non-object extra claims")
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_oidc_sessions
		SET authorization_code_hash = $2, subject = 'subject-1', authenticated_at = now() - interval '1 minute', acr = 'aal1',
		    amr = ARRAY['password'], extra_claims_schema_version = 1, extra_claims = '{}'::jsonb
		WHERE authorization_request_id = $1
	`, oidcRequestID, oidcAuthorizationCodeHash); err != nil {
		t.Fatalf("complete OIDC session: %v", err)
	}

	deviceRequestID := "44444444-4444-4444-8444-444444444444"
	if _, err := db.Exec(`
		INSERT INTO system.oauth_device_authorizations
		    (id, device_code_hash, user_code_hash, client_id, requested_scopes, requested_audiences,
		     next_poll_at, requested_at, expires_at)
		VALUES
		    ($1, $2, $3, 'addp-cli', ARRAY['addp.api'], ARRAY['addp.api'],
		     now() + interval '5 seconds', now() - interval '1 second', now() + interval '10 minutes')
	`, deviceRequestID, tokenHash('b'), tokenHash('c')); err != nil {
		t.Fatalf("create device authorization: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_device_authorizations
		SET last_polled_at = now(), next_poll_at = now() + interval '5 seconds'
		WHERE id = $1
	`, deviceRequestID); err != nil {
		t.Fatalf("record allowed device poll: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_device_authorizations
		SET poll_interval_seconds = poll_interval_seconds + 5,
		    last_polled_at = now(),
		    next_poll_at = now() + interval '10 seconds'
		WHERE id = $1
	`, deviceRequestID); err != nil {
		t.Fatalf("record device slow_down: %v", err)
	}
	var pollInterval int
	if err := db.QueryRow(`SELECT poll_interval_seconds FROM system.oauth_device_authorizations WHERE id = $1`, deviceRequestID).Scan(&pollInterval); err != nil {
		t.Fatalf("read device poll interval: %v", err)
	}
	if pollInterval != 10 {
		t.Fatalf("device poll interval = %d, want 10", pollInterval)
	}
	if _, err := db.Exec(`UPDATE system.oauth_device_authorizations SET poll_interval_seconds = 5 WHERE id = $1`, deviceRequestID); err == nil {
		t.Fatal("device authorization poll interval decreased")
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_device_authorizations
		SET status = 'approved',
		    granted_scopes = ARRAY['addp.api'],
		    granted_audiences = ARRAY['addp.api'],
		    principal_id = $2,
		    context_type = 'tenant',
		    tenant_membership_id = $3,
		    issued_authorization_version = $4,
		    authentication_methods = ARRAY['password'],
		    assurance_level = 'aal1',
		    authenticated_at = now() - interval '1 minute',
		    decided_at = now()
		WHERE id = $1
	`, deviceRequestID, tenantUserID, tenantMembershipID, authorizationVersion); err != nil {
		t.Fatalf("approve device authorization: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.oauth_clients SET status = 'disabled' WHERE client_id = 'addp-cli'`); err != nil {
		t.Fatalf("disable OAuth client: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE system.oauth_device_authorizations
		SET status = 'invalidated', invalidated_at = now()
		WHERE id = $1
	`, deviceRequestID); err != nil {
		t.Fatalf("invalidate device authorization: %v", err)
	}
	assertTimestampSet(t, db, `SELECT invalidated_at FROM system.oauth_device_authorizations WHERE id = $1`, deviceRequestID, "device authorization invalidation")
}

func tokenHash(character byte) string {
	return strings.Repeat(string(character), 64)
}

func assertTimestampSet(t *testing.T, db *sql.DB, query string, id any, operation string) {
	t.Helper()
	var timestamp sql.NullTime
	if err := db.QueryRow(query, id).Scan(&timestamp); err != nil {
		t.Fatalf("read timestamp after %s: %v", operation, err)
	}
	if !timestamp.Valid {
		t.Fatalf("timestamp after %s is null", operation)
	}
}

func assertForeignKeyColumnsIndexed(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`
		SELECT c.conrelid::regclass::text, attribute.attname
		FROM pg_constraint c
		JOIN pg_namespace namespace ON namespace.oid = c.connamespace
		JOIN unnest(c.conkey) AS key(attnum) ON true
		JOIN pg_attribute attribute
		  ON attribute.attrelid = c.conrelid AND attribute.attnum = key.attnum
		WHERE c.contype = 'f'
		  AND namespace.nspname = 'system'
		  AND NOT EXISTS (
		      SELECT 1
		      FROM pg_index index_definition
		      WHERE index_definition.indrelid = c.conrelid
		        AND key.attnum = ANY(index_definition.indkey)
		  )
		ORDER BY c.conrelid::regclass::text, attribute.attname
	`)
	if err != nil {
		t.Fatalf("inspect foreign-key indexes: %v", err)
	}
	defer rows.Close()

	var missing []string
	for rows.Next() {
		var tableName, columnName string
		if err := rows.Scan(&tableName, &columnName); err != nil {
			t.Fatalf("scan missing foreign-key index: %v", err)
		}
		missing = append(missing, tableName+"."+columnName)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate missing foreign-key indexes: %v", err)
	}
	if len(missing) > 0 {
		t.Fatalf("foreign-key columns without indexes: %s", strings.Join(missing, ", "))
	}
}

func assertAuthorizationGovernanceConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	var tenantID, tenantUserID, tenantMembershipID int64
	if err := db.QueryRow(`SELECT id FROM system.tenants WHERE code = 'migration-test'`).Scan(&tenantID); err != nil {
		t.Fatalf("find authorization test tenant: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.users WHERE display_name = 'Migration User'`).Scan(&tenantUserID); err != nil {
		t.Fatalf("find authorization test user: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE tenant_id = $1 AND principal_id = $2`, tenantID, tenantUserID).Scan(&tenantMembershipID); err != nil {
		t.Fatalf("find authorization test membership: %v", err)
	}

	var tenantPermissionID, platformPermissionID, departmentPermissionID int64
	if err := db.QueryRow(`
		INSERT INTO system.permissions
		    (permission_key, owner_module, action, risk_level, allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key)
		VALUES
		    ('test.resource.read', 'test', 'read', 'low', ARRAY['tenant'], true, 'permissions.test.resource.read.name', 'permissions.test.resource.read.description')
		RETURNING id
	`).Scan(&tenantPermissionID); err != nil {
		t.Fatalf("create tenant permission: %v", err)
	}
	if err := db.QueryRow(`
		INSERT INTO system.permissions
		    (permission_key, owner_module, action, risk_level, allowed_scope_types, name_i18n_key, description_i18n_key)
		VALUES
		    ('test.platform.read', 'test', 'read', 'low', ARRAY['platform'], 'permissions.test.platform.read.name', 'permissions.test.platform.read.description')
		RETURNING id
	`).Scan(&platformPermissionID); err != nil {
		t.Fatalf("create platform permission: %v", err)
	}
	if err := db.QueryRow(`
		INSERT INTO system.permissions
		    (permission_key, owner_module, action, risk_level, allowed_scope_types, tenant_customizable, name_i18n_key, description_i18n_key)
		VALUES
		    ('test.department.read', 'test', 'read', 'low', ARRAY['department'], true, 'permissions.test.department.read.name', 'permissions.test.department.read.description')
		RETURNING id
	`).Scan(&departmentPermissionID); err != nil {
		t.Fatalf("create department permission: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.permissions SET permission_key = 'test.resource.changed' WHERE id = $1`, tenantPermissionID); err == nil {
		t.Fatal("published permission key update succeeded")
	}
	if _, err := db.Exec(`
		INSERT INTO system.permissions
		    (permission_key, owner_module, action, risk_level, allowed_scope_types, name_i18n_key, description_i18n_key)
		VALUES
		    ('test.duplicate.read', 'test', 'read', 'low', ARRAY['tenant', 'tenant'], 'permissions.test.duplicate.read.name', 'permissions.test.duplicate.read.description')
	`); err == nil {
		t.Fatal("permission accepted duplicate scope types")
	}

	var tenantRoleID, departmentRoleID int64
	if err := db.QueryRow(`
		INSERT INTO system.roles
		    (tenant_id, role_key, name, description, role_type, allowed_scope_types, allowed_principal_types, immutable, created_by_principal_id)
		VALUES
		    ($1, 'tenant.custom_reader', 'Custom Reader', '', 'tenant_custom', ARRAY['tenant'], ARRAY['user'], false, $2)
		RETURNING id
	`, tenantID, tenantUserID).Scan(&tenantRoleID); err != nil {
		t.Fatalf("create tenant custom role: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
		VALUES ($1, $2, 'tenant', $3)
	`, tenantRoleID, tenantPermissionID, tenantUserID); err != nil {
		t.Fatalf("attach tenant-customizable permission: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.role_permissions (role_id, permission_id, source_type, created_by_principal_id)
		VALUES ($1, $2, 'tenant', $3)
	`, tenantRoleID, platformPermissionID, tenantUserID); err == nil {
		t.Fatal("tenant custom role accepted non-customizable platform permission")
	}
	if _, err := db.Exec(`
		INSERT INTO system.roles
		    (role_key, role_type, allowed_scope_types, allowed_principal_types, immutable)
		VALUES ('tenant.missing_i18n', 'tenant_builtin', ARRAY['tenant'], ARRAY['user'], true)
	`); err == nil {
		t.Fatal("built-in role without i18n keys succeeded")
	}

	if err := db.QueryRow(`
		INSERT INTO system.roles
		    (role_key, name_i18n_key, description_i18n_key, role_type, allowed_scope_types, allowed_principal_types, immutable)
		VALUES
		    ('tenant.department_reader', 'roles.tenant.department_reader.name', 'roles.tenant.department_reader.description', 'tenant_builtin', ARRAY['department'], ARRAY['user'], true)
		RETURNING id
	`).Scan(&departmentRoleID); err != nil {
		t.Fatalf("create department built-in role: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.role_permissions (role_id, permission_id, source_type) VALUES ($1, $2, 'product')`, departmentRoleID, departmentPermissionID); err != nil {
		t.Fatalf("attach department permission: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.role_permissions (role_id, permission_id, source_type) VALUES ($1, $2, 'product')`, departmentRoleID, platformPermissionID); err == nil {
		t.Fatal("role permission accepted a scope narrower than the role")
	}

	var tenantAssignmentID int64
	if err := db.QueryRow(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, tenant_id, source_type, created_by_principal_id)
		VALUES ($1, $2, 'tenant', $3, 'manual', $1)
		RETURNING id
	`, tenantUserID, tenantRoleID, tenantID).Scan(&tenantAssignmentID); err != nil {
		t.Fatalf("create tenant role assignment: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, tenant_id, source_type, created_by_principal_id)
		VALUES ($1, $2, 'tenant', $3, 'manual', $1)
	`, tenantUserID, tenantRoleID, tenantID); err == nil {
		t.Fatal("duplicate active role assignment succeeded")
	}
	if _, err := db.Exec(`DELETE FROM system.role_assignments WHERE id = $1`, tenantAssignmentID); err == nil {
		t.Fatal("physical role assignment deletion succeeded")
	}

	var departmentID int64
	if err := db.QueryRow(`INSERT INTO system.departments (tenant_id, code, name) VALUES ($1, 'authorization', 'Authorization') RETURNING id`, tenantID).Scan(&departmentID); err != nil {
		t.Fatalf("create authorization department: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.department_memberships
		    (tenant_id, department_id, tenant_membership_id, membership_type)
		VALUES ($1, $2, $3, 'additional')
	`, tenantID, departmentID, tenantMembershipID); err != nil {
		t.Fatalf("create authorization department membership: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, tenant_id, department_id, source_type, created_by_principal_id)
		VALUES ($1, $2, 'department', $3, $4, 'manual', $1)
	`, tenantUserID, departmentRoleID, tenantID, departmentID); err != nil {
		t.Fatalf("create department role assignment: %v", err)
	}

	targetUserID := createMigrationUser(t, db, "Governed Target")
	requesterUserID := createMigrationUser(t, db, "Governed Requester")
	reviewerUserID := createMigrationUser(t, db, "Governed Reviewer")

	var platformRoleID, conflictingPlatformRoleID int64
	if err := db.QueryRow(`
		INSERT INTO system.roles
		    (role_key, name_i18n_key, description_i18n_key, role_type, allowed_scope_types, allowed_principal_types, immutable)
		VALUES
		    ('platform.test_administrator', 'roles.platform.test_administrator.name', 'roles.platform.test_administrator.description', 'platform_builtin', ARRAY['platform'], ARRAY['user'], true)
		RETURNING id
	`).Scan(&platformRoleID); err != nil {
		t.Fatalf("create platform test role: %v", err)
	}
	if err := db.QueryRow(`
		INSERT INTO system.roles
		    (role_key, name_i18n_key, description_i18n_key, role_type, allowed_scope_types, allowed_principal_types, immutable)
		VALUES
		    ('platform.conflicting_administrator', 'roles.platform.conflicting_administrator.name', 'roles.platform.conflicting_administrator.description', 'platform_builtin', ARRAY['platform'], ARRAY['user'], true)
		RETURNING id
	`).Scan(&conflictingPlatformRoleID); err != nil {
		t.Fatalf("create conflicting platform role: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.role_conflicts (role_id_low, role_id_high, reason) VALUES (LEAST($1::bigint, $2::bigint), GREATEST($1::bigint, $2::bigint), 'separation of duties')`, platformRoleID, conflictingPlatformRoleID); err != nil {
		t.Fatalf("create platform role conflict: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, source_type, created_by_principal_id)
		VALUES ($1, $2, 'platform', 'manual', $3)
	`, targetUserID, platformRoleID, requesterUserID); err == nil {
		t.Fatal("platform role assignment succeeded without an approved grant request")
	}
	if _, err := db.Exec(`
		INSERT INTO system.privileged_change_requests
		    (change_type, target_principal_id, target_role_id, reason, requested_by_principal_id, status, decided_at)
		VALUES ('platform_role_grant', $1, $2, 'forged approval', $3, 'approved', now())
	`, targetUserID, platformRoleID, requesterUserID); err == nil {
		t.Fatal("privileged request insert accepted a forged approved status")
	}

	grantRequestID := createPrivilegedChangeRequest(t, db, "platform_role_grant", targetUserID, platformRoleID, requesterUserID)
	if _, err := db.Exec(`UPDATE system.privileged_change_requests SET status = 'approved', decided_at = now() WHERE id = $1`, grantRequestID); err == nil {
		t.Fatal("privileged request direct approval succeeded without an approval row")
	}
	if _, err := db.Exec(`
		INSERT INTO system.privileged_change_approvals (request_id, reviewer_principal_id, decision)
		VALUES ($1, $2, 'approved')
	`, grantRequestID, requesterUserID); err == nil {
		t.Fatal("privileged request requester approved their own request")
	}
	approvePrivilegedChangeRequest(t, db, grantRequestID, reviewerUserID)

	var platformAssignmentID int64
	if err := db.QueryRow(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, source_type, created_by_principal_id, grant_change_request_id)
		VALUES ($1, $2, 'platform', 'manual', $3, $4)
		RETURNING id
	`, targetUserID, platformRoleID, requesterUserID, grantRequestID).Scan(&platformAssignmentID); err != nil {
		t.Fatalf("apply approved platform role grant: %v", err)
	}
	assertPrivilegedChangeStatus(t, db, grantRequestID, "applied")
	if _, err := db.Exec(`UPDATE system.privileged_change_approvals SET reason = 'changed' WHERE request_id = $1`, grantRequestID); err == nil {
		t.Fatal("privileged approval update succeeded")
	}

	conflictRequestID := createPrivilegedChangeRequest(t, db, "platform_role_grant", targetUserID, conflictingPlatformRoleID, requesterUserID)
	approvePrivilegedChangeRequest(t, db, conflictRequestID, reviewerUserID)
	if _, err := db.Exec(`
		INSERT INTO system.role_assignments
		    (principal_id, role_id, scope_type, source_type, created_by_principal_id, grant_change_request_id)
		VALUES ($1, $2, 'platform', 'manual', $3, $4)
	`, targetUserID, conflictingPlatformRoleID, requesterUserID, conflictRequestID); err == nil {
		t.Fatal("conflicting platform role assignment succeeded")
	}
	assertPrivilegedChangeStatus(t, db, conflictRequestID, "approved")

	if _, err := db.Exec(`UPDATE system.principals SET status = 'suspended' WHERE id = $1`, targetUserID); err == nil {
		t.Fatal("governed platform principal suspension succeeded without approval")
	}
	suspendRequestID := createPrivilegedChangeRequest(t, db, "platform_identity_suspend", targetUserID, 0, requesterUserID)
	approvePrivilegedChangeRequest(t, db, suspendRequestID, reviewerUserID)
	if _, err := db.Exec(`UPDATE system.principals SET status = 'suspended', status_change_request_id = $1 WHERE id = $2`, suspendRequestID, targetUserID); err != nil {
		t.Fatalf("apply approved principal suspension: %v", err)
	}
	assertPrivilegedChangeStatus(t, db, suspendRequestID, "applied")

	revokeRequestID := createPrivilegedChangeRequest(t, db, "platform_role_revoke", targetUserID, platformRoleID, requesterUserID)
	approvePrivilegedChangeRequest(t, db, revokeRequestID, reviewerUserID)
	if _, err := db.Exec(`
		UPDATE system.role_assignments
		SET status = 'revoked',
		    revoked_by_principal_id = $1,
		    revoked_at = now(),
		    revoke_change_request_id = $2
		WHERE id = $3
	`, requesterUserID, revokeRequestID, platformAssignmentID); err != nil {
		t.Fatalf("apply approved platform role revocation: %v", err)
	}
	assertPrivilegedChangeStatus(t, db, revokeRequestID, "applied")
}

func createMigrationUser(t *testing.T, db *sql.DB, displayName string) int64 {
	t.Helper()
	var principalID int64
	if err := db.QueryRow(`INSERT INTO system.principals (principal_type) VALUES ('user') RETURNING id`).Scan(&principalID); err != nil {
		t.Fatalf("create %s principal: %v", displayName, err)
	}
	if _, err := db.Exec(`INSERT INTO system.users (id, display_name) VALUES ($1, $2)`, principalID, displayName); err != nil {
		t.Fatalf("create %s user: %v", displayName, err)
	}
	return principalID
}

func createPrivilegedChangeRequest(t *testing.T, db *sql.DB, changeType string, targetPrincipalID, targetRoleID, requesterPrincipalID int64) int64 {
	t.Helper()
	var requestID int64
	var err error
	if targetRoleID == 0 {
		err = db.QueryRow(`
			INSERT INTO system.privileged_change_requests
			    (change_type, target_principal_id, reason, requested_by_principal_id)
			VALUES ($1, $2, 'migration constraint test', $3)
			RETURNING id
		`, changeType, targetPrincipalID, requesterPrincipalID).Scan(&requestID)
	} else {
		err = db.QueryRow(`
			INSERT INTO system.privileged_change_requests
			    (change_type, target_principal_id, target_role_id, reason, requested_by_principal_id)
			VALUES ($1, $2, $3, 'migration constraint test', $4)
			RETURNING id
		`, changeType, targetPrincipalID, targetRoleID, requesterPrincipalID).Scan(&requestID)
	}
	if err != nil {
		t.Fatalf("create %s request: %v", changeType, err)
	}
	return requestID
}

func approvePrivilegedChangeRequest(t *testing.T, db *sql.DB, requestID, reviewerPrincipalID int64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO system.privileged_change_approvals (request_id, reviewer_principal_id, decision)
		VALUES ($1, $2, 'approved')
	`, requestID, reviewerPrincipalID); err != nil {
		t.Fatalf("approve privileged request %d: %v", requestID, err)
	}
	assertPrivilegedChangeStatus(t, db, requestID, "approved")
}

func assertPrivilegedChangeStatus(t *testing.T, db *sql.DB, requestID int64, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT status FROM system.privileged_change_requests WHERE id = $1`, requestID).Scan(&got); err != nil {
		t.Fatalf("read privileged request %d status: %v", requestID, err)
	}
	if got != want {
		t.Fatalf("privileged request %d status = %q, want %q", requestID, got, want)
	}
}

func assertFederationOrganizationConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	var userPrincipalID, servicePrincipalID, tenantID, serviceTenantMembershipID int64
	if err := db.QueryRow(`SELECT id FROM system.users WHERE display_name = 'Migration User'`).Scan(&userPrincipalID); err != nil {
		t.Fatalf("find migration user: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.service_principals WHERE name = 'test-service'`).Scan(&servicePrincipalID); err != nil {
		t.Fatalf("find migration service principal: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.tenants WHERE code = 'migration-test'`).Scan(&tenantID); err != nil {
		t.Fatalf("find migration tenant: %v", err)
	}
	if err := db.QueryRow(`SELECT id FROM system.tenant_memberships WHERE tenant_id = $1 AND principal_id = $2`, tenantID, servicePrincipalID).Scan(&serviceTenantMembershipID); err != nil {
		t.Fatalf("find service tenant membership: %v", err)
	}

	var userTenantMembershipID int64
	if err := db.QueryRow(`
		INSERT INTO system.tenant_memberships
		    (tenant_id, principal_id, source_type, joined_at, created_by_principal_id)
		VALUES ($1, $2, 'manual', now(), $2)
		RETURNING id
	`, tenantID, userPrincipalID).Scan(&userTenantMembershipID); err != nil {
		t.Fatalf("create user tenant membership: %v", err)
	}

	var identityProviderID, connectionID int64
	if err := db.QueryRow(`
		INSERT INTO system.identity_providers (issuer, protocol, display_name)
		VALUES ('https://idp.example.test', 'oidc', 'Migration IdP')
		RETURNING id
	`).Scan(&identityProviderID); err != nil {
		t.Fatalf("create identity provider: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.identity_providers (issuer, protocol, display_name) VALUES ('https://idp.example.test', 'oidc', 'Duplicate')`); err == nil {
		t.Fatal("duplicate identity provider issuer succeeded")
	}
	if err := db.QueryRow(`
		INSERT INTO system.tenant_idp_connections (tenant_id, identity_provider_id, provisioning_mode)
		VALUES ($1, $2, 'jit')
		RETURNING id
	`, tenantID, identityProviderID).Scan(&connectionID); err != nil {
		t.Fatalf("create tenant IdP connection: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.external_identities (identity_provider_id, subject, user_id)
		VALUES ($1, 'subject-1', $2)
	`, identityProviderID, userPrincipalID); err != nil {
		t.Fatalf("create external identity: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.user_attribute_authorities
		    (user_id, attribute_name, authority_type, tenant_idp_connection_id)
		VALUES ($1, 'display_name', 'identity_provider', $2)
	`, userPrincipalID, connectionID); err != nil {
		t.Fatalf("create external user attribute authority: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.user_attribute_authorities
		    (user_id, attribute_name, authority_type, tenant_idp_connection_id)
		VALUES ($1, 'primary_email', 'local', $2)
	`, userPrincipalID, connectionID); err == nil {
		t.Fatal("local user attribute authority accepted an IdP connection")
	}

	var otherTenantID int64
	if err := db.QueryRow(`INSERT INTO system.tenants (code, name) VALUES ('other-test', 'Other Test') RETURNING id`).Scan(&otherTenantID); err != nil {
		t.Fatalf("create second tenant: %v", err)
	}
	var rootDepartmentID, childDepartmentID int64
	if err := db.QueryRow(`INSERT INTO system.departments (tenant_id, code, name) VALUES ($1, 'root', 'Root') RETURNING id`, tenantID).Scan(&rootDepartmentID); err != nil {
		t.Fatalf("create root department: %v", err)
	}
	if err := db.QueryRow(`INSERT INTO system.departments (tenant_id, parent_id, code, name) VALUES ($1, $2, 'child', 'Child') RETURNING id`, tenantID, rootDepartmentID).Scan(&childDepartmentID); err != nil {
		t.Fatalf("create child department: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.departments SET parent_id = $1 WHERE id = $2`, childDepartmentID, rootDepartmentID); err == nil {
		t.Fatal("department cycle update succeeded")
	}
	if _, err := db.Exec(`INSERT INTO system.departments (tenant_id, parent_id, code, name) VALUES ($1, $2, 'cross-tenant', 'Cross Tenant')`, otherTenantID, rootDepartmentID); err == nil {
		t.Fatal("cross-tenant department parent succeeded")
	}

	if _, err := db.Exec(`
		INSERT INTO system.department_memberships
		    (tenant_id, department_id, tenant_membership_id, membership_type)
		VALUES ($1, $2, $3, 'primary')
	`, tenantID, rootDepartmentID, userTenantMembershipID); err != nil {
		t.Fatalf("create user department membership: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.department_memberships
		    (tenant_id, department_id, tenant_membership_id, membership_type)
		VALUES ($1, $2, $3, 'additional')
	`, tenantID, rootDepartmentID, serviceTenantMembershipID); err == nil {
		t.Fatal("department membership accepted a service principal")
	}
	if _, err := db.Exec(`
		INSERT INTO system.department_memberships
		    (tenant_id, department_id, tenant_membership_id, membership_type)
		VALUES ($1, $2, $3, 'primary')
	`, tenantID, childDepartmentID, userTenantMembershipID); err == nil {
		t.Fatal("second active primary department membership succeeded")
	}
	assertAuthorizationVersion(t, db, userPrincipalID, 3, "department membership creation")
	if _, err := db.Exec(`UPDATE system.departments SET status = 'disabled' WHERE id = $1`, rootDepartmentID); err != nil {
		t.Fatalf("disable department: %v", err)
	}
	assertAuthorizationVersion(t, db, userPrincipalID, 4, "department disable")

	var projectGroupID int64
	if err := db.QueryRow(`
		INSERT INTO system.project_groups (tenant_id, code, name, status)
		VALUES ($1, 'migration-project', 'Migration Project', 'active')
		RETURNING id
	`, tenantID).Scan(&projectGroupID); err != nil {
		t.Fatalf("create project group: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.project_group_memberships
		    (tenant_id, project_group_id, tenant_membership_id, relation_role)
		VALUES ($1, $2, $3, 'coordinator')
	`, tenantID, projectGroupID, serviceTenantMembershipID); err != nil {
		t.Fatalf("create service principal project group membership: %v", err)
	}
	assertAuthorizationVersion(t, db, servicePrincipalID, 3, "project group membership creation")
	if _, err := db.Exec(`UPDATE system.project_groups SET status = 'closed' WHERE id = $1`, projectGroupID); err != nil {
		t.Fatalf("close project group: %v", err)
	}
	assertAuthorizationVersion(t, db, servicePrincipalID, 4, "project group close")
	if _, err := db.Exec(`
		INSERT INTO system.project_group_memberships
		    (tenant_id, project_group_id, tenant_membership_id)
		VALUES ($1, $2, $3)
	`, tenantID, projectGroupID, userTenantMembershipID); err == nil {
		t.Fatal("active membership in a closed project group succeeded")
	}
}

func assertAuthorizationVersion(t *testing.T, db *sql.DB, principalID, want int64, operation string) {
	t.Helper()
	var got int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id = $1`, principalID).Scan(&got); err != nil {
		t.Fatalf("read authorization version after %s: %v", operation, err)
	}
	if got != want {
		t.Fatalf("authorization_version after %s = %d, want %d", operation, got, want)
	}
}

func assertIdentityTenantConstraints(t *testing.T, db *sql.DB) {
	t.Helper()

	var userPrincipalID int64
	if err := db.QueryRow(`INSERT INTO system.principals (principal_type) VALUES ('user') RETURNING id`).Scan(&userPrincipalID); err != nil {
		t.Fatalf("create user principal: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.users (id, display_name) VALUES ($1, 'Migration User')`, userPrincipalID); err != nil {
		t.Fatalf("create user subtype: %v", err)
	}
	if _, err := db.Exec(`UPDATE system.principals SET principal_type = 'service_principal' WHERE id = $1`, userPrincipalID); err == nil {
		t.Fatal("principal type update succeeded after subtype creation")
	}

	var servicePrincipalID int64
	if err := db.QueryRow(`INSERT INTO system.principals (principal_type) VALUES ('service_principal') RETURNING id`).Scan(&servicePrincipalID); err != nil {
		t.Fatalf("create service principal: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO system.users (id, display_name) VALUES ($1, 'Wrong Subtype')`, servicePrincipalID); err == nil {
		t.Fatal("user subtype accepted a service principal")
	}

	var tenantID int64
	if err := db.QueryRow(`INSERT INTO system.tenants (code, name) VALUES ('migration-test', 'Migration Test') RETURNING id`).Scan(&tenantID); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.service_principals (id, name, owner_scope, owner_tenant_id, created_by_principal_id)
		VALUES ($1, 'test-service', 'tenant', $2, $3)
	`, servicePrincipalID, tenantID, userPrincipalID); err == nil {
		t.Fatal("tenant-owned service principal succeeded without tenant membership")
	}
	if _, err := db.Exec(`
		INSERT INTO system.tenant_memberships
		    (tenant_id, principal_id, source_type, joined_at, created_by_principal_id)
		VALUES ($1, $2, 'manual', now(), $3)
	`, tenantID, servicePrincipalID, userPrincipalID); err != nil {
		t.Fatalf("create service principal membership: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO system.service_principals (id, name, owner_scope, owner_tenant_id, created_by_principal_id)
		VALUES ($1, 'test-service', 'tenant', $2, $3)
	`, servicePrincipalID, tenantID, userPrincipalID); err != nil {
		t.Fatalf("create tenant-owned service principal: %v", err)
	}

	var authorizationVersion int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id = $1`, servicePrincipalID).Scan(&authorizationVersion); err != nil {
		t.Fatalf("read authorization version: %v", err)
	}
	if authorizationVersion != 2 {
		t.Fatalf("authorization_version = %d, want 2 after membership creation", authorizationVersion)
	}
	if _, err := db.Exec(`UPDATE system.tenant_memberships SET principal_id = $1 WHERE principal_id = $2`, userPrincipalID, servicePrincipalID); err == nil {
		t.Fatal("tenant membership principal identity update succeeded")
	}
}
