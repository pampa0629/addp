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
	base := &models.Metric{
		TenantID: tenantID, Name: "PG Revenue", Code: "pg_revenue", Type: "atomic", Status: "approved",
		DerivationConfig: models.JSONB{}, Tags: models.StringArray{}, CreatedBy: 1, Version: 1, LifecycleState: "active",
	}
	subject := &models.Metric{
		TenantID: tenantID, Name: "PG Growth", Code: "pg_growth", Type: "derived", Status: "approved",
		DerivationConfig: models.JSONB{}, Tags: models.StringArray{}, CreatedBy: 1, Version: 1, LifecycleState: "active",
	}
	for _, metric := range []*models.Metric{base, subject} {
		if err := tx.Create(metric).Error; err != nil {
			t.Fatal(err)
		}
	}
	subject.BaseMetricID = &base.ID
	if err := tx.Model(subject).Update("base_metric_id", base.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Create(&models.MetricDependency{FromMetricID: subject.ID, ToMetricID: base.ID, Note: "PG dependency"}).Error; err != nil {
		t.Fatal(err)
	}
	graph, err := NewMetricService(nil, repository.NewMetricRepository(tx), nil, nil).
		GetProfessionalRelations(subject.ID, tenantID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) != 2 || len(graph.Edges) != 2 || graph.Edges[1].Target.ResourceID != graph.Nodes[1].ResourceID {
		t.Fatalf("metric graph = %#v", graph)
	}
}
