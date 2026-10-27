package main

import (
	"database/sql"
	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func catalogBrowserTestEnvironment() []string {
	return []string{"ADDP_CATALOG_BROWSER_LOCAL=1", "ADDP_CATALOG_BROWSER_RUN_ID=11111111-1111-4111-8111-111111111111", "ADDP_CATALOG_BROWSER_MINUTES=60", "POSTGRES_DB=addp_test", "POSTGRES_HOST=127.0.0.1"}
}
func TestCatalogBrowserAdmission(t *testing.T) {
	base := catalogBrowserTestEnvironment()
	if _, err := catalogBrowserEnvironment(base, catalogBrowserLocalSuite); err != nil {
		t.Fatal(err)
	}
	for _, extra := range []string{"GITHUB_ACTIONS=true", "ADDP_ONLINE_HOSTED=1", "ADDP_ONLINE_OWNER_MANAGED=1", "ADDP_ONLINE_LOCAL_SGM=1", "ADDP_EXECUTION_TRACE_LOCAL=1", "ADDP_ONLINE_HOST=1", "POSTGRES_DB=addp", "POSTGRES_DB=addp_iam_test", "POSTGRES_HOST=host.docker.internal", "ADDP_CATALOG_BROWSER_MINUTES=121", "ADDP_CATALOG_BROWSER_MINUTES=0", "ADDP_CATALOG_BROWSER_RUN_ID=00000000-0000-0000-0000-000000000000"} {
		if _, err := catalogBrowserEnvironment(append(append([]string{}, base...), extra), catalogBrowserLocalSuite); err == nil {
			t.Fatal("accepted ambiguous/foreign input", extra)
		}
	}
	if _, err := catalogBrowserEnvironment(base, "catalog-domain-overview"); err == nil {
		t.Fatal("opened hosted suite")
	}
	if _, err := catalogBrowserEnvironment(nil, catalogBrowserLocalSuite); err == nil {
		t.Fatal("suite alone admitted")
	}
	path := filepath.Join(t.TempDir(), "identities.json")
	if err := os.WriteFile(path, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runCatalogBrowserLocal(base, catalogBrowserLocalSuite, path, ""); err == nil {
		t.Fatal("accepted existing output before connecting")
	}
}
func TestCatalogBrowserReaderPermissions(t *testing.T) {
	want := []string{"catalog.entry.read", "catalog.inventory.read", "standard.domain.read", "standard.element.read"}
	if !reflect.DeepEqual(catalogBrowserMinimum, want) {
		t.Fatal("minimum permissions drift")
	}
	extended := append(append([]string{}, want...), "model.entity.read", "standard.unit.read", "standard.document.read")
	if !reflect.DeepEqual(catalogBrowserExtended(), extended) {
		t.Fatal("extended permissions drift")
	}
	copy := catalogBrowserExtended()
	copy[0] = "write"
	if catalogBrowserMinimum[0] != "catalog.entry.read" {
		t.Fatal("aliased permissions")
	}
}
func TestCatalogBrowserDatabaseIdentity(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct {
		db, cluster string
		accept      bool
	}{{"addp_test", "addp-catalog-browser-" + id, true}, {"addp_test", "", false}, {"addp_test", "foreign", false}, {"addp", "addp-catalog-browser-" + id, false}} {
		driver := "catalog_local_" + uuid.NewString()
		sql.Register(driver, &sqlite3.SQLiteDriver{ConnectHook: func(c *sqlite3.SQLiteConn) error {
			if err := c.RegisterFunc("current_database", func() string { return tc.db }, true); err != nil {
				return err
			}
			return c.RegisterFunc("current_setting", func(string) string { return tc.cluster }, true)
		}})
		db, err := gorm.Open(sqlite.Dialector{DriverName: driver, DSN: ":memory:"}, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := db.DB()
		err = requireCatalogBrowserDatabase(db, id)
		raw.Close()
		if (err == nil) != tc.accept {
			t.Fatalf("database %s cluster %s: %v", tc.db, tc.cluster, err)
		}
	}
}
