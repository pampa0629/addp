package service

import (
	"testing"

	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMetricProfessionalRelationsIncludeBaseAndBothDependencyDirections(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:standard-professional-relations?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ATTACH DATABASE ':memory:' AS standard`,
		`CREATE TABLE standard.metrics (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, name TEXT, code TEXT, type TEXT, status TEXT, base_metric_id INTEGER)`,
		`CREATE TABLE standard.metric_dependencies (id INTEGER PRIMARY KEY, from_metric_id INTEGER NOT NULL, to_metric_id INTEGER NOT NULL, coefficient REAL, note TEXT, created_at DATETIME)`,
		`INSERT INTO standard.metrics VALUES (1, 7, 'Revenue growth', 'revenue_growth', 'derived', 'approved', 2), (2, 7, 'Revenue', 'revenue', 'atomic', 'approved', NULL), (3, 7, 'Growth score', 'growth_score', 'composite', 'approved', NULL), (4, 8, 'Other tenant', 'other', 'atomic', 'approved', NULL)`,
		`INSERT INTO standard.metric_dependencies VALUES (11, 1, 2, 1.5, 'input', CURRENT_TIMESTAMP), (12, 3, 1, NULL, 'downstream', CURRENT_TIMESTAMP)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("execute %q: %v", statement, err)
		}
	}
	metricService := NewMetricService(nil, repository.NewMetricRepository(db), nil, nil)
	graph, err := metricService.GetProfessionalRelations(1, 7, 100)
	if err != nil {
		t.Fatal(err)
	}
	if graph.SchemaVersion != "addp.professional_relations/v1" || len(graph.Nodes) != 3 || len(graph.Edges) != 3 {
		t.Fatalf("metric graph = %#v", graph)
	}
	if graph.Edges[0].RelationKind != "standard.metric.base_metric" {
		t.Fatalf("first edge = %#v", graph.Edges[0])
	}
	if graph.Edges[1].RelationKind != "standard.metric.dependency" || graph.Edges[2].RelationKind != "standard.metric.dependency" {
		t.Fatalf("dependency edges = %#v", graph.Edges)
	}
	truncated, err := metricService.GetProfessionalRelations(1, 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated.Truncated || len(truncated.Edges) != 1 {
		t.Fatalf("truncated graph = %#v", truncated)
	}
}
