package migration

import (
	"database/sql"
	"testing"
)

func TestGraphProtectionRuntimeForwardMigrationAgainstPostgres(t *testing.T) {
	testAuthorizationFactsMigration(t, "000218_graph_protection_runtime.up.sql", 218, 0, func(db *sql.DB) func() {
		var assignments, before int64
		if err := db.QueryRow("SELECT count(*) FROM system.role_assignments").Scan(&assignments); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow("SELECT p.authorization_version FROM system.principals p JOIN system.service_principals s ON s.id=p.id WHERE s.name='addp-graph'").Scan(&before); err != nil {
			t.Fatal(err)
		}
		return func() {
			var count int64
			if err := db.QueryRow(`SELECT count(*) FROM system.role_permissions b JOIN system.roles r ON r.id=b.role_id JOIN system.permissions p ON p.id=b.permission_id WHERE r.tenant_id IS NULL AND ((r.role_key='tenant.graph_runtime' AND p.permission_key IN ('security.protection_projection.read','security.protection_projection.update')) OR (r.role_key='platform.graph_runtime' AND p.permission_key='platform.tenant.read'))`).Scan(&count); err != nil || count != 3 {
				t.Fatalf("Graph runtime bindings=%d %v", count, err)
			}
			if err := db.QueryRow("SELECT count(*) FROM system.role_assignments").Scan(&count); err != nil || count != assignments {
				t.Fatal("new assignments created", err)
			}
			if err := db.QueryRow("SELECT p.authorization_version FROM system.principals p JOIN system.service_principals s ON s.id=p.id WHERE s.name='addp-graph'").Scan(&count); err != nil || count <= before {
				t.Fatal("runtime authorization version was not invalidated", err)
			}
		}
	})
}
