package falkor

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/addp/ontology/internal/platform"
	"github.com/addp/ontology/internal/semantic"
	"github.com/google/uuid"
)

// Projection is an immutable plan derived only from a validated frozen snapshot.
// Its identity must come from the trusted publication owner, never a user key.
// Build/Verify do not authorize, publish, activate, or certify business facts.
type Projection struct {
	key      string
	digest   string
	nodes    []any
	edges    []any
	nodeRows [][]any
	edgeRows [][]any
}

func (p *Projection) node(id, kind, name string) {
	p.nodes = append(p.nodes, map[string]any{"id": id, "kind": kind, "name": name})
	p.nodeRows = append(p.nodeRows, []any{id, kind, name})
}

func (p *Projection) edge(from, to, kind string) {
	p.edges = append(p.edges, map[string]any{"from_id": from, "to_id": to, "kind": kind})
	p.edgeRows = append(p.edgeRows, []any{from, to, kind})
}

func Plan(snapshot *semantic.Snapshot, generation string) (*Projection, error) {
	if snapshot == nil {
		return nil, ErrInvalid
	}
	id, err := uuid.Parse(generation)
	if err != nil || id == uuid.Nil || id.String() != generation {
		return nil, ErrInvalid
	}
	scope := snapshot.Scope()
	p := &Projection{key: fmt.Sprintf("ontology:t%d:%s:r%d:g%s", scope.TenantID, scope.OntologyID, scope.Revision, strings.ReplaceAll(generation, "-", "")), digest: snapshot.Digest()}
	if !graphKeyPattern.MatchString(p.key) {
		return nil, ErrInvalid
	}
	var stored struct {
		Definition semantic.Definition `json:"definition"`
	}
	if err := json.Unmarshal(snapshot.CanonicalJSON(), &stored); err != nil {
		return nil, ErrInvalid
	}
	node, edge := p.node, p.edge
	for _, c := range stored.Definition.Classes {
		node(c.ID, "class", c.Name)
		for _, parent := range c.Parents {
			edge(c.ID, parent, "parent")
		}
	}
	for _, property := range stored.Definition.Properties {
		node(property.ID, "property", property.Name)
		edge(property.ID, property.ClassID, "owner")
	}
	for _, relation := range stored.Definition.Relations {
		node(relation.ID, "relation", relation.Name)
		edge(relation.ID, relation.From, "from")
		edge(relation.ID, relation.To, "to")
		if relation.InverseID != "" {
			edge(relation.ID, relation.InverseID, "inverse")
		}
	}
	for _, rule := range stored.Definition.Rules {
		node(rule.ID, "rule", rule.ID)
		edge(rule.ID, rule.ClassID, "target")
		for _, input := range rule.Inputs {
			edge(rule.ID, input.PropertyID, "input")
		}
	}
	sortRows(p.nodeRows)
	sortRows(p.edgeRows)
	return p, nil
}

func sortRows(rows [][]any) {
	slices.SortFunc(rows, func(a, b []any) int {
		for i := range a {
			if order := strings.Compare(a[i].(string), b[i].(string)); order != 0 {
				return order
			}
		}
		return 0
	})
}

// PlanPlatform projects only concepts, localized names and explicit relations.
// The complete operation contract remains in its authoritative PG snapshot.
func PlanPlatform(snapshot *platform.Snapshot, generation string) (*Projection, error) {
	d, err := snapshot.Context()
	if err != nil {
		return nil, ErrInvalid
	}
	id, err := uuid.Parse(generation)
	if err != nil || id == uuid.Nil || id.String() != generation {
		return nil, ErrInvalid
	}
	p := &Projection{key: fmt.Sprintf("ontology:p:%s:r%d:g%s", d.Capability, d.Revision, strings.ReplaceAll(generation, "-", "")), digest: snapshot.Digest()}
	if !graphKeyPattern.MatchString(p.key) {
		return nil, ErrInvalid
	}
	node, edge := p.node, p.edge
	for _, concept := range d.Concepts {
		conceptID := "concept:" + concept.ID
		node(conceptID, "concept", concept.ID)
		for _, language := range slices.Sorted(maps.Keys(concept.Name)) {
			nameID := "name:" + concept.ID + ":" + language
			node(nameID, "name", concept.Name[language])
			edge(conceptID, nameID, "name:"+language)
		}
	}
	for _, relation := range d.Relations {
		edge("concept:"+relation.From, "concept:"+relation.To, relation.Kind)
	}
	sortRows(p.nodeRows)
	sortRows(p.edgeRows)
	return p, nil
}

// Build creates a never-overwritten generation. Failure leaves an untrusted
// partial graph for the owner to reconcile; it is never reported as ready.
// The owner must authorize and fence publication before and after this call:
// Tenant execution uses its lease; platform publication uses its generation CAS.
func (c *Client) Build(ctx context.Context, p *Projection) error {
	if p == nil || !graphKeyPattern.MatchString(p.key) || len(p.nodes) == 0 {
		return ErrInvalid
	}
	rows, err := c.query(ctx, p.key,
		"OPTIONAL MATCH (n) WITH count(n) AS existing WHERE existing = 0 CREATE (:Projection {digest:$digest}) RETURN 1",
		map[string]any{"digest": p.digest}, false, MaxQueryTime)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(rows, [][]any{{int64(1)}}) {
		return ErrRejected
	}
	_, err = c.query(ctx, p.key, "UNWIND $nodes AS item CREATE (:Definition {id:item.id, kind:item.kind, name:item.name}) RETURN count(*)", map[string]any{"nodes": p.nodes}, false, MaxQueryTime)
	if err != nil {
		return err
	}
	if len(p.edges) != 0 {
		_, err = c.query(ctx, p.key, "UNWIND $edges AS item MATCH (a:Definition {id:item.from_id}), (b:Definition {id:item.to_id}) CREATE (a)-[:Link {kind:item.kind}]->(b) RETURN count(*)", map[string]any{"edges": p.edges}, false, MaxQueryTime)
		if err != nil {
			return err
		}
	}
	return c.Verify(ctx, p)
}

// Verify compares all projected members and edges, not merely a stored digest.
// Rules and business values remain in their original owners, not in this graph.
func (c *Client) Verify(ctx context.Context, p *Projection) error {
	if p == nil || !graphKeyPattern.MatchString(p.key) {
		return ErrInvalid
	}
	meta, err := c.query(ctx, p.key, "MATCH (p:Projection) RETURN p.digest", nil, true, MaxQueryTime)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(meta, [][]any{{p.digest}}) {
		return ErrProtocol
	}
	nodes, err := c.query(ctx, p.key, "MATCH (n:Definition) RETURN n.id, n.kind, n.name ORDER BY n.id, n.kind, n.name LIMIT $limit", map[string]any{"limit": int64(len(p.nodeRows) + 1)}, true, MaxQueryTime)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(nodes, p.nodeRows) {
		return ErrProtocol
	}
	edges, err := c.query(ctx, p.key, "MATCH (a)-[e]->(b) RETURN a.id, b.id, e.kind ORDER BY a.id, b.id, e.kind LIMIT $limit", map[string]any{"limit": int64(len(p.edgeRows) + 1)}, true, MaxQueryTime)
	if err != nil {
		return err
	}
	if len(edges) != len(p.edgeRows) {
		return ErrProtocol
	}
	for i := range edges {
		if !reflect.DeepEqual(edges[i], p.edgeRows[i]) {
			return ErrProtocol
		}
	}
	counts, err := c.query(ctx, p.key, "MATCH (n) RETURN count(n)", nil, true, MaxQueryTime)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(counts, [][]any{{int64(len(p.nodes) + 1)}}) {
		return ErrProtocol
	}
	return ctx.Err()
}
