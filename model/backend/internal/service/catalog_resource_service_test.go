package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

type catalogResourceRepositoryStub struct {
	changes       []repository.CatalogResourceChangeRow
	entities      []models.Entity
	logicalTables []models.LogicalTable
}

func (s *catalogResourceRepositoryStub) ListChanges(context.Context, int64, int64, int) ([]repository.CatalogResourceChangeRow, error) {
	return s.changes, nil
}
func (s *catalogResourceRepositoryStub) ListEntities(context.Context, int64, []int64) ([]models.Entity, error) {
	return s.entities, nil
}
func (s *catalogResourceRepositoryStub) ListLogicalTables(context.Context, int64, []int64) ([]models.LogicalTable, error) {
	return s.logicalTables, nil
}

func TestCatalogResourceServiceListsOpaqueVersionedChanges(t *testing.T) {
	service := NewCatalogResourceService(&catalogResourceRepositoryStub{changes: []repository.CatalogResourceChangeRow{
		{ID: 42, SourceType: models.CatalogSourceTypeEntity, SourceIdentity: 9, Operation: "upsert", Snapshot: models.JSONB{"name": "Order"}, ObservedAt: time.Now().UTC()},
	}})
	result, err := service.ListChanges(context.Background(), 7, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changes) != 1 || result.Changes[0].SourceIdentity != "9" || result.Changes[0].SourceVersion != "00000000000000000042" || result.NextCursor == "" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCatalogResourceServiceResolvesInRequestOrder(t *testing.T) {
	domainID, entityID := int64(30), int64(8)
	service := NewCatalogResourceService(&catalogResourceRepositoryStub{
		entities:      []models.Entity{{ID: 8, TenantID: 7, Name: "Order", Code: "order", Status: "draft", Version: 2, DomainID: &domainID}},
		logicalTables: []models.LogicalTable{{ID: 12, TenantID: 7, Name: "Orders", Code: "fact_orders", Status: "approved", Version: 3, TableType: "fact", Layer: "dwd", EntityID: &entityID}},
	})
	result, err := service.Resolve(context.Background(), 7, []models.CatalogReference{
		{SourceType: "logical_table", SourceIdentity: "12"},
		{SourceType: "entity", SourceIdentity: "99"},
		{SourceType: "entity", SourceIdentity: "8"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 3 || !result.Results[0].Found || result.Results[1].Found || !result.Results[2].Found || result.Results[2].Summary["domain_id"] != "30" {
		t.Fatalf("results = %#v", result.Results)
	}
}

func TestCatalogResourceServiceRejectsNonCanonicalIdentity(t *testing.T) {
	service := NewCatalogResourceService(&catalogResourceRepositoryStub{})
	for _, identity := range []string{"", "0", "01", "+1", "-1", " 1"} {
		if _, err := service.Resolve(context.Background(), 7, []models.CatalogReference{{SourceType: "entity", SourceIdentity: identity}}); err == nil {
			t.Fatalf("identity %q accepted", identity)
		}
	}
}
