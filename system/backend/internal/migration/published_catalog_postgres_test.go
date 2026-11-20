package migration

import (
	"database/sql"
	"path/filepath"
	"testing"

	commonauth "github.com/addp/common/authorization"
)

// Compare a freshly migrated database before any test identities are created.
func assertPublishedRoleBindingsAndRisks(t *testing.T, db *sql.DB) {
	t.Helper()
	catalog, err := commonauth.LoadRepositoryAuthorizationCatalog(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	type fact struct{ kind, key, value string }
	expected := make(map[fact]bool)
	for _, permission := range catalog.Permissions {
		if permission.Status == "active" {
			expected[fact{"risk", permission.Key, permission.RiskLevel}] = true
		}
	}
	for _, role := range catalog.Roles {
		expected[fact{"role", role.Key, ""}] = true
		for _, permission := range role.Permissions {
			expected[fact{"binding", role.Key, permission}] = true
		}
	}

	rows, err := db.Query(`
		SELECT 'risk', permission_key, risk_level FROM system.permissions WHERE status = 'active'
		UNION ALL
		SELECT 'role', role_key, '' FROM system.roles WHERE tenant_id IS NULL AND status = 'active'
		UNION ALL
		SELECT 'binding', r.role_key, p.permission_key
		FROM system.role_permissions b
		JOIN system.roles r ON r.id = b.role_id
		JOIN system.permissions p ON p.id = b.permission_id
		WHERE b.source_type = 'product' AND r.tenant_id IS NULL AND r.status = 'active' AND p.status = 'active'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var actual fact
		if err := rows.Scan(&actual.kind, &actual.key, &actual.value); err != nil {
			t.Fatal(err)
		}
		if !expected[actual] {
			t.Errorf("unexpected PG catalog entry: %v", actual)
		}
		delete(expected, actual)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for missing := range expected {
		t.Errorf("published catalog entry missing from PG: %v", missing)
	}
}
