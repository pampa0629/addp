package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
)

type catalogResourceRepositoryStub struct {
	changes []models.CatalogResourceChangeRow
	metrics []models.MetricDefinitionAggregate
}

func (s *catalogResourceRepositoryStub) ListChanges(context.Context, int64, int64, int) ([]models.CatalogResourceChangeRow, error) {
	return s.changes, nil
}

func (s *catalogResourceRepositoryStub) ListMetrics(context.Context, int64, []int64) ([]models.MetricDefinitionAggregate, error) {
	return s.metrics, nil
}

func TestCatalogResourceServiceListsOpaqueVersionedMetricChanges(t *testing.T) {
	now := time.Now().UTC()
	service := NewCatalogResourceService(&catalogResourceRepositoryStub{changes: []models.CatalogResourceChangeRow{{
		ID: 42, TenantID: 7, SourceType: models.CatalogSourceTypeMetric, SourceIdentity: 9,
		Operation: "upsert", ResourceVersion: 3, Snapshot: models.JSONB{"name": "Order amount"}, ObservedAt: now,
	}}})
	result, err := service.ListChanges(context.Background(), 7, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != models.CatalogResourceChangesSchemaVersion || result.NextCursor != "NDI" ||
		len(result.Changes) != 1 || result.Changes[0].SourceVersion != "00000000000000000042" || result.Changes[0].SourceIdentity != "9" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCatalogResourceServiceResolvesMetricsInRequestOrder(t *testing.T) {
	domainID, categoryID, unitID := int64(31), int64(41), int64(51)
	service := NewCatalogResourceService(&catalogResourceRepositoryStub{metrics: []models.MetricDefinitionAggregate{{
		MetricDefinition: models.MetricDefinition{ID: 9, TenantID: 7, Code: "order_amount", LifecycleState: "active", Version: 4, OwnerDomainID: &domainID, CategoryID: &categoryID},
		CurrentRevision:  &models.MetricDefinitionRevision{ID: 19, MetricDefinitionID: 9, RevisionNo: 1, Name: "Order amount", MetricType: "atomic", Status: models.RevisionStatusPublished, UnitID: &unitID},
	}}})
	result, err := service.Resolve(context.Background(), 7, []models.CatalogReference{
		{SourceType: "metric", SourceIdentity: "9"}, {SourceType: "metric", SourceIdentity: "10"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 2 || !result.Results[0].Found || result.Results[1].Found ||
		result.Results[0].Summary["domain_id"] != "31" || result.Results[0].DetailPath != "/standard/metrics/9" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCatalogResourceServiceRejectsNonCanonicalMetricIdentity(t *testing.T) {
	service := NewCatalogResourceService(&catalogResourceRepositoryStub{})
	for _, identity := range []string{"0", "01", " 1", "-1", "metric-1"} {
		if _, err := service.Resolve(context.Background(), 7, []models.CatalogReference{{SourceType: "metric", SourceIdentity: identity}}); err == nil {
			t.Fatalf("identity %q accepted", identity)
		}
	}
}
