package doris

import "testing"

func TestDorisMetadataDialectDoesNotIncludeEngine(t *testing.T) {
	if dorisMetadataDialect.IncludeEngine {
		t.Fatal("Doris must not enable table native engine before information_schema.tables.engine is confirmed stable")
	}
	if got := dorisMetadataDialect.IsSystemSchema("__internal_schema"); !got {
		t.Fatal("Doris should filter __internal_schema as a system database")
	}
	if got := dorisMetadataDialect.IsSystemSchema("analytics"); got {
		t.Fatal("Doris should not filter user database analytics")
	}
}
