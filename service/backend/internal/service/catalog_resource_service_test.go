package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/service/internal/models"
)

type catalogResourceRepositoryStub struct {
	changes       []models.CatalogResourceChangeRow
	queryServices []models.QueryService
	versions      map[int64]int64
}

func (s *catalogResourceRepositoryStub) ListChanges(context.Context, int64, int64, int) ([]models.CatalogResourceChangeRow, error) {
	return s.changes, nil
}

func (s *catalogResourceRepositoryStub) ListQueryServices(context.Context, int64, []int64) ([]models.QueryService, error) {
	return s.queryServices, nil
}

func (s *catalogResourceRepositoryStub) LatestChangeVersions(context.Context, int64, []int64) (map[int64]int64, error) {
	return s.versions, nil
}

func TestCatalogResourceServiceListsOpaqueVersionedQueryServiceChanges(t *testing.T) {
	now := time.Now().UTC()
	svc := NewCatalogResourceService(&catalogResourceRepositoryStub{changes: []models.CatalogResourceChangeRow{{
		ID: 42, TenantID: 7, SourceType: models.CatalogSourceTypeQueryService, SourceIdentity: 9,
		Operation: "upsert", Snapshot: models.JSONB{"name": "Orders"}, ObservedAt: now,
	}}})
	result, err := svc.ListChanges(context.Background(), 7, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != models.CatalogResourceChangesSchemaVersion || result.NextCursor != "NDI" ||
		len(result.Changes) != 1 || result.Changes[0].SourceVersion != "00000000000000000042" || result.Changes[0].SourceIdentity != "9" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCatalogResourceServiceResolvesQueryServicesInRequestOrder(t *testing.T) {
	engineID, runtimeID := uint(31), uint(41)
	svc := NewCatalogResourceService(&catalogResourceRepositoryStub{
		queryServices: []models.QueryService{{ID: 9, TenantID: 7, Title: "Orders", ServiceName: "orders", ConfigType: "sql", Status: "active", PublicAccess: true, EngineID: &engineID, RuntimeEngineID: &runtimeID}},
		versions:      map[int64]int64{9: 42},
	})
	result, err := svc.Resolve(context.Background(), 7, []models.CatalogReference{
		{SourceType: "query_service", SourceIdentity: "9"}, {SourceType: "query_service", SourceIdentity: "10"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 2 || !result.Results[0].Found || result.Results[1].Found ||
		result.Results[0].Version != 42 || result.Results[0].Summary["access_mode"] != "public" ||
		result.Results[0].Summary["engine_id"] != "31" || result.Results[0].DetailPath != "/service/published-services/9" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCatalogResourceServiceRejectsNonCanonicalQueryServiceIdentity(t *testing.T) {
	svc := NewCatalogResourceService(&catalogResourceRepositoryStub{})
	for _, identity := range []string{"0", "01", " 1", "-1", "service-1"} {
		if _, err := svc.Resolve(context.Background(), 7, []models.CatalogReference{{SourceType: "query_service", SourceIdentity: identity}}); err == nil {
			t.Fatalf("identity %q accepted", identity)
		}
	}
}

func TestCatalogResourceServiceRequiresChangeVersionForFoundQueryService(t *testing.T) {
	svc := NewCatalogResourceService(&catalogResourceRepositoryStub{
		queryServices: []models.QueryService{{ID: 9, TenantID: 7, Title: "Orders", ServiceName: "orders", ConfigType: "sql", Status: "active"}},
		versions:      map[int64]int64{},
	})
	if _, err := svc.Resolve(context.Background(), 7, []models.CatalogReference{{SourceType: "query_service", SourceIdentity: "9"}}); err == nil {
		t.Fatal("found QueryService without a change version was accepted")
	}
}
