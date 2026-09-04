package service

import (
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMetricProfessionalRelationsExposeRevisionDependencies(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:standard-professional-relations?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ATTACH DATABASE ':memory:' AS standard`,
		`CREATE TABLE standard.metric_definitions (id INTEGER PRIMARY KEY, tenant_id INTEGER NOT NULL, category_id INTEGER, scope_type TEXT NOT NULL, owner_domain_id INTEGER, code TEXT NOT NULL, steward_id INTEGER, tags TEXT, draft_revision_id INTEGER, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1, lifecycle_state TEXT NOT NULL DEFAULT 'active')`,
		`CREATE TABLE standard.metric_definition_revisions (id INTEGER PRIMARY KEY, metric_definition_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, metric_type TEXT NOT NULL, name TEXT NOT NULL, definition TEXT NOT NULL, statistical_caliber TEXT NOT NULL, semantic_formula TEXT, unit_id INTEGER, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE standard.metric_definition_revision_dependencies (id INTEGER PRIMARY KEY, metric_definition_revision_id INTEGER NOT NULL, dependency_definition_id INTEGER NOT NULL, dependency_revision_id INTEGER, relation_kind TEXT NOT NULL, coefficient REAL, note TEXT, created_at DATETIME)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("execute %q: %v", statement, err)
		}
	}
	for _, metric := range []models.MetricDefinition{
		{ID: 1, TenantID: 7, Code: "revenue_growth", ScopeType: "tenant_common", Tags: models.StringArray{}, CreatedBy: 1, LifecycleState: "active"},
		{ID: 2, TenantID: 7, Code: "revenue", ScopeType: "tenant_common", Tags: models.StringArray{}, CreatedBy: 1, LifecycleState: "active"},
		{ID: 3, TenantID: 7, Code: "growth_score", ScopeType: "tenant_common", Tags: models.StringArray{}, CreatedBy: 1, LifecycleState: "active"},
		{ID: 4, TenantID: 8, Code: "other", ScopeType: "tenant_common", Tags: models.StringArray{}, CreatedBy: 1, LifecycleState: "active"},
	} {
		if err := db.Create(&metric).Error; err != nil {
			t.Fatal(err)
		}
	}
	effectiveFrom := time.Now().UTC().Add(-time.Hour)
	for _, revision := range []models.MetricDefinitionRevision{
		{ID: 11, MetricDefinitionID: 1, RevisionNo: 1, Status: models.RevisionStatusPublished, MetricType: models.MetricTypeDerived, Name: "Revenue growth", Definition: "Growth", StatisticalCaliber: "Published", ChangeSummary: "Initial", EffectiveFrom: &effectiveFrom, CreatedBy: 1},
		{ID: 12, MetricDefinitionID: 2, RevisionNo: 1, Status: models.RevisionStatusPublished, MetricType: models.MetricTypeAtomic, Name: "Revenue", Definition: "Revenue", StatisticalCaliber: "Published", ChangeSummary: "Initial", EffectiveFrom: &effectiveFrom, CreatedBy: 1},
		{ID: 13, MetricDefinitionID: 3, RevisionNo: 1, Status: models.RevisionStatusPublished, MetricType: models.MetricTypeComposite, Name: "Growth score", Definition: "Score", StatisticalCaliber: "Published", ChangeSummary: "Initial", EffectiveFrom: &effectiveFrom, CreatedBy: 1},
		{ID: 14, MetricDefinitionID: 4, RevisionNo: 1, Status: models.RevisionStatusPublished, MetricType: models.MetricTypeAtomic, Name: "Other", Definition: "Other", StatisticalCaliber: "Published", ChangeSummary: "Initial", EffectiveFrom: &effectiveFrom, CreatedBy: 1},
	} {
		if err := db.Create(&revision).Error; err != nil {
			t.Fatal(err)
		}
	}
	coefficient := 1.5
	if err := db.Create(&models.MetricDefinitionRevisionDependency{ID: 21, MetricDefinitionRevisionID: 11, DependencyDefinitionID: 2, DependencyRevisionID: metricTestInt64Pointer(12), RelationKind: models.MetricDependencyBase, Coefficient: &coefficient, Note: "input"}).Error; err != nil {
		t.Fatal(err)
	}
	metricService := NewMetricService(nil, repository.NewMetricRepository(db), nil, nil)
	graph, err := metricService.GetProfessionalRelations(1, 7, 100)
	if err != nil {
		t.Fatal(err)
	}
	if graph.SchemaVersion != "addp.professional_relations/v1" || len(graph.Nodes) != 2 || len(graph.Edges) != 1 {
		t.Fatalf("metric graph = %#v", graph)
	}
	if graph.Edges[0].RelationKind != "standard.metric.base" {
		t.Fatalf("first edge = %#v", graph.Edges[0])
	}
	truncated, err := metricService.GetProfessionalRelations(1, 7, 1)
	if err != nil {
		t.Fatal(err)
	}
	if truncated.Truncated || len(truncated.Edges) != 1 {
		t.Fatalf("truncated graph = %#v", truncated)
	}
}

func metricTestInt64Pointer(value int64) *int64 { return &value }
