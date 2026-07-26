package migration

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestEmbeddedMigrationCatalog(t *testing.T) {
	catalog, err := ReadCatalog(EmbeddedSQL, DefaultMigrationsRoot)
	if err != nil {
		t.Fatalf("ReadCatalog() error = %v", err)
	}
	if catalog.LatestVersion != 17 {
		t.Fatalf("LatestVersion = %d, want 17", catalog.LatestVersion)
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
