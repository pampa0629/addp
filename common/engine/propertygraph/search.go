// Package propertygraph composes native graph operations without owner-specific semantics.
package propertygraph

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/addp/common/engine/plugin"
)

func Identifier(value string) string { return "`" + strings.ReplaceAll(value, "`", "``") + "`" }

func Strings(value interface{}) []string {
	if values, ok := value.([]string); ok {
		return values
	}
	var out []string
	if values, ok := value.([]interface{}); ok {
		for _, v := range values {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func Set(values []string) []string {
	seen := map[string]bool{}
	for _, v := range values {
		seen[v] = true
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func Same(a, b []string) bool { return strings.Join(Set(a), "\x00") == strings.Join(Set(b), "\x00") }

func ValidateSchema(schema plugin.GraphSearchSchema) error {
	if schema.OwnerPrefix == "" {
		return fmt.Errorf("graph search owner is required")
	}
	for _, index := range schema.Indexes {
		if !strings.HasPrefix(index.Name, schema.OwnerPrefix) || len(index.Labels) == 0 || len(index.Properties) == 0 {
			return fmt.Errorf("invalid graph search index definition")
		}
		for _, name := range append(append([]string{index.Name}, index.Labels...), index.Properties...) {
			if name == "" || strings.ContainsRune(name, 0) {
				return fmt.Errorf("invalid graph search identifier")
			}
		}
	}
	return nil
}

// Search merges native fulltext results, preserving scores and accurate totals.
// The predicate comes exclusively from the trusted owner's domain model.
func Search(ctx context.Context, p plugin.PropertyGraphProvider, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, indexes []plugin.GraphSearchIndex, term, predicate string, skip, limit int) (*plugin.GraphQueryResult, int64, error) {
	const budget = 10000
	if skip < 0 || limit <= 0 || limit > budget {
		return nil, 0, fmt.Errorf("invalid graph search pagination")
	}
	type hit struct {
		node  plugin.GraphNode
		score float64
	}
	hits := map[string]hit{}
	for _, index := range indexes {
		query, params, err := p.GraphSearchQuery(index, term)
		if err != nil {
			return nil, 0, err
		}
		query += fmt.Sprintf(" WITH node, score WHERE all(label IN $labels WHERE label IN labels(node)) AND (%s) RETURN node, %s AS graph_id, score LIMIT %d", predicate, p.GraphDialect().ID("node"), budget+1)
		params["labels"] = index.Labels
		result, err := p.ExecuteGraphQueryAtPath(ctx, conn, path, query, plugin.QueryOptions{Parameters: params})
		if err != nil {
			return nil, 0, err
		}
		if len(result.Rows) > budget {
			return nil, 0, plugin.ErrGraphSearchLimit
		}
		nodes := map[string]plugin.GraphNode{}
		if result.GraphData != nil {
			for _, node := range result.GraphData.Nodes {
				nodes[node.ElementId] = node
			}
		}
		for _, row := range result.Rows {
			id := fmt.Sprint(row["graph_id"])
			node, ok := nodes[id]
			if !ok {
				return nil, 0, fmt.Errorf("graph search returned an unresolved node")
			}
			score, _ := row["score"].(float64)
			if old, ok := hits[id]; !ok || old.score < score {
				hits[id] = hit{node, score}
			}
			if len(hits) > budget {
				return nil, 0, plugin.ErrGraphSearchLimit
			}
		}
	}
	ordered := make([]hit, 0, len(hits))
	for _, h := range hits {
		ordered = append(ordered, h)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].score == ordered[j].score {
			return ordered[i].node.ElementId < ordered[j].node.ElementId
		}
		return ordered[i].score > ordered[j].score
	})
	out := &plugin.GraphQueryResult{GraphData: &plugin.GraphData{Nodes: []plugin.GraphNode{}, Relationships: []plugin.GraphRelationship{}}}
	for i := min(skip, len(ordered)); i < min(skip+limit, len(ordered)); i++ {
		out.GraphData.Nodes = append(out.GraphData.Nodes, ordered[i].node)
	}
	return out, int64(len(ordered)), nil
}

func ValidateTarget(path plugin.EngineCatalogPath, model plugin.EngineCatalogModelSpec) error {
	if path.Version != plugin.EngineCatalogPathVersion || path.EngineID == 0 || len(path.Segments) != len(model.Levels)+1 || path.Segments[0].Term != model.RootTerm || path.Segments[0].Kind != model.RootTerm || path.Segments[0].Name != "" {
		return fmt.Errorf("invalid graph target path")
	}
	for i, level := range model.Levels {
		s := path.Segments[i+1]
		valid := false
		for _, kind := range level.Kinds {
			valid = valid || s.Kind == kind
		}
		if s.Term != level.Term || !valid || s.Name == "" {
			return fmt.Errorf("invalid graph target segment")
		}
	}
	return nil
}
