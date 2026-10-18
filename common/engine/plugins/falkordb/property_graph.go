package falkordb

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/addp/common/engine/plugin"
	pg "github.com/addp/common/engine/propertygraph"
)

var _ plugin.PropertyGraphProvider = (*FalkorDBPlugin)(nil)

func (*FalkorDBPlugin) GraphDialect() plugin.PropertyGraphDialect {
	return plugin.PropertyGraphDialect{IDTemplate: "toString(id(%s))", DegreeTemplate: "size((%s)--())"}
}

// FalkorDB IDs are native integers; the domain contract consistently uses strings.
// The expression is built here, never rewritten inside user query text.
func (p *FalkorDBPlugin) SyncGraphSearchIndexes(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath, schema plugin.GraphSearchSchema) ([]plugin.GraphSearchIndex, error) {
	if err := pg.ValidateSchema(schema); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	run := func(q string, params map[string]interface{}) (*plugin.GraphQueryResult, error) {
		return p.ExecuteGraphQueryAtPath(ctx, c, path, q, plugin.QueryOptions{Parameters: params})
	}
	result, err := run("CALL db.indexes() YIELD label, properties, types, entitytype RETURN label, properties, types, entitytype", nil)
	if err != nil {
		return nil, err
	}
	expected := map[string][]string{}
	for _, d := range schema.Indexes {
		label := d.Labels[0]
		expected[label] = pg.Set(append(expected[label], d.Properties...))
	}
	existing := map[string][]string{}
	for _, row := range result.Rows {
		if fmt.Sprint(row["entitytype"]) != "NODE" {
			continue
		}
		fulltext, err := graphFullTextProperties(row)
		if err != nil {
			return nil, err
		}
		if len(fulltext) > 0 {
			existing[fmt.Sprint(row["label"])] = pg.Set(fulltext)
		}
	}
	if !schema.Managed {
		for label, props := range expected {
			if !pg.Same(existing[label], props) {
				return nil, fmt.Errorf("%w: %s", plugin.ErrGraphSearchIndexConflict, label)
			}
		}
		return schema.Indexes, waitGraphSearchIndexes(ctx, expected, run)
	}
	for _, label := range sortedKeys(existing) {
		if !pg.Same(existing[label], expected[label]) {
			if _, err = run("CALL db.idx.fulltext.drop($label)", map[string]interface{}{"label": label}); err != nil {
				return nil, err
			}
		}
	}
	for _, label := range sortedKeys(expected) {
		props := expected[label]
		if pg.Same(existing[label], props) {
			continue
		}
		args := []string{"$label"}
		params := map[string]interface{}{"label": label}
		for i, prop := range props {
			key := fmt.Sprintf("p%d", i)
			params[key] = prop
			args = append(args, "$"+key)
		}
		if _, err = run("CALL db.idx.fulltext.createNodeIndex("+strings.Join(args, ",")+")", params); err != nil {
			return nil, err
		}
	}
	return schema.Indexes, waitGraphSearchIndexes(ctx, expected, run)
}

func graphFullTextProperties(row map[string]interface{}) ([]string, error) {
	// 4.20.6 reports a property -> index kinds map, not a flat kind list.
	kinds, ok := row["types"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid FalkorDB index metadata")
	}
	var fulltext []string
	for _, prop := range pg.Strings(row["properties"]) {
		for _, kind := range pg.Strings(kinds[prop]) {
			if kind == "FULLTEXT" {
				fulltext = append(fulltext, prop)
			}
		}
	}
	return fulltext, nil
}

// Full-text index creation returns before background population completes.
// A synchronized search schema must be usable before either Graph reader runs.
func waitGraphSearchIndexes(ctx context.Context, expected map[string][]string, run func(string, map[string]interface{}) (*plugin.GraphQueryResult, error)) error {
	if len(expected) == 0 {
		return nil
	}
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	for {
		result, err := run("CALL db.indexes() YIELD label, properties, types, entitytype, status RETURN label, properties, types, entitytype, status", nil)
		if err != nil {
			return err
		}
		ready := map[string]bool{}
		for _, row := range result.Rows {
			label, node := fmt.Sprint(row["label"]), row["entitytype"] == "NODE"
			props, needed := expected[label]
			if !node || !needed {
				continue
			}
			fulltext, err := graphFullTextProperties(row)
			if err != nil {
				return err
			}
			if len(fulltext) == 0 {
				continue
			}
			if !pg.Same(fulltext, props) {
				return fmt.Errorf("%w: %s", plugin.ErrGraphSearchIndexConflict, label)
			}
			status, ok := row["status"].(string)
			if !ok || (status != "OPERATIONAL" && !(strings.HasPrefix(status, "[Indexing] ") && strings.HasSuffix(status, ": UNDER CONSTRUCTION"))) {
				return fmt.Errorf("invalid FalkorDB index status for %s", label)
			}
			ready[label] = status == "OPERATIONAL"
		}
		allReady := len(ready) == len(expected)
		for _, value := range ready {
			allReady = allReady && value
		}
		if allReady {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for FalkorDB full-text indexes: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return pg.Set(keys)
}
func (*FalkorDBPlugin) GraphSearchQuery(index plugin.GraphSearchIndex, term string) (string, map[string]interface{}, error) {
	// Escape RediSearch punctuation and bind a literal phrase to the declared fields.
	var escaped strings.Builder
	for _, r := range strings.TrimSpace(term) {
		if strings.ContainsRune(`,.<>{}[]"':;!@#$%^&*()-+=~|/\?`, r) {
			escaped.WriteByte('\\')
		}
		escaped.WriteRune(r)
	}
	fields := make([]string, 0, len(index.Properties))
	for _, field := range index.Properties {
		var b strings.Builder
		for _, r := range field {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
		fields = append(fields, b.String())
	}
	return "CALL db.idx.fulltext.queryNodes($label, $term) YIELD node, score", map[string]interface{}{"label": index.Labels[0], "term": "@" + strings.Join(fields, "|") + ":(\"" + escaped.String() + "\")"}, nil
}
func (p *FalkorDBPlugin) ListGraphConstraints(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath) ([]plugin.GraphConstraint, error) {
	result, err := p.ExecuteGraphQueryAtPath(ctx, c, path, "CALL db.constraints() YIELD type, label, properties, entitytype, status RETURN type, label, properties, entitytype, status", plugin.QueryOptions{})
	if err != nil {
		return nil, err
	}
	out := make([]plugin.GraphConstraint, 0, len(result.Rows))
	for _, row := range result.Rows {
		if row["entitytype"] != "NODE" {
			continue
		}
		label := fmt.Sprint(row["label"])
		props := pg.Strings(row["properties"])
		out = append(out, plugin.GraphConstraint{Name: label + ":" + strings.Join(props, ","), Type: fmt.Sprint(row["type"]), Label: label, Properties: props, Status: fmt.Sprint(row["status"])})
	}
	return out, nil
}
func (p *FalkorDBPlugin) EnsureGraphUniqueConstraint(ctx context.Context, c plugin.ConnectionInfo, path plugin.EngineCatalogPath, d plugin.GraphConstraint) error {
	if d.Label == "" || len(d.Properties) != 1 {
		return fmt.Errorf("unique constraint requires a label and one property")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	find := func() (bool, error) {
		all, err := p.ListGraphConstraints(ctx, c, path)
		if err != nil {
			return false, err
		}
		for _, v := range all {
			if v.Type == "UNIQUE" && v.Label == d.Label && pg.Same(v.Properties, d.Properties) {
				switch v.Status {
				case "OPERATIONAL":
					return true, nil
				case "FAILED":
					return false, fmt.Errorf("graph unique constraint failed: %s", d.Label)
				}
			}
		}
		return false, nil
	}
	all, err := p.ListGraphConstraints(ctx, c, path)
	if err != nil {
		return err
	}
	exists := false
	for _, v := range all {
		if v.Type == "UNIQUE" && v.Label == d.Label && pg.Same(v.Properties, d.Properties) {
			exists = true
			if v.Status == "FAILED" {
				return fmt.Errorf("graph unique constraint failed: %s", d.Label)
			}
		}
	}
	if !exists {
		_, err = p.ExecuteGraphQueryAtPath(ctx, c, path, "CREATE INDEX FOR (n:"+pg.Identifier(d.Label)+") ON (n."+pg.Identifier(d.Properties[0])+")", plugin.QueryOptions{})
		if err != nil {
			return err
		}
		name, err := p.graphName(path)
		if err != nil {
			return err
		}
		client, close, err := p.open(ctx, c)
		if err != nil {
			return err
		}
		defer close()
		if err = verifyLocal(ctx, client, name); err != nil {
			return err
		}
		if err = client.Do(ctx, "GRAPH.CONSTRAINT", "CREATE", name, "UNIQUE", "NODE", d.Label, "PROPERTIES", 1, d.Properties[0]).Err(); err != nil {
			return err
		}
	}
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	for {
		ready, err := find()
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
