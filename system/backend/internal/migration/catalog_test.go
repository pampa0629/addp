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
	if catalog.LatestVersion != 34 {
		t.Fatalf("LatestVersion = %d, want 34", catalog.LatestVersion)
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
