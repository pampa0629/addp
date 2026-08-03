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
	if catalog.LatestVersion != 36 {
		t.Fatalf("LatestVersion = %d, want 36", catalog.LatestVersion)
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
