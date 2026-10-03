package migration

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
	"github.com/lib/pq"
)

func TestIAMStatementTimeConstraintsForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable System PostgreSQL gate")
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
	before, through := migrationFilesBeforeAndThrough(t, "000179_iam_statement_time_constraints.up.sql")
	run := func(r *Runner) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := r.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	run(&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot})
	principal, tenant := seedInitializedMigrationTenant(t, db, "statement-clock", "Statement Clock")
	var membership int64
	if err := db.QueryRow("SELECT id FROM system.tenant_memberships WHERE tenant_id=$1 AND principal_id=$2", tenant, principal).Scan(&membership); err != nil {
		t.Fatal(err)
	}
	var initialized time.Time
	if err := db.QueryRow("SELECT initialized_at FROM system.tenants WHERE id=$1", tenant).Scan(&initialized); err != nil {
		t.Fatal(err)
	}
	// Replacing functions must preserve existing trigger targets and data.
	identities := func() string {
		t.Helper()
		var value string
		if err := db.QueryRow(`SELECT string_agg(oid::text, ',' ORDER BY proname) FROM pg_proc WHERE pronamespace='system'::regnamespace AND proname IN ('has_stable_tenant_administrator','oauth_identity_context_is_valid','validate_oauth_authorization_request','validate_oauth_oidc_session','validate_oauth_device_authorization')`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	oldIdentities := identities()
	exerciseStatementTimeConstraints(t, db, principal, tenant, membership, false)
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	run(runner)
	run(runner)
	if identities() != oldIdentities {
		t.Fatal("forward migration changed function identities")
	}
	var retained time.Time
	if err := db.QueryRow("SELECT initialized_at FROM system.tenants WHERE id=$1", tenant).Scan(&retained); err != nil || !retained.Equal(initialized) {
		t.Fatalf("historical initialization changed: %v", err)
	}
	exerciseStatementTimeConstraints(t, db, principal, tenant, membership, true)
}

func exerciseStatementTimeConstraints(t *testing.T, db *sql.DB, principal, tenant, membership int64, migrated bool) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(query, args...); err != nil {
			t.Fatal(err)
		}
	}
	attempt := func(query string, accepted bool, message string, args ...any) {
		t.Helper()
		exec("SAVEPOINT boundary")
		_, err := tx.Exec(query, args...)
		if accepted && err != nil {
			t.Fatalf("current statement fact rejected: %v", err)
		}
		if !accepted {
			var pg *pq.Error
			if !errors.As(err, &pg) || pg.Code != "23514" || !strings.Contains(pg.Message, message) {
				t.Fatalf("expected constraint %q, got %v", message, err)
			}
		}
		exec("ROLLBACK TO SAVEPOINT boundary")
		exec("RELEASE SAVEPOINT boundary")
	}
	exec("SELECT pg_sleep(0.02)")
	// A grant established after transaction start is effective immediately; a future grant is not.
	var newTenant int64
	if err := tx.QueryRow("INSERT INTO system.tenants(code,name) VALUES('late-statement','Late Statement') RETURNING id").Scan(&newTenant); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO system.tenant_memberships(tenant_id,principal_id,status,source_type,joined_at,created_by_principal_id) VALUES($1,$2,'active','bootstrap',statement_timestamp(),$2)`, newTenant, principal)
	exec(`INSERT INTO system.role_assignments(principal_id,role_id,scope_type,tenant_id,status,valid_from,source_type) SELECT $1,id,'tenant',$2,'active',statement_timestamp(),'bootstrap' FROM system.roles WHERE tenant_id IS NULL AND role_key='tenant.administrator'`, principal, newTenant)
	var effective bool
	if err := tx.QueryRow("SELECT system.has_stable_tenant_administrator($1)", newTenant).Scan(&effective); err != nil || effective != migrated {
		t.Fatalf("late stable administrator effective=%v migrated=%v err=%v", effective, migrated, err)
	}
	exec("SAVEPOINT future_grant")
	exec(`INSERT INTO system.role_assignments(principal_id,role_id,scope_type,status,valid_from,source_type) SELECT $1,id,'platform','active',statement_timestamp()+interval '1 minute','bootstrap' FROM system.roles WHERE tenant_id IS NULL AND role_key='platform.system_administrator'`, principal)
	var version int64
	if err := tx.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", principal).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow("SELECT system.oauth_identity_context_is_valid($1,'platform',NULL,$2,'aal2')", principal, version).Scan(&effective); err != nil || effective {
		t.Fatalf("future platform grant accepted=%v err=%v", effective, err)
	}
	exec("ROLLBACK TO SAVEPOINT future_grant")
	exec("RELEASE SAVEPOINT future_grant")
	exec(`INSERT INTO system.role_assignments(principal_id,role_id,scope_type,status,valid_from,source_type) SELECT $1,id,'platform','active',statement_timestamp(),'bootstrap' FROM system.roles WHERE tenant_id IS NULL AND role_key='platform.system_administrator'`, principal)
	if err := tx.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", principal).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow("SELECT system.oauth_identity_context_is_valid($1,'platform',NULL,$2,'aal2')", principal, version).Scan(&effective); err != nil || effective != migrated {
		t.Fatalf("late platform grant effective=%v migrated=%v err=%v", effective, migrated, err)
	}
	// Expiry crossed within a transaction must be evaluated at the current statement.
	exec(`INSERT INTO system.tenant_memberships(tenant_id,principal_id,status,source_type,joined_at,expires_at,created_by_principal_id) VALUES($1,$2,'active','bootstrap',now()-interval '1 second',statement_timestamp()+interval '20 milliseconds',$2)`, tenant, principalForExpiry(t, tx))
	var expPrincipal, expMembership, expVersion int64
	if err := tx.QueryRow(`SELECT m.principal_id,m.id,p.authorization_version FROM system.tenant_memberships m JOIN system.principals p ON p.id=m.principal_id WHERE m.tenant_id=$1 AND m.expires_at IS NOT NULL`, tenant).Scan(&expPrincipal, &expMembership, &expVersion); err != nil {
		t.Fatal(err)
	}
	exec("SELECT pg_sleep(0.03)")
	if err := tx.QueryRow("SELECT system.oauth_identity_context_is_valid($1,'tenant',$2,$3,'aal1')", expPrincipal, expMembership, expVersion).Scan(&effective); err != nil || effective == migrated {
		t.Fatalf("expired membership valid=%v migrated=%v err=%v", effective, migrated, err)
	}
	exec(`INSERT INTO system.oauth_clients(client_id,display_name,client_type,redirect_uris,grant_types,response_types,allowed_scopes,allowed_audiences,token_endpoint_auth_method) VALUES('statement-test','Statement Test','public',ARRAY['http://127.0.0.1/callback'],ARRAY['authorization_code','urn:ietf:params:oauth:grant-type:device_code'],ARRAY['code'],ARRAY['openid','addp.api'],ARRAY['addp.api'],'none')`)
	request := "a1111111-1111-4111-8111-111111111111"
	oidcRequest := "a2222222-2222-4222-8222-222222222222"
	for i, id := range []string{request, oidcRequest} {
		exec(`INSERT INTO system.oauth_authorization_requests(id,request_secret_hash,client_id,redirect_uri,response_types,response_mode,requested_scopes,requested_audiences,requested_at,expires_at) VALUES($1,$2,'statement-test','http://127.0.0.1/callback',ARRAY['code'],'query',ARRAY['openid'],ARRAY['addp.api'],now()-interval '1 second',now()+interval '1 hour')`, id, strings.Repeat(string(rune('a'+i)), 64))
	}
	completion := `UPDATE system.oauth_authorization_requests SET status='cancelled',completed_at=statement_timestamp() WHERE id=$1`
	attempt(completion, migrated, "completion cannot be in the future", request)
	attempt(strings.Replace(completion, "statement_timestamp()", "statement_timestamp()+interval '1 minute'", 1), false, "completion cannot be in the future", request)
	exec(`INSERT INTO system.oauth_oidc_sessions(authorization_request_id,requested_at,expires_at) SELECT id,requested_at,expires_at FROM system.oauth_authorization_requests WHERE id=$1`, oidcRequest)
	exec(`UPDATE system.oauth_authorization_requests SET status='approved',principal_id=$2,context_type='tenant',tenant_membership_id=$3,issued_authorization_version=$4,granted_scopes=ARRAY['openid'],granted_audiences=ARRAY['addp.api'],authentication_methods=ARRAY['password'],assurance_level='aal1',authenticated_at=now()-interval '1 second',completed_at=now() WHERE id=$1`, oidcRequest, principal, membership, version)
	hash := strings.Repeat("c", 64)
	exec(`INSERT INTO system.oauth_authorization_codes(code_hash,authorization_request_id,expires_at) SELECT $2,id,expires_at FROM system.oauth_authorization_requests WHERE id=$1`, oidcRequest, hash)
	oidc := `UPDATE system.oauth_oidc_sessions SET authorization_code_hash=$2,subject='subject',authenticated_at=statement_timestamp(),acr='aal1',amr=ARRAY['password'],extra_claims_schema_version=1,extra_claims='{}'::jsonb WHERE authorization_request_id=$1`
	attempt(oidc, migrated, "OIDC session requires", oidcRequest, hash)
	attempt(strings.Replace(oidc, "statement_timestamp()", "statement_timestamp()+interval '1 minute'", 1), false, "OIDC session requires", oidcRequest, hash)
	device := "a3333333-3333-4333-8333-333333333333"
	exec(`INSERT INTO system.oauth_device_authorizations(id,device_code_hash,user_code_hash,client_id,requested_scopes,requested_audiences,next_poll_at,requested_at,expires_at) VALUES($1,$2,$3,'statement-test',ARRAY['addp.api'],ARRAY['addp.api'],now()+interval '5 seconds',now()-interval '1 second',now()+interval '1 hour')`, device, strings.Repeat("d", 64), strings.Repeat("e", 64))
	decision := `UPDATE system.oauth_device_authorizations SET status='rejected',decided_at=statement_timestamp() WHERE id=$1`
	attempt(decision, migrated, "decision cannot be in the future", device)
	attempt(strings.Replace(decision, "statement_timestamp()", "statement_timestamp()+interval '1 minute'", 1), false, "decision cannot be in the future", device)
	if migrated {
		exec(completion, request)
		attempt(`UPDATE system.oauth_authorization_requests SET status='pending',completed_at=NULL WHERE id=$1`, false, "terminal state are immutable", request)
		exec(oidc, oidcRequest, hash)
		attempt(`UPDATE system.oauth_oidc_sessions SET subject='changed' WHERE authorization_request_id=$1`, false, "immutable", oidcRequest)
		exec(decision, device)
		attempt(`UPDATE system.oauth_device_authorizations SET status='pending',decided_at=NULL WHERE id=$1`, false, "immutable", device)
	}
}

func principalForExpiry(t *testing.T, tx *sql.Tx) int64 {
	t.Helper()
	var id int64
	if err := tx.QueryRow("INSERT INTO system.principals(principal_type) VALUES('user') RETURNING id").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("INSERT INTO system.users(id,display_name) VALUES($1,'Expiry Boundary')", id); err != nil {
		t.Fatal(err)
	}
	return id
}
