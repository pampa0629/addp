package migration

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestReadCatalogRequiresContinuousOrderedMigrations(t *testing.T) {
	catalog, err := ReadCatalog(fstest.MapFS{
		"sql/000002_second.up.sql": {Data: []byte("SELECT 2")},
		"sql/000001_first.up.sql":  {Data: []byte("SELECT 1")},
	}, "sql")
	if err != nil {
		t.Fatalf("ReadCatalog: %v", err)
	}
	if catalog.LatestVersion != 2 || catalog.Files[0].Name != "000001_first.up.sql" || catalog.Files[1].Name != "000002_second.up.sql" {
		t.Fatalf("catalog = %#v", catalog)
	}
	if catalog.Files[0].SHA256 == "" || catalog.Files[0].Contents != "SELECT 1" {
		t.Fatalf("first migration = %#v", catalog.Files[0])
	}
}

func TestReadCatalogRejectsGapAndUnknownFile(t *testing.T) {
	for name, source := range map[string]fstest.MapFS{
		"gap": {
			"sql/000001_first.up.sql": {Data: []byte("SELECT 1")},
			"sql/000003_third.up.sql": {Data: []byte("SELECT 3")},
		},
		"unknown": {
			"sql/000001_first.up.sql": {Data: []byte("SELECT 1")},
			"sql/readme.md":           {Data: []byte("not a migration")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadCatalog(source, "sql"); err == nil {
				t.Fatal("ReadCatalog unexpectedly succeeded")
			}
		})
	}
}

func TestVerifyAppliedMigrationsRejectsChecksumMismatch(t *testing.T) {
	catalog, err := ReadCatalog(fstest.MapFS{
		"sql/000001_first.up.sql": {Data: []byte("SELECT 1")},
	}, "sql")
	if err != nil {
		t.Fatalf("ReadCatalog: %v", err)
	}
	err = verifyAppliedMigrations(catalog, []appliedMigration{{Version: 1, Filename: "000001_first.up.sql", SHA256: strings.Repeat("0", 64)}})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("verifyAppliedMigrations error = %v", err)
	}
}
