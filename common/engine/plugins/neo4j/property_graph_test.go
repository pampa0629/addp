package neo4j

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	pg "github.com/addp/common/engine/propertygraph"
)

// Neo4j IF NOT EXISTS also skips a different name with an equivalent schema.
// Keep the physical catalog independent of the names returned by the provider.
func TestSearchIndexesResolveOverlappingLabelDefinitionsToExistingPhysicalNames(t *testing.T) {
	indexes := []plugin.GraphSearchIndex{
		{Name: "graph_4_partner", Labels: []string{"Organization", "Partner"}, Properties: []string{"name", "id"}},
		{Name: "graph_4_organization", Labels: []string{"Organization"}, Properties: []string{"id", "name"}},
		{Name: "graph_4_other_fields", Labels: []string{"Organization"}, Properties: []string{"title"}},
	}
	physical := map[string]plugin.GraphSearchIndex{"other_owner_person": {Name: "other_owner_person", Labels: []string{"Person"}, Properties: []string{"name"}}}
	created, dropped := 0, 0
	identifiers := regexp.MustCompile("`([^`]+)`")
	run := func(query string, _ map[string]interface{}) (*plugin.GraphQueryResult, error) {
		result := &plugin.GraphQueryResult{}
		switch {
		case strings.HasPrefix(query, "SHOW FULLTEXT INDEXES"):
			for _, index := range physical {
				result.Rows = append(result.Rows, map[string]interface{}{"name": index.Name, "labelsOrTypes": index.Labels, "properties": index.Properties, "state": "ONLINE", "entityType": "NODE"})
			}
		case strings.HasPrefix(query, "CREATE FULLTEXT INDEX"):
			parts := identifiers.FindAllStringSubmatch(query, -1)
			index := plugin.GraphSearchIndex{Name: parts[0][1], Labels: []string{parts[1][1]}}
			for _, part := range parts[2:] {
				index.Properties = append(index.Properties, part[1])
			}
			for _, existing := range physical {
				if index.Name == existing.Name || (pg.Same(index.Labels, existing.Labels) && pg.Same(index.Properties, existing.Properties)) {
					return result, nil // Native idempotent DDL does not create the requested alias.
				}
			}
			physical[index.Name] = index
			created++
		case strings.HasPrefix(query, "DROP INDEX"):
			name := identifiers.FindStringSubmatch(query)[1]
			if !strings.HasPrefix(name, "graph_4_") {
				t.Fatalf("foreign index deletion: %s", query)
			}
			delete(physical, name)
			dropped++
		case query == "CALL db.awaitIndexes(30)":
		default:
			t.Fatalf("unexpected query: %s", query)
		}
		return result, nil
	}
	for _, managed := range []bool{true, true, false} {
		resolved, err := syncGraphSearchIndexes(plugin.GraphSearchSchema{OwnerPrefix: "graph_4_", Managed: managed, Indexes: indexes}, run)
		if err != nil {
			t.Fatal(err)
		}
		if len(resolved) != len(indexes) {
			t.Fatal(resolved)
		}
		for i, index := range resolved {
			if _, exists := physical[index.Name]; !exists {
				t.Fatalf("resolved missing physical index %q for %v", index.Name, index.Labels)
			}
			if !reflect.DeepEqual(index.Labels, indexes[i].Labels) || !reflect.DeepEqual(index.Properties, indexes[i].Properties) {
				t.Fatalf("logical search definition changed: %#v", index)
			}
		}
		if resolved[0].Name != resolved[1].Name || resolved[0].Name == resolved[2].Name {
			t.Fatalf("physical schema grouping differs: %#v", resolved)
		}
		if created != 2 || dropped != 0 || len(physical) != 3 {
			t.Fatalf("non-idempotent/foreign mutation: created=%d dropped=%d catalog=%v", created, dropped, physical)
		}
		// Reordering logical definitions must not replace the existing physical index.
		indexes[0], indexes[1] = indexes[1], indexes[0]
	}
}
