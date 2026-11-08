package migration

import (
	"database/sql"
	"testing"
)

func TestDevelopFileReadPermissionDefinitionAgainstPostgres(t *testing.T) {
	testAuthorizationFactsMigration(t, "000274_develop_file_execution_read_definition.up.sql", 274, 1, func(db *sql.DB) func() {
		seedInitializedMigrationTenant(t, db, "file-definition-only", "File Definition Only")
		tables := []string{"roles", "role_permissions", "role_assignments", "principals", "refresh_token_families", "engine_access_grants", "engine_access_grant_revocations"}
		snapshot := func() map[string]string {
			t.Helper()
			values := map[string]string{}
			for _, table := range tables {
				var value string
				// Names above are fixed test-owned identifiers, never input.
				if err := db.QueryRow("SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text)::text,'[]') FROM system." + table + " t").Scan(&value); err != nil {
					t.Fatal(err)
				}
				values[table] = value
			}
			return values
		}
		before := snapshot()
		return func() {
			for table, after := range snapshot() {
				if after != before[table] {
					t.Fatalf("definition changed existing %s authority or credentials", table)
				}
			}
			var count int
			if err := db.QueryRow(`SELECT count(*) FROM system.permissions WHERE permission_key='develop.file_execution.read'
                AND owner_module='develop' AND action='read' AND risk_level='low'
                AND delegable=false AND tenant_customizable=false AND allowed_scope_types=ARRAY['tenant']::text[] AND status='active'`).Scan(&count); err != nil || count != 1 {
				t.Fatal("invalid permission definition", count, err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions r JOIN system.permissions p ON p.id=r.permission_id WHERE p.permission_key='develop.file_execution.read'`).Scan(&count); err != nil || count != 0 {
				t.Fatal("definition assigned to a role", count, err)
			}
		}
	})
}
