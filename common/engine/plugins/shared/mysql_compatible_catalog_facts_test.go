package shared

import "testing"

func TestMySQLCompatibleTableNativeRequiresIncludeEngine(t *testing.T) {
	dialect := MySQLCompatibleCatalogFactsDialect{}
	if got := dialect.tableNative("InnoDB"); got != nil {
		t.Fatalf("tableNative() = %#v, want nil when IncludeEngine is false", got)
	}

	dialect.IncludeEngine = true
	got := dialect.tableNative(" InnoDB ")
	if got["engine"] != "InnoDB" {
		t.Fatalf("tableNative() = %#v, want InnoDB engine", got)
	}
}
