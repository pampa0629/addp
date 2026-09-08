package postgresql

import (
	"strings"
	"testing"
)

func TestPostgreSQLProtocolCatalogQueriesUseSharedCatalogRelations(t *testing.T) {
	t.Parallel()

	queries := map[string]string{
		"list tables":               postgresListTablesQuery,
		"qualified relation lookup": postgresQualifiedRelationQuery,
		"visible relation lookup":   postgresVisibleRelationQuery,
		"relation existence":        postgresRelationExistsQuery,
		"unique index fields":       postgresUniqueIndexFieldsQuery,
	}
	for name, query := range queries {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(strings.ToLower(query), "to_regclass") {
				t.Fatalf("query relies on optional to_regclass(): %s", query)
			}
			if !strings.Contains(query, "pg_catalog.pg_class") || !strings.Contains(query, "pg_catalog.pg_namespace") {
				t.Fatalf("query must resolve relations from pg_class and pg_namespace: %s", query)
			}
			lowerQuery := strings.ToLower(query)
			if strings.Contains(lowerQuery, " lateral ") || strings.Contains(lowerQuery, "with ordinality") || strings.Contains(lowerQuery, "unnest(") {
				t.Fatalf("query relies on optional catalog-array expansion syntax: %s", query)
			}
		})
	}
}

func TestPostgresUniqueFieldsMatchIgnoresIndexColumnOrder(t *testing.T) {
	t.Parallel()

	if !postgresUniqueFieldsMatch([]string{"tenant_id", "code"}, 2, []string{"code", "tenant_id"}) {
		t.Fatal("same unique field set in a different index order must match")
	}
	if postgresUniqueFieldsMatch([]string{"id"}, 2, []string{"id"}) {
		t.Fatal("expression index with an unresolved key column must not match")
	}
}
