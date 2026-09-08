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
	}
	for name, query := range queries {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(strings.ToLower(query), "to_regclass") {
				t.Fatalf("query relies on optional to_regclass(): %s", query)
			}
			if !strings.Contains(query, "pg_catalog.pg_class") || !strings.Contains(query, "pg_catalog.pg_namespace") {
				t.Fatalf("query must resolve relations from pg_class and pg_namespace: %s", query)
			}
		})
	}
}
