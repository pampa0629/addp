package propertygraph

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
)

// ShortestPath finds one undirected shortest path using native one-hop queries.
// Both engines use the same BFS semantics, including domain filtering before
// traversal. A budget overrun is an error, never an incomplete path/no-path answer.
func ShortestPath(ctx context.Context, p plugin.PropertyGraphProvider, conn plugin.ConnectionInfo, path plugin.EngineCatalogPath, source, target, nodePredicate, relationshipPredicate string) (*plugin.GraphQueryResult, error) {
	const budget = 10000
	out := &plugin.GraphQueryResult{GraphData: &plugin.GraphData{Nodes: []plugin.GraphNode{}, Relationships: []plugin.GraphRelationship{}}}
	id := p.GraphDialect().ID
	seed, err := p.ExecuteGraphQueryAtPath(ctx, conn, path, fmt.Sprintf("MATCH(n) WHERE %s IN $ids AND (%s) RETURN n", id("n"), nodePredicate), plugin.QueryOptions{Parameters: map[string]interface{}{"ids": []string{source, target}}})
	if err != nil {
		return nil, err
	}
	nodes := map[string]plugin.GraphNode{}
	if seed.GraphData != nil {
		for _, n := range seed.GraphData.Nodes {
			nodes[n.ElementId] = n
		}
	}
	if _, ok := nodes[source]; !ok {
		return out, nil
	}
	if _, ok := nodes[target]; !ok {
		return out, nil
	}
	if source == target {
		out.GraphData.Nodes = append(out.GraphData.Nodes, nodes[source])
		return out, nil
	}
	type previous struct {
		node string
		edge plugin.GraphRelationship
	}
	parents := map[string]previous{}
	seen := map[string]bool{source: true}
	frontier := []string{source}
	for depth := 0; depth < 10 && len(frontier) > 0; depth++ {
		q := fmt.Sprintf("MATCH(n)-[r]-(m) WHERE %s IN $frontier AND (%s) RETURN n,r,m,%s AS from_id,%s AS to_id,%s AS relationship_id LIMIT %d", id("n"), relationshipPredicate, id("n"), id("m"), id("r"), budget+1)
		result, err := p.ExecuteGraphQueryAtPath(ctx, conn, path, q, plugin.QueryOptions{Parameters: map[string]interface{}{"frontier": frontier}})
		if err != nil {
			return nil, err
		}
		if len(result.Rows) > budget {
			return nil, plugin.ErrGraphTraversalLimit
		}
		edges := map[string]plugin.GraphRelationship{}
		if result.GraphData != nil {
			for _, n := range result.GraphData.Nodes {
				nodes[n.ElementId] = n
			}
			for _, r := range result.GraphData.Relationships {
				edges[r.ElementId] = r
			}
		}
		next := []string{}
		for _, row := range result.Rows {
			from, to, edgeID := fmt.Sprint(row["from_id"]), fmt.Sprint(row["to_id"]), fmt.Sprint(row["relationship_id"])
			if seen[to] {
				continue
			}
			edge, ok := edges[edgeID]
			if !ok {
				return nil, fmt.Errorf("graph path returned an unresolved relationship")
			}
			seen[to] = true
			parents[to] = previous{from, edge}
			if len(seen) > budget {
				return nil, plugin.ErrGraphTraversalLimit
			}
			next = append(next, to)
			if to == target {
				for current := target; current != source; {
					out.GraphData.Nodes = append(out.GraphData.Nodes, nodes[current])
					step := parents[current]
					out.GraphData.Relationships = append(out.GraphData.Relationships, step.edge)
					current = step.node
				}
				out.GraphData.Nodes = append(out.GraphData.Nodes, nodes[source])
				return out, nil
			}
		}
		frontier = next
	}
	return out, nil
}
