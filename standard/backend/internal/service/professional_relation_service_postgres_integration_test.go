package service

import (
	"os"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresMetricProfessionalRelationsUseOwnerSchemaAndTenantBoundary(t *testing.T) {
	dsn := os.Getenv("STANDARD_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("STANDARD_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Migrate(db); err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	tenantID := time.Now().UnixNano()
	base := &models.MetricDefinition{
		TenantID: tenantID, Code: "pg_revenue", ScopeType: "tenant_common",
		Tags: models.StringArray{}, CreatedBy: 1, Version: 1, LifecycleState: "active",
	}
	subject := &models.MetricDefinition{
		TenantID: tenantID, Code: "pg_growth", ScopeType: "tenant_common",
		Tags: models.StringArray{}, CreatedBy: 1, Version: 1, LifecycleState: "active",
	}
	metricRepo := repository.NewMetricRepository(tx)
	baseRevision := &models.MetricDefinitionRevision{MetricType: models.MetricTypeAtomic, Name: "PG Revenue", Definition: "Revenue", StatisticalCaliber: "Published", ChangeSummary: "Initial", CreatedBy: 1}
	subjectRevision := &models.MetricDefinitionRevision{MetricType: models.MetricTypeDerived, Name: "PG Growth", Definition: "Growth", StatisticalCaliber: "Published", ChangeSummary: "Initial", CreatedBy: 1}
	if err := metricRepo.Create(base, baseRevision, nil); err != nil {
		t.Fatal(err)
	}
	if err := metricRepo.Create(subject, subjectRevision, []models.MetricDefinitionRevisionDependency{{DependencyDefinitionID: base.ID, RelationKind: models.MetricDependencyBase, Note: "PG dependency"}}); err != nil {
		t.Fatal(err)
	}
	graph, err := NewMetricService(nil, metricRepo, nil, nil).
		GetProfessionalRelations(subject.ID, tenantID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 2 || len(graph.Edges) != 1 || graph.Edges[0].Target.ResourceID != graph.Nodes[1].ResourceID {
		t.Fatalf("metric graph = %#v", graph)
	}
}
