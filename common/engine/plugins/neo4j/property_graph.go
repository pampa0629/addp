package neo4j

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/engine/plugin"
	pg "github.com/addp/common/engine/propertygraph"
)

var _ plugin.PropertyGraphProvider = (*Neo4jPlugin)(nil)

func (*Neo4jPlugin) GraphDialect() plugin.PropertyGraphDialect {
	return plugin.PropertyGraphDialect{IDTemplate: "elementId(%s)", DegreeTemplate: "COUNT { (%s)--() }", GDS: true, Spatial: true, AllShortestPaths: true, ExcludedNodeLabels: []string{"SpatialLayer"}, ExcludedRelationshipTypes: []string{"RTREE_METADATA", "RTREE_REFERENCE", "RTREE_ROOT"}}
}

func (p *Neo4jPlugin) ExecuteGraphQueryAtPath(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, query string, opts plugin.QueryOptions) (*plugin.GraphQueryResult, error) {
	if err := pg.ValidateTarget(path, p.EngineCatalogModel()); err != nil {
		return nil, err
	}
	database, ok := neo4jDatabaseFromCatalogPath(path)
	if !ok || path.EngineID == 0 || plugin.EngineCatalogPathWithoutRoot(path).Segments[1].Name != "graph" {
		return nil, fmt.Errorf("explicit graph leaf is required")
	}
	scoped := cloneConnectionInfo(conn)
	scoped["database"] = database
	if err := p.verifyLocalReadCypher(ctx, scoped, &cypherReadProof{}); err != nil {
		return nil, err
	}
	return p.executeGraphQuery(ctx, scoped, query, opts)
}

func (p *Neo4jPlugin) SyncGraphSearchIndexes(ctx context.Context, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, schema plugin.GraphSearchSchema) ([]plugin.GraphSearchIndex, error) {
	run := func(q string, params map[string]interface{}) (*plugin.GraphQueryResult, error) {
		return p.ExecuteGraphQueryAtPath(ctx, conn, path, q, plugin.QueryOptions{Parameters: params})
	}
	return syncGraphSearchIndexes(schema, run)
}

func syncGraphSearchIndexes(schema plugin.GraphSearchSchema, run func(string, map[string]interface{}) (*plugin.GraphQueryResult, error)) ([]plugin.GraphSearchIndex, error) {
	if err := pg.ValidateSchema(schema); err != nil {
		return nil, err
	}
	result, err := run("SHOW FULLTEXT INDEXES YIELD name, labelsOrTypes, properties, state, entityType RETURN name, labelsOrTypes, properties, state, entityType", nil)
	if err != nil {
		return nil, err
	}
	existing := map[string]bool{}
	resolved := append([]plugin.GraphSearchIndex(nil), schema.Indexes...)
	if !schema.Managed {
		for i, d := range resolved {
			found := false
			for _, row := range result.Rows {
				if row["entityType"] == "NODE" && row["state"] == "ONLINE" && pg.Same(pg.Strings(row["labelsOrTypes"]), []string{d.Labels[0]}) && pg.Same(pg.Strings(row["properties"]), d.Properties) {
					resolved[i].Name = fmt.Sprint(row["name"])
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("%w: %s", plugin.ErrGraphSearchIndexConflict, d.Labels[0])
			}
		}
		return resolved, nil
	}
	// Neo4j permits only one fulltext index for a label/property-set schema.
	// Logical definitions still retain all labels for the search predicate.
	physicalKey := func(index plugin.GraphSearchIndex) string {
		return index.Labels[0] + "\x00" + strings.Join(pg.Set(index.Properties), "\x00")
	}
	physical := map[string]plugin.GraphSearchIndex{}
	for _, index := range schema.Indexes {
		key := physicalKey(index)
		if previous, exists := physical[key]; !exists || index.Name < previous.Name {
			physical[key] = index
		}
	}
	expected := map[string]plugin.GraphSearchIndex{}
	definitions := make([]plugin.GraphSearchIndex, 0, len(physical))
	for _, index := range physical {
		expected[index.Name] = index
		definitions = append(definitions, index)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Name < definitions[j].Name })
	for i := range resolved {
		resolved[i].Name = physical[physicalKey(resolved[i])].Name
	}
	for _, row := range result.Rows {
		name := fmt.Sprint(row["name"])
		if !strings.HasPrefix(name, schema.OwnerPrefix) {
			continue
		}
		d, ok := expected[name]
		if ok && row["entityType"] == "NODE" && pg.Same(pg.Strings(row["labelsOrTypes"]), []string{d.Labels[0]}) && pg.Same(pg.Strings(row["properties"]), d.Properties) {
			existing[name] = true
			continue
		}
		if _, err = run("DROP INDEX "+pg.Identifier(name)+" IF EXISTS", nil); err != nil {
			return nil, err
		}
	}
	for _, d := range definitions {
		if existing[d.Name] {
			continue
		}
		props := make([]string, 0, len(d.Properties))
		for _, prop := range d.Properties {
			props = append(props, "n."+pg.Identifier(prop))
		}
		if _, err = run("CREATE FULLTEXT INDEX "+pg.Identifier(d.Name)+" IF NOT EXISTS FOR (n:"+pg.Identifier(d.Labels[0])+") ON EACH ["+strings.Join(props, ",")+"]", nil); err != nil {
			return nil, err
		}
	}
	if len(schema.Indexes) > 0 {
		if _, err = run("CALL db.awaitIndexes(30)", nil); err != nil {
			return nil, err
		}
	}
	return resolved, nil
}

func (*Neo4jPlugin) GraphSearchQuery(index plugin.GraphSearchIndex, term string) (string, map[string]interface{}, error) {
	// An explicit phrase treats user input as text rather than Lucene syntax.
	term = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(term), `\`, `\\`), `"`, `\"`)
	return "CALL db.index.fulltext.queryNodes($index, $term) YIELD node, score", map[string]interface{}{"index": index.Name, "term": `"` + term + `"`}, nil
}

func (p *Neo4jPlugin) EnsureGraphUniqueConstraint(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath, d plugin.GraphConstraint) error {
	if d.Name == "" || d.Label == "" || len(d.Properties) != 1 {
		return fmt.Errorf("unique constraint requires name, label and one property")
	}
	q := "CREATE CONSTRAINT " + pg.Identifier(d.Name) + " IF NOT EXISTS FOR (n:" + pg.Identifier(d.Label) + ") REQUIRE n." + pg.Identifier(d.Properties[0]) + " IS UNIQUE"
	_, err := p.ExecuteGraphQueryAtPath(ctx, c, path, q, plugin.QueryOptions{})
	return err
}
func (p *Neo4jPlugin) ListGraphConstraints(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath) ([]plugin.GraphConstraint, error) {
	result, err := p.ExecuteGraphQueryAtPath(ctx, c, path, "SHOW CONSTRAINTS", plugin.QueryOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]plugin.GraphConstraint, 0, len(result.Rows))
	for _, row := range result.Rows {
		labels := pg.Strings(row["labelsOrTypes"])
		label := ""
		if len(labels) > 0 {
			label = labels[0]
		}
		out = append(out, plugin.GraphConstraint{Name: fmt.Sprint(row["name"]), Type: fmt.Sprint(row["type"]), Label: label, Properties: pg.Strings(row["properties"]), Status: "OPERATIONAL"})
	}
	return out, nil
}
