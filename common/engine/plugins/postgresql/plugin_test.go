package postgresql

import "testing"

func TestPostgreSQLIsSystemSchema(t *testing.T) {
	plugin := &PostgreSQLPlugin{}

	for _, name := range []string{"pg_catalog", "information_schema", "pg_toast", "PG_TEMP_12", "pg_toast_temp_7"} {
		if !plugin.isSystemSchema(name) {
			t.Fatalf("isSystemSchema(%q) = false, want true", name)
		}
	}

	if plugin.isSystemSchema("public") {
		t.Fatal("isSystemSchema(\"public\") = true, want false")
	}
}

func TestPostgresTableNativeKeepsSourceFactsOnly(t *testing.T) {
	native := postgresTableNative(" BASE TABLE ", " r ")

	if native["table_type"] != "BASE TABLE" || native["relkind"] != "r" {
		t.Fatalf("postgresTableNative() = %#v, want table_type and relkind", native)
	}
	if native["kind"] != nil {
		t.Fatalf("postgresTableNative() should not include platform kind: %#v", native)
	}
}

func TestPostgresTableNativeReturnsNilForEmptyFacts(t *testing.T) {
	if got := postgresTableNative("", " "); got != nil {
		t.Fatalf("postgresTableNative() = %#v, want nil", got)
	}
}
