package migration

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

func TestEmbeddedMigrationCatalog(t *testing.T) {
	catalog, err := ReadCatalog(EmbeddedSQL, DefaultMigrationsRoot)
	if err != nil {
		t.Fatalf("ReadCatalog() error = %v", err)
	}
	if catalog.LatestVersion != 41 {
		t.Fatalf("LatestVersion = %d, want 41", catalog.LatestVersion)
	}
}

func TestIAMSecurityPolicyMigrationPublishesPolicyAndAuthorization(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000041_iam_security_policy.up.sql")
	if err != nil {
		t.Fatalf("read migration 41: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"CREATE TABLE system.iam_security_policy",
		"CREATE INDEX idx_iam_security_policy_updated_by_principal_id",
		"applied_version bigint NOT NULL",
		"'iam.security_policy.read'",
		"'iam.security_policy.update'",
		"role.role_key = 'platform.security_administrator'",
		"INSERT INTO system.role_permissions",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 41 missing %q", fragment)
		}
	}
}

func TestManagerPlatformRuntimeMigrationPublishesRoleAndAssignment(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000040_iam_manager_platform_runtime.up.sql")
	if err != nil {
		t.Fatalf("read migration 40: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'platform.manager_runtime'",
		"ARRAY['platform']::text[]",
		"ARRAY['service_principal']::text[]",
		"permission.permission_key = 'system.runtime_registry.update'",
		"service_principal.name = 'addp-manager'",
		"INSERT INTO system.role_assignments",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 40 missing %q", fragment)
		}
	}
}

func TestManagerPlatformConfigurationAuthorizationMigrationPublishesPermissionAndRoleBinding(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000039_manager_platform_configuration_authorization.up.sql")
	if err != nil {
		t.Fatalf("read migration 39: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"'manager.configuration.read', 'manager', 'read'",
		"'manager.configuration.update', 'manager', 'update'",
		"ARRAY['platform']::text[]",
		"role.role_key = 'platform.system_administrator'",
		"INSERT INTO system.role_permissions",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 39 missing %q", fragment)
		}
	}
}

func TestNotebookSessionAuthorizationRepairMigrationPublishesCanonicalState(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000038_iam_notebook_session_authorization_repair.up.sql")
	if err != nil {
		t.Fatalf("read migration 38: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"to_regclass('system.notebook_catalog_authorizations')",
		"RENAME TO notebook_session_authorizations",
		"CREATE TABLE system.notebook_session_authorizations",
		"source_notebook_session_authorization_id",
		"system.schema_migration_checksums",
		"legacy notebook catalog authorization schema still exists",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 38 missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"DROP TABLE IF EXISTS system.notebook_catalog_authorizations",
		"DELETE FROM system.permissions",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("migration 38 must preserve IAM history, found %q", forbidden)
		}
	}
}

func TestNotebookSessionAuthorizationMigrationPublishesBoundary(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000037_iam_notebook_session_authorization.up.sql")
	if err != nil {
		t.Fatalf("read migration 37: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"CREATE TABLE system.notebook_session_authorizations",
		"operations text[] NOT NULL CHECK",
		"ARRAY['catalog.list_children', 'execution_engine_access.derive']::text[]",
		"token_family_id bigint NOT NULL REFERENCES system.refresh_token_families(id)",
		"source_notebook_session_authorization_id uuid",
		"trg_notebook_session_authorizations_revoke_executions",
		"'system.notebook_session_authorization.execute'",
		"role.role_key = 'tenant.develop_runtime'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 37 missing %q", fragment)
		}
	}
}

func TestServiceQuerySampleMigrationPublishesAuthorizationBoundary(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000036_iam_service_query_sample.up.sql")
	if err != nil {
		t.Fatalf("read migration 36: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"audience IN ('develop', 'duckdb', 'service')",
		"'service.data_read.execute'",
		"'system.execution_authorization.create'",
		"role.role_key = 'tenant.service_publisher'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 36 missing %q", fragment)
		}
	}
}

func TestDuckDBRuntimeMigrationPublishesAuthorizationBoundary(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000035_iam_duckdb_runtime.up.sql")
	if err != nil {
		t.Fatalf("read migration 35: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"execution_authorizations_source_check",
		"source_type = 'service_definition'",
		"'tenant.duckdb_runtime'",
		"'addp-duckdb'",
		"'meta.catalog.read'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 35 missing %q", fragment)
		}
	}
}

func TestDevelopNotebookUpdateMigrationPublishesPermissionAndRoleBinding(t *testing.T) {
	data, err := fs.ReadFile(EmbeddedSQL, "sql/000034_iam_develop_notebook_update.up.sql")
	if err != nil {
		t.Fatalf("read migration 34: %v", err)
	}
	sql := string(data)
	for _, fragment := range []string{
		"permission_key = 'develop.notebook.update'",
		"SET status = 'active'",
		"role.role_key = 'tenant.data_engineer'",
		"INSERT INTO system.role_permissions",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration 34 missing %q", fragment)
		}
	}
}

func TestReadCatalogRejectsVersionGap(t *testing.T) {
	_, err := ReadCatalog(fstest.MapFS{
		"sql/000001_first.up.sql": {},
		"sql/000003_third.up.sql": {},
	}, "sql")
	if err == nil || !strings.Contains(err.Error(), "expected 000002") {
		t.Fatalf("ReadCatalog() error = %v, want version gap", err)
	}
}

func TestReadCatalogRejectsNonMigrationFile(t *testing.T) {
	_, err := ReadCatalog(fstest.MapFS{"sql/README.md": {}}, "sql")
	if err == nil || !strings.Contains(err.Error(), "invalid migration filename") {
		t.Fatalf("ReadCatalog() error = %v, want invalid filename", err)
	}
}
