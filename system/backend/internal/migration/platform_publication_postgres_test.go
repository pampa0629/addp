package migration

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
)

func TestPlatformOntologyPublicationForwardMigrationAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("requires System PostgreSQL gate")
	}
	testsupport.RequireDisposablePostgresDSN(t, dsn)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reset := func() {
		t.Helper()
		if _, err := db.Exec(`DROP SCHEMA IF EXISTS system CASCADE; DROP SCHEMA IF EXISTS common CASCADE`); err != nil {
			t.Fatal(err)
		}
	}
	reset()
	defer reset()
	before, through := migrationFilesBeforeAndThrough(t, "000201_platform_ontology_publication.up.sql")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := (&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	var principal, priorVersion, family int64
	if err := db.QueryRow(`SELECT p.id,p.authorization_version FROM system.principals p JOIN system.service_principals s ON s.id=p.id WHERE s.name='addp-ontology' AND s.owner_scope='platform'`).Scan(&principal, &priorVersion); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO system.refresh_token_families(protocol_request_id,principal_id,context_type,issued_authorization_version,client_id,auth_type,audiences,scopes,authentication_methods,assurance_level,authenticated_at,expires_at,created_at,updated_at)
	    VALUES('20100000-0000-4000-8000-000000000001',$1,'platform',$2,'addp-ontology','oauth',ARRAY['addp.api'],ARRAY['addp.api'],ARRAY['service_secret'],'not_applicable',statement_timestamp(),statement_timestamp()+interval '5 minutes',statement_timestamp(),statement_timestamp()) RETURNING id`, principal, priorVersion).Scan(&family); err != nil {
		t.Fatal(err)
	}
	// Everything outside the affected machine identity remains byte-for-byte
	// unchanged, including Tenant runtime permissions and user roles.
	snapshot := func() string {
		t.Helper()
		var value string
		if err := db.QueryRow(`SELECT jsonb_build_object(
	    'roles',(SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM system.roles r),
	    'bindings',(SELECT jsonb_agg(to_jsonb(b) ORDER BY b.role_id,b.permission_id) FROM system.role_permissions b JOIN system.roles r ON r.id=b.role_id WHERE r.role_key<>'platform.ontology_runtime'),
	    'principals',(SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM system.principals p WHERE p.id<>$1),
	    'assignments',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM system.role_assignments a),
	    'clients',(SELECT jsonb_agg(to_jsonb(c) ORDER BY c.client_id) FROM system.oauth_clients c)
	)::text`, principal).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	original := snapshot()
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if snapshot() != original {
		t.Fatal("migration changed unrelated identity, assignments, clients or Tenant/user permissions")
	}
	var role, scope, owner, action, risk string
	var customizable, delegable bool
	if err := db.QueryRow(`SELECT r.role_key,array_to_string(p.allowed_scope_types,','),p.owner_module,p.action,p.risk_level,p.tenant_customizable,p.delegable FROM system.permissions p JOIN system.role_permissions b ON b.permission_id=p.id JOIN system.roles r ON r.id=b.role_id WHERE p.permission_key='ontology.platform_definition.publish'`).Scan(&role, &scope, &owner, &action, &risk, &customizable, &delegable); err != nil {
		t.Fatal(err)
	}
	if role != "platform.ontology_runtime" || scope != "platform" || owner != "ontology" || action != "publish" || risk != "high" || customizable || delegable {
		t.Fatalf("incorrect platform permission %s %s %s %s %s %t %t", role, scope, owner, action, risk, customizable, delegable)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions b JOIN system.permissions p ON p.id=b.permission_id WHERE p.permission_key='ontology.platform_definition.publish'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("granted roles=%d %v", count, err)
	}
	var version int64
	var reason string
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id=$1`, principal).Scan(&version); err != nil || version <= priorVersion {
		t.Fatalf("old token version unchanged %d %v", version, err)
	}
	if err := db.QueryRow(`SELECT revoked_reason FROM system.refresh_token_families WHERE id=$1 AND revoked_at IS NOT NULL`, family).Scan(&reason); err != nil || reason != "authorization_catalog_changed" {
		t.Fatalf("old family remains usable %q %v", reason, err)
	}
	if err := runner.Run(ctx); err != nil {
		t.Fatal(err)
	}
	var repeated int64
	if err := db.QueryRow(`SELECT authorization_version FROM system.principals WHERE id=$1`, principal).Scan(&repeated); err != nil || repeated != version {
		t.Fatal("repeat startup changed machine authorization")
	}
}
