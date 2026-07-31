package service

import (
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/graph/internal/models"
	"gorm.io/datatypes"
)

func TestBuildSubgraphAppliesVisualSemantics(t *testing.T) {
	result := &plugin.GraphQueryResult{GraphData: &plugin.GraphData{
		Nodes: []plugin.GraphNode{
			{ElementId: "person-1", Labels: []string{"Person"}, Properties: map[string]interface{}{"name": "Ada"}},
			{ElementId: "company-1", Labels: []string{"Company"}, Properties: map[string]interface{}{"name": "ADDP"}},
			{ElementId: "researcher-1", Labels: []string{"Researcher", "Person"}, Properties: map[string]interface{}{"name": "Lin"}},
			{ElementId: "student-1", Labels: []string{"Student", "Person"}, Properties: map[string]interface{}{"name": "Sam"}},
		},
		Relationships: []plugin.GraphRelationship{
			{ElementId: "works-at-1", Type: "WORKS_AT", StartNodeId: "person-1", EndNodeId: "company-1"},
			{ElementId: "knows-1", Type: "KNOWS", StartNodeId: "person-1", EndNodeId: "company-1"},
		},
	}}

	subgraph := buildSubgraph(
		result,
		map[string]string{"Person": "#2563EB", "Person+Researcher": "#059669"},
		map[string]string{"WORKS_AT": "#DC2626"},
		map[string]bool{"WORKS_AT": true, "KNOWS": false},
		nil,
	)

	if got := subgraph.Nodes[0].Color; got != "#2563EB" {
		t.Fatalf("person color = %q, want #2563EB", got)
	}
	if got := subgraph.Nodes[1].Color; got != defaultGraphNodeColor {
		t.Fatalf("default node color = %q, want %q", got, defaultGraphNodeColor)
	}
	if got := subgraph.Nodes[2].Color; got != "#059669" {
		t.Fatalf("complete label-set color = %q, want #059669", got)
	}
	if got := subgraph.Nodes[3].Color; got != defaultGraphNodeColor {
		t.Fatalf("unmapped complete label-set color = %q, want %q", got, defaultGraphNodeColor)
	}
	if got := subgraph.Edges[0]; got.Color != "#DC2626" || !got.Directed {
		t.Fatalf("WORKS_AT visual semantics = %#v", got)
	}
	if got := subgraph.Edges[1]; got.Color != defaultGraphEdgeColor || got.Directed {
		t.Fatalf("KNOWS visual semantics = %#v", got)
	}
}

func TestBuildSubgraphUsesConfiguredDisplayPropertyAndEntityIDFallback(t *testing.T) {
	result := &plugin.GraphQueryResult{GraphData: &plugin.GraphData{Nodes: []plugin.GraphNode{
		{ElementId: "person-1", Labels: []string{"Person"}, Properties: map[string]interface{}{"name": "Ada", "employee_no": "E-001"}},
		{ElementId: "company-1", Labels: []string{"Company"}, Properties: map[string]interface{}{"name": "ADDP"}},
	}}}
	semantics := newOntologySemantics(&models.Ontology{EntityTypes: []models.EntityType{
		{Name: "Person", DisplayProperty: "employee_no", NodeLabels: datatypes.JSON(`["Person"]`)},
		{Name: "Company", NodeLabels: datatypes.JSON(`["Company"]`)},
	}})

	subgraph := buildSubgraph(result, nil, nil, nil, semantics)
	if got := subgraph.Nodes[0].DisplayName; got != "E-001" {
		t.Fatalf("configured display name = %q", got)
	}
	if got := subgraph.Nodes[1].DisplayName; got != "company-1" {
		t.Fatalf("fallback display name = %q", got)
	}
}
