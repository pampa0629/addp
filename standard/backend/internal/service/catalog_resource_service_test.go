package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/standard/internal/models"
)

type catalogResourceRepositoryStub struct {
	changes []models.CatalogResourceChangeRow
	metrics []models.Metric
}

func (s *catalogResourceRepositoryStub) ListChanges(context.Context, int64, int64, int) ([]models.CatalogResourceChangeRow, error) {
	return s.changes, nil
}

func (s *catalogResourceRepositoryStub) ListMetrics(context.Context, int64, []int64) ([]models.Metric, error) {
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
	service := NewCatalogResourceService(&catalogResourceRepositoryStub{metrics: []models.Metric{{
		ID: 9, TenantID: 7, Name: "Order amount", Code: "order_amount", Type: "atomic",
		Status: "approved", LifecycleState: "active", Version: 4, DomainID: &domainID, CategoryID: &categoryID, UnitID: &unitID,
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
