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
