package clickhouse

import "testing"

func TestClickHouseIsSystemSchema(t *testing.T) {
	plugin := &ClickHousePlugin{}

	for _, name := range []string{"system", "information_schema", "INFORMATION_SCHEMA"} {
		if !plugin.isSystemSchema(name) {
			t.Fatalf("isSystemSchema(%q) = false, want true", name)
		}
	}

	if plugin.isSystemSchema("analytics") {
		t.Fatal("isSystemSchema(\"analytics\") = true, want false")
	}
}
