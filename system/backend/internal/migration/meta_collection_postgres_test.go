package migration

import (
	"database/sql"
	"testing"
)

func TestMetaCollectionQualificationRuntimeForwardMigrationAgainstPostgres(t *testing.T) {
	// Exercises published-before -> 234 -> repeated startup, existing machine
	// authority invalidation/token-family revocation, and no new assignments.
	t.Run("meta consumer", func(t *testing.T) {
		testRuntimePermissionMigration(t, "addp-meta", "tenant.meta_runtime", "000234_meta_collection_qualifications.up.sql", 234, 4, "system.meta_collection_qualification.execute", []string{"tenant.meta_runtime"})
	})
	t.Run("system facts reader", func(t *testing.T) {
		testRuntimePermissionMigration(t, "addp-system", "tenant.system_runtime", "000234_meta_collection_qualifications.up.sql", 234, 4, "meta.task_provider.read", []string{"tenant.orchestrator_runtime", "tenant.system_runtime"})
	})
}

func TestMetaCollectionQualificationApprovalForwardMigrationAgainstPostgres(t *testing.T) {
	testAuthorizationFactsMigration(t, "000234_meta_collection_qualifications.up.sql", 234, 4, func(db *sql.DB) func() {
		user, tenant := seedInitializedMigrationTenant(t, db, "collection-approval", "Collection approval")
		if _, err := db.Exec(`INSERT INTO system.role_assignments
			(principal_id,role_id,scope_type,tenant_id,status,valid_from,source_type,grant_reason,created_by_principal_id)
			SELECT $1,id,'tenant',$2,'active',now(),'manual','disposable collection migration fixture',$1 FROM system.roles
			WHERE tenant_id IS NULL AND role_key='tenant.engine_access_delegation_administrator'`, user, tenant); err != nil {
			t.Fatal(err)
		}
		var before, assignments, grants, denies int64
		if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", user).Scan(&before); err != nil {
			t.Fatal(err)
		}
		for query, target := range map[string]*int64{
			"SELECT count(*) FROM system.role_assignments":     &assignments,
			"SELECT count(*) FROM system.engine_access_grants": &grants,
			"SELECT count(*) FROM system.engine_access_denies": &denies,
		} {
			if err := db.QueryRow(query).Scan(target); err != nil {
				t.Fatal(err)
			}
		}
		return func() {
			var current int64
			if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", user).Scan(&current); err != nil || current != before+3 {
				t.Fatal("assigned approver authority did not advance exactly once per new Permission", current, before, err)
			}
			for query, want := range map[string]int64{
				"SELECT count(*) FROM system.role_assignments":               assignments,
				"SELECT count(*) FROM system.engine_access_grants":           grants,
				"SELECT count(*) FROM system.engine_access_denies":           denies,
				"SELECT count(*) FROM system.meta_collection_qualifications": 0,
				`SELECT count(*) FROM system.role_permissions b JOIN system.roles r ON r.id=b.role_id JOIN system.permissions p ON p.id=b.permission_id WHERE r.role_key='tenant.engine_access_delegation_administrator' AND p.permission_key LIKE 'system.meta_collection_qualification.%' AND p.action IN('approve','read','revoke') AND p.tenant_customizable AND NOT p.delegable AND p.allowed_scope_types=ARRAY['tenant']::text[]`: 3,
				`SELECT count(*) FROM system.role_permissions b JOIN system.permissions p ON p.id=b.permission_id WHERE p.permission_key LIKE 'system.meta_collection_qualification.%'`:                                                                                                                                                                                                                                                 4,
			} {
				if err := db.QueryRow(query).Scan(&current); err != nil || current != want {
					t.Fatalf("publication changed unrelated authority/created qualification: count=%d want=%d err=%v", current, want, err)
				}
			}
			assertForeignKeyColumnsIndexed(t, db)
		}
	})
}

func TestMetaCollectionRetirementForwardMigrationAgainstPostgres(t *testing.T) {
	testAuthorizationFactsMigration(t, "000259_retire_meta_collection_approval.up.sql", 259, 1, func(db *sql.DB) func() {
		user, tenant := seedInitializedMigrationTenant(t, db, "collection-retirement", "Collection retirement")
		if _, err := db.Exec(`INSERT INTO system.role_assignments (principal_id,role_id,scope_type,tenant_id,status,valid_from,source_type,grant_reason,created_by_principal_id)
  SELECT $1,id,'tenant',$2,'active',now(),'manual','retirement fixture',$1 FROM system.roles WHERE tenant_id IS NULL AND role_key='tenant.engine_access_delegation_administrator'`, user, tenant); err != nil {
			t.Fatal(err)
		}
		var version, assignments, grants, denies int64
		for query, target := range map[string]*int64{"SELECT count(*) FROM system.role_assignments": &assignments, "SELECT count(*) FROM system.engine_access_grants": &grants, "SELECT count(*) FROM system.engine_access_denies": &denies} {
			if err := db.QueryRow(query).Scan(target); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", user).Scan(&version); err != nil {
			t.Fatal(err)
		}
		return func() {
			var n int64
			if err := db.QueryRow("SELECT authorization_version FROM system.principals WHERE id=$1", user).Scan(&n); err != nil || n <= version {
				t.Fatal("old approval authority not invalidated", n, version, err)
			}
			for query, want := range map[string]int64{
				"SELECT count(*) FROM system.role_assignments":     assignments,
				"SELECT count(*) FROM system.engine_access_grants": grants,
				"SELECT count(*) FROM system.engine_access_denies": denies,
				"SELECT count(*) FROM system.permissions WHERE permission_key LIKE 'system.meta_collection_qualification.%' AND status='active'":                                                                                                                                                                                   0,
				"SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key LIKE 'system.meta_collection_qualification.%'":                                                                                                                                          0,
				"SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='meta.scan_task.cancel'":                                                                                                                                                                0,
				"SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id JOIN system.roles r ON r.id=rp.role_id WHERE p.permission_key='system.meta_collection.execute' AND r.role_key='tenant.meta_runtime' AND r.tenant_id IS NULL AND NOT p.delegable AND NOT p.tenant_customizable": 1,
				"SELECT count(*) FROM system.role_permissions rp JOIN system.permissions p ON p.id=rp.permission_id WHERE p.permission_key='system.meta_collection.execute'":                                                                                                                                                       1,
				"SELECT count(*) FROM pg_tables WHERE schemaname='system' AND tablename='meta_collection_qualifications'":                                                                                                                                                                                                          0,
			} {
				if err := db.QueryRow(query).Scan(&n); err != nil || n != want {
					t.Fatal(query, n, want, err)
				}
			}
			assertForeignKeyColumnsIndexed(t, db)
		}
	})
}
