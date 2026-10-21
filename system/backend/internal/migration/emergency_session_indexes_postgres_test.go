package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/addp/system/internal/testsupport"
	"github.com/google/uuid"
)

func TestEmergencySessionForeignKeyIndexesForwardMigrationAgainstPostgres(t *testing.T) {
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
	before, through := migrationFilesBeforeAndThrough(t, "000232_emergency_session_foreign_key_indexes.up.sql")
	run := func(files *Runner) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := files.Run(ctx); err != nil {
			t.Fatal(err)
		}
	}
	run(&Runner{DSN: dsn, FS: before, Root: DefaultMigrationsRoot})
	wantMissing := []string{
		"system.emergency_session_approvals.principal_id",
		"system.emergency_session_requests.closed_by_principal_id",
		"system.emergency_session_requests.executor_membership_id",
		"system.emergency_session_requests.executor_principal_id",
		"system.emergency_session_requests.target_family_id",
		"system.emergency_session_requests.target_membership_id",
		"system.emergency_session_requests.target_principal_id",
	}
	if got := missingEmergencyLeadingForeignKeyIndexes(t, db); !reflect.DeepEqual(got, wantMissing) {
		t.Fatalf("published 231 leading-index gap = %v, want %v", got, wantMissing)
	}
	seedEmergencyIndexMigrationHistory(t, db)
	facts := emergencyMigrationFactDigests(t, db)
	var oldChecksum string
	if err := db.QueryRow("SELECT sha256 FROM system.schema_migration_checksums WHERE version=231").Scan(&oldChecksum); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{DSN: dsn, FS: through, Root: DefaultMigrationsRoot}
	run(runner)
	if got := missingEmergencyLeadingForeignKeyIndexes(t, db); len(got) != 0 {
		t.Fatalf("emergency foreign keys lack valid leading B-tree indexes: %v", got)
	}
	// Keep the original, complete schema-wide gate in addition to the stricter
	// check of these two tables; do not exempt the failing published schema.
	assertForeignKeyColumnsIndexed(t, db)
	if !reflect.DeepEqual(facts, emergencyMigrationFactDigests(t, db)) {
		t.Fatal("index upgrade changed emergency history, receipts, audit or authorization facts")
	}
	var version, checksumCount int
	var dirty bool
	var retainedChecksum string
	if err := db.QueryRow("SELECT version,dirty FROM system.schema_migrations").Scan(&version, &dirty); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT sha256 FROM system.schema_migration_checksums WHERE version=231").Scan(&retainedChecksum); err != nil {
		t.Fatal(err)
	}
	if version != 232 || dirty || retainedChecksum != oldChecksum {
		t.Fatal("forward migration did not retain the applied 231 checksum or clean version")
	}
	if err := db.QueryRow("SELECT count(*) FROM system.schema_migration_checksums WHERE version=232").Scan(&checksumCount); err != nil || checksumCount != 1 {
		t.Fatalf("232 checksum records=%d error=%v", checksumCount, err)
	}
	indexIDs := emergencyIndexIdentities(t, db)
	run(runner)
	if indexIDs != emergencyIndexIdentities(t, db) || !reflect.DeepEqual(facts, emergencyMigrationFactDigests(t, db)) {
		t.Fatal("repeated Runner execution recreated indexes or changed persisted facts")
	}
	t.Log("published 231 upgraded to 232 with all emergency foreign keys leading-indexed; completed receipts, cancellations, approvals, audit and authorization facts are unchanged; repeated Runner execution is a no-op")
}

func missingEmergencyLeadingForeignKeyIndexes(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`
		SELECT c.conrelid::regclass::text || '.' || a.attname
		FROM pg_constraint c
		JOIN unnest(c.conkey) key(attnum) ON true
		JOIN pg_attribute a ON a.attrelid=c.conrelid AND a.attnum=key.attnum
		WHERE c.contype='f' AND c.conrelid IN
		 ('system.emergency_session_requests'::regclass,'system.emergency_session_approvals'::regclass)
		AND NOT EXISTS (
		 SELECT 1 FROM pg_index i JOIN pg_class idx ON idx.oid=i.indexrelid
		 JOIN pg_am am ON am.oid=idx.relam
		 WHERE i.indrelid=c.conrelid AND i.indisvalid AND i.indisready
		 AND i.indpred IS NULL AND i.indexprs IS NULL AND am.amname='btree'
		 AND i.indnkeyatts>=1 AND i.indkey[0]=key.attnum
		) ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		result = append(result, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func seedEmergencyIndexMigrationHistory(t *testing.T, db *sql.DB) {
	t.Helper()
	executor, tenant := seedInitializedMigrationTenant(t, db, "emergency-index-upgrade", "Emergency index upgrade")
	target := createMigrationUser(t, db, "Migration emergency target")
	systemReviewer := createMigrationUser(t, db, "Migration system reviewer")
	securityReviewer := createMigrationUser(t, db, "Migration security reviewer")
	var executorMembership, targetMembership, family int64
	if err := db.QueryRow("SELECT id FROM system.tenant_memberships WHERE tenant_id=$1 AND principal_id=$2", tenant, executor).Scan(&executorMembership); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO system.tenant_memberships(tenant_id,principal_id,status,source_type,joined_at,created_by_principal_id)
	 VALUES($1,$2,'active','manual',statement_timestamp(),$3) RETURNING id`, tenant, target, executor).Scan(&targetMembership); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.role_assignments(principal_id,role_id,scope_type,status,valid_from,source_type)
	 SELECT CASE role_key WHEN 'platform.system_administrator' THEN $1::bigint ELSE $2::bigint END,id,'platform','active',statement_timestamp(),'bootstrap'
	 FROM system.roles WHERE tenant_id IS NULL AND role_key IN ('platform.system_administrator','platform.security_administrator')`, systemReviewer, securityReviewer); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`INSERT INTO system.refresh_token_families
	 (principal_id,context_type,tenant_membership_id,issued_authorization_version,client_id,auth_type,audiences,scopes,authentication_methods,assurance_level,authenticated_at,expires_at)
	 SELECT id,'tenant',$2,authorization_version,'addp-web','first_party',ARRAY['addp.api']::text[],ARRAY[]::text[],ARRAY['password']::text[],'aal1',statement_timestamp()-interval '1 minute',statement_timestamp()+interval '1 hour'
	 FROM system.principals WHERE id=$1 RETURNING id`, target, targetMembership).Scan(&family); err != nil {
		t.Fatal(err)
	}
	completed, cancelled := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{completed, cancelled} {
		var requested time.Time
		if err := db.QueryRow("SELECT clock_timestamp()").Scan(&requested); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO system.emergency_session_requests
		 (id,tenant_id,executor_principal_id,executor_membership_id,executor_authorization_version,target_principal_id,target_membership_id,target_authorization_version,target_family_id,reason,incident_reference,requested_at,approval_expires_at)
		 SELECT $1,$2,$3,$4,e.authorization_version,$5,$6,p.authorization_version,$7,'retained migration history','INDEX-REGRESSION',$8::timestamptz,$8::timestamptz+interval '10 minutes'
		 FROM system.principals e CROSS JOIN system.principals p WHERE e.id=$3 AND p.id=$5`, id, tenant, executor, executorMembership, target, targetMembership, family, requested); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO system.emergency_session_approvals(request_id,role_key,principal_id,authorization_version,reason,approved_at)
	 SELECT r.id,review.role_key,p.id,p.authorization_version,'retained independent review',clock_timestamp()
	 FROM system.emergency_session_requests r
	 CROSS JOIN (VALUES ('platform.system_administrator',$2::bigint),('platform.security_administrator',$3::bigint)) review(role_key,principal_id)
	 JOIN system.principals p ON p.id=review.principal_id WHERE r.id=$1`, completed, systemReviewer, securityReviewer); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE system.emergency_session_requests SET status='approved',version=2,
	 approved_at=(SELECT max(approved_at) FROM system.emergency_session_approvals WHERE request_id=$1),
	 execute_before=(SELECT max(approved_at)+interval '15 minutes' FROM system.emergency_session_approvals WHERE request_id=$1) WHERE id=$1`, completed); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE system.refresh_token_families SET revoked_at=statement_timestamp(),revoked_reason='emergency_session_revocation' WHERE id=$1`, family); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE system.emergency_session_requests SET status='executed',version=3,execution_id=$2,executed_at=clock_timestamp(),outcome='revoked' WHERE id=$1`, completed, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE system.emergency_session_requests SET status='cancelled',version=2,closed_by_principal_id=$2,close_reason='retained cancellation' WHERE id=$1`, cancelled, executor); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO system.audit_logs(principal_id,principal_type,context_type,tenant_id,event_name,result,risk_level,module_name,entity_type,entity_id,details)
	 VALUES($1,'user','tenant',$2,'iam.emergency_session_revocation.execute','succeeded','high','system','emergency_session_request',$3,jsonb_build_object('outcome','revoked'))`, executor, tenant, completed.String()); err != nil {
		t.Fatal(err)
	}
}

func emergencyMigrationFactDigests(t *testing.T, db *sql.DB) map[string][32]byte {
	t.Helper()
	result := map[string][32]byte{}
	for _, table := range []string{"emergency_session_requests", "emergency_session_approvals", "principals", "users", "tenants", "tenant_memberships", "roles", "role_permissions", "role_assignments", "permissions", "refresh_token_families", "audit_logs"} {
		var snapshot string
		// The table set is closed and owner-local. Never log its row snapshots.
		if err := db.QueryRow(fmt.Sprintf(`SELECT COALESCE(jsonb_agg(row_data ORDER BY row_data::text),'[]'::jsonb)::text FROM (SELECT to_jsonb(item) row_data FROM system.%s item) facts`, table)).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		result[table] = sha256.Sum256([]byte(snapshot))
	}
	return result
}

func emergencyIndexIdentities(t *testing.T, db *sql.DB) string {
	t.Helper()
	var result string
	if err := db.QueryRow(`SELECT string_agg(indexrelid::text,',' ORDER BY indexrelid) FROM pg_index
	 WHERE indrelid IN ('system.emergency_session_requests'::regclass,'system.emergency_session_approvals'::regclass)`).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
