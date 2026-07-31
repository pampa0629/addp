package service

import (
	"strings"
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/graph/internal/models"
)

func TestBuildAggregatedOverviewUsesShapeBucketsAndRelationshipCounts(t *testing.T) {
	nodeResult := &plugin.GraphQueryResult{QueryResult: plugin.QueryResult{Rows: []map[string]interface{}{
		{"labels": []interface{}{"Person"}, "cnt": int64(820)},
		{"labels": []interface{}{"Company", "POI"}, "cnt": int64(90)},
	}}}
	relationshipResult := &plugin.GraphQueryResult{QueryResult: plugin.QueryResult{Rows: []map[string]interface{}{
		{
			"source_labels": []interface{}{"Person"},
			"rel_type":      "WORKS_AT",
			"target_labels": []interface{}{"Company", "POI"},
			"cnt":           int64(820),
		},
	}}}

	result := buildAggregatedOverview(
		nodeResult,
		relationshipResult,
		map[string]string{"Person": "#111111", "Company+POI": "#222222"},
		map[string]string{"WORKS_AT": "#333333"},
		map[string]bool{"WORKS_AT": true},
	)

	if len(result.Nodes) != 2 || len(result.Edges) != 1 {
		t.Fatalf("overview size = %d nodes / %d edges, want 2 / 1", len(result.Nodes), len(result.Edges))
	}
	if got := result.Nodes[0]; got.Kind != "aggregate" || got.MemberCount != 820 || got.EntityType != "Person" {
		t.Fatalf("person aggregate = %#v", got)
	}
	if got := result.Edges[0]; got.Kind != "aggregate" || got.Count != 820 || got.Type != "WORKS_AT" {
		t.Fatalf("aggregate edge = %#v", got)
	}
	if result.Edges[0].Source != result.Nodes[0].ID || result.Edges[0].Target != result.Nodes[1].ID {
		t.Fatalf("aggregate edge endpoints = %q -> %q, want bucket ids", result.Edges[0].Source, result.Edges[0].Target)
	}
}

func TestBuildBrowseSnapshotDerivesSchemaStatsAndOverviewFromSameFacts(t *testing.T) {
	nodeResult := &plugin.GraphQueryResult{QueryResult: plugin.QueryResult{Rows: []map[string]interface{}{
		{"labels": []interface{}{"Person"}, "cnt": int64(820)},
		{"labels": []interface{}{"Company", "POI"}, "cnt": int64(90)},
	}}}
	relationshipResult := &plugin.GraphQueryResult{QueryResult: plugin.QueryResult{Rows: []map[string]interface{}{
		{
			"source_labels": []interface{}{"Person"},
			"rel_type":      "WORKS_AT",
			"target_labels": []interface{}{"Company", "POI"},
			"cnt":           int64(820),
		},
	}}}

	snapshot := buildBrowseSnapshot(
		nodeResult,
		relationshipResult,
		map[string]string{"Person": "#111111", "Company+POI": "#222222"},
		map[string]string{"WORKS_AT": "#333333"},
		map[string]bool{"WORKS_AT": true},
	)

	if snapshot.Stats.NodeCount != 910 || snapshot.Stats.RelationshipCount != 820 {
		t.Fatalf("snapshot stats = %#v, want 910 nodes / 820 relationships", snapshot.Stats)
	}
	if snapshot.Stats.ByLabel["Person"] != 820 || snapshot.Stats.ByLabel["Company"] != 90 || snapshot.Stats.ByLabel["POI"] != 90 {
		t.Fatalf("snapshot labels = %#v", snapshot.Stats.ByLabel)
	}
	if len(snapshot.Schema.NodeShapes) != 2 || len(snapshot.Schema.RelationshipShapes) != 1 {
		t.Fatalf("snapshot schema = %#v", snapshot.Schema)
	}
	if len(snapshot.Overview.Nodes) != 2 || len(snapshot.Overview.Edges) != 1 {
		t.Fatalf("snapshot overview = %#v", snapshot.Overview)
	}
}

func TestBuildSubgraphMarksRealGraphElements(t *testing.T) {
	result := &plugin.GraphQueryResult{GraphData: &plugin.GraphData{
		Nodes: []plugin.GraphNode{{ElementId: "person-1", Labels: []string{"Person"}}},
	}}

	subgraph := buildSubgraph(result, nil, nil, nil, nil)
	if got := subgraph.Nodes[0].Kind; got != "entity" {
		t.Fatalf("node kind = %q, want entity", got)
	}
}

func TestAggregateSeedQueryMatchesCompleteLabelSet(t *testing.T) {
	query := aggregateSeedQuery([]string{"Person", "Employee"}, 20)

	for _, fragment := range []string{
		"size(labels(n)) = 2",
		"all(label IN ['Employee','Person'] WHERE label IN labels(n))",
		"ORDER BY degree DESC, elementId(n)",
		"LIMIT 20",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("aggregateSeedQuery() = %q, missing %q", query, fragment)
		}
	}
}

func TestMergeExpandedSubgraphEnforcesBudgetsAndDeduplicates(t *testing.T) {
	out := &models.SubgraphResult{}
	incoming := &models.SubgraphResult{
		Nodes: []models.GraphNodeDTO{
			{ID: "a"},
			{ID: "b"},
			{ID: "c"},
			{ID: "d"},
		},
		Edges: []models.GraphEdgeDTO{
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e1", Source: "a", Target: "b"},
			{ID: "e2", Source: "b", Target: "c"},
			{ID: "e3", Source: "c", Target: "d"},
		},
	}

	frontier := mergeExpandedSubgraph(out, incoming, map[string]bool{}, map[string]bool{}, 3, 2)
	if len(out.Nodes) != 3 || len(out.Edges) != 2 {
		t.Fatalf("merged size = %d nodes / %d edges, want 3 / 2", len(out.Nodes), len(out.Edges))
	}
	if len(frontier) != 3 {
		t.Fatalf("frontier size = %d, want 3", len(frontier))
	}
	if out.Edges[0].ID != "e1" || out.Edges[1].ID != "e2" {
		t.Fatalf("merged edges = %#v, want unique e1/e2 within budget", out.Edges)
	}
}

func TestExpandFrontierQueryExcludesSeenRelationshipsBeforeLimit(t *testing.T) {
	query := expandFrontierQuery([]string{"node-a", "node-b"}, []string{"rel-1"}, 20)

	for _, fragment := range []string{
		"NOT elementId(r) IN ['rel-1']",
		"WITH DISTINCT r",
		"startNode(r) AS source",
		"endNode(r) AS target",
		"LIMIT 20",
	} {
		if !strings.Contains(query, fragment) {
			t.Fatalf("expandFrontierQuery() = %q, missing %q", query, fragment)
		}
	}
}
