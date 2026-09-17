package falkor

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/addp/ontology/internal/semantic"
	"github.com/google/uuid"
)

// Projection is an immutable plan derived only from a frozen semantic snapshot.
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
	node := func(id, kind, name string) {
		p.nodes = append(p.nodes, map[string]any{"id": id, "kind": kind, "name": name})
		p.nodeRows = append(p.nodeRows, []any{id, kind, name})
	}
	edge := func(from, to, kind string) {
		p.edges = append(p.edges, map[string]any{"from_id": from, "to_id": to, "kind": kind})
		p.edgeRows = append(p.edgeRows, []any{from, to, kind})
	}
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

// Build creates a never-overwritten generation. Failure leaves an untrusted
// partial graph for the owner to reconcile; it is never reported as ready.
// The caller must hold execution authorization and a valid PG lease, then
// recheck both plus withdrawal and activation CAS after this call succeeds.
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
	nodes, err := c.query(ctx, p.key, "MATCH (n:Definition) RETURN n.id, n.kind, n.name ORDER BY n.id, n.kind, n.name LIMIT 513", nil, true, MaxQueryTime)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(nodes, p.nodeRows) {
		return ErrProtocol
	}
	edges, err := c.query(ctx, p.key, "MATCH (a)-[e]->(b) RETURN a.id, b.id, e.kind ORDER BY a.id, b.id, e.kind LIMIT 4097", nil, true, MaxQueryTime)
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
