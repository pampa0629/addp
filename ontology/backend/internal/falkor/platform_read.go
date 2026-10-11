package falkor

import (
	"context"
	"reflect"

	"github.com/addp/ontology/internal/platform"
)

// PlatformContext expands only the dependencies of the PG-selected generation.
// PG supplies frozen descriptions and ordering, never a graph-failure fallback.
func (c *Client) PlatformContext(ctx context.Context, snapshot *platform.Snapshot, generation string) (platform.Context, error) {
	p, err := PlanPlatform(snapshot, generation)
	if err != nil {
		return platform.Context{}, err
	}
	if err := c.Verify(ctx, p); err != nil {
		return platform.Context{}, err
	}
	d, err := snapshot.Context()
	if err != nil {
		return platform.Context{}, ErrInvalid
	}
	rows, err := c.query(ctx, p.key,
		"MATCH (a:Definition {id:$capability,kind:'capability'})-[r:Link]->(n:Definition) WHERE r.kind IN $kinds OPTIONAL MATCH (n)-[:Link {kind:'uses_tool'}]->(tool:Definition) RETURN r.kind,n.kind,n.name,coalesce(tool.name,'') ORDER BY r.kind,n.kind,n.name,coalesce(tool.name,'') LIMIT $limit",
		platformDependencyParameters(d.Capability, len(p.dependencyRows)+1), true, MaxQueryTime)
	if err != nil {
		return platform.Context{}, err
	}
	if !reflect.DeepEqual(rows, p.dependencyRows) {
		return platform.Context{}, ErrProtocol
	}
	conditionRows, err := c.query(ctx, p.key,
		"MATCH (:Definition {id:$capability,kind:'capability'})-[:Link {kind:'requires'}]->(condition:Definition {kind:'condition'})-[:Link {kind:'reads_via'}]->(entry:Definition {kind:'requirement'})-[:Link {kind:'uses_tool'}]->(tool:Definition {kind:'tool'}) RETURN condition.name,entry.name,tool.name ORDER BY condition.name,entry.name,tool.name LIMIT $limit",
		map[string]any{"capability": "capability:" + d.Capability, "limit": int64(len(p.conditionRows) + 1)}, true, MaxQueryTime)
	if err != nil {
		return platform.Context{}, err
	}
	if !reflect.DeepEqual(conditionRows, p.conditionRows) {
		return platform.Context{}, ErrProtocol
	}
	evidence := map[platform.Relation]bool{}
	for _, row := range conditionRows {
		evidence[platform.Relation{From: row[0].(string), Kind: "reads_via", To: row[1].(string)}] = true
	}
	relations := make([]platform.Relation, 0, len(d.Relations))
	for _, relation := range d.Relations {
		if relation.Kind != "reads_via" || evidence[relation] {
			relations = append(relations, relation)
		}
	}
	d.Relations = relations
	// Expand the graph's dependency selection; hydrate text/order from this same
	// immutable release. Exact comparison above rejects missing or extra paths.
	groups := map[string]map[string]bool{}
	selected := map[string]map[string]bool{}
	for _, row := range rows {
		kind, name, tool := row[0].(string), row[2].(string), row[3].(string)
		if selected[kind] == nil {
			selected[kind] = map[string]bool{}
		}
		selected[kind][name] = true
		switch kind {
		case "owned_by":
			d.Operation.Owner = name
		case "implemented_by":
			d.Operation.Skill = name
		case "invokes":
			d.Operation.Tool = name
		case "has_requirement":
			if groups[name] == nil {
				groups[name] = map[string]bool{}
			}
			groups[name][tool] = true
		}
	}
	d.Operation.InputsRequired = selectedInOrder(d.Operation.InputsRequired, selected["requires"])
	d.Operation.Effects = selectedInOrder(d.Operation.Effects, selected["produces"])
	d.Operation.ExcludedEffects = selectedInOrder(d.Operation.ExcludedEffects, selected["excludes"])
	for i := range d.Requirements {
		d.Requirements[i].Tools = selectedInOrder(d.Requirements[i].Tools, groups[d.Requirements[i].ID])
	}
	return d, ctx.Err()
}

func platformDependencyParameters(capability string, limit int) map[string]any {
	return map[string]any{"capability": "capability:" + capability, "kinds": []any{"owned_by", "implemented_by", "invokes", "requires", "produces", "excludes", "has_requirement"}, "limit": int64(limit)}
}

func selectedInOrder(order []string, selected map[string]bool) []string {
	result := make([]string, 0, len(order))
	for _, id := range order {
		if selected[id] {
			result = append(result, id)
		}
	}
	return result
}
