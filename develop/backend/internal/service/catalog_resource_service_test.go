package service

import (
	"context"
	"testing"
	"time"

	"github.com/addp/develop/backend/internal/models"
)

type catalogResourceRepositoryStub struct {
	changes  []models.CatalogResourceChangeRow
	tasks    []models.DevTask
	versions map[int64]int64
}

func (s *catalogResourceRepositoryStub) ListChanges(context.Context, int64, int64, int) ([]models.CatalogResourceChangeRow, error) {
	return s.changes, nil
}

func (s *catalogResourceRepositoryStub) ListReusableDevTasks(context.Context, int64, []int64) ([]models.DevTask, error) {
	return s.tasks, nil
}

func (s *catalogResourceRepositoryStub) LatestChangeVersions(context.Context, int64, []int64) (map[int64]int64, error) {
	return s.versions, nil
}

func TestCatalogResourceServiceListsOpaqueDevTaskChanges(t *testing.T) {
	now := time.Now().UTC()
	svc := NewCatalogResourceService(&catalogResourceRepositoryStub{changes: []models.CatalogResourceChangeRow{{
		ID: 42, TenantID: 7, SourceType: models.CatalogSourceTypeDevTask, SourceIdentity: 9,
		Operation: "upsert", Snapshot: map[string]any{"name": "Orders workflow"}, ObservedAt: now,
	}}})
	result, err := svc.ListChanges(context.Background(), 7, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	if result.SchemaVersion != models.CatalogResourceChangesSchemaVersion || result.NextCursor != "NDI" ||
		len(result.Changes) != 1 || result.Changes[0].SourceVersion != "00000000000000000042" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCatalogResourceServiceResolvesReusableTasksOnly(t *testing.T) {
	svc := NewCatalogResourceService(&catalogResourceRepositoryStub{
		tasks: []models.DevTask{{ID: 9, TenantID: 7, Name: "orders", DisplayName: "Orders query", DevType: "query", Status: "active",
			Content: models.DevTaskContent{"query_type": "sql"}, ExecutionConfig: models.DevTaskContent{"engine_id": float64(31)}}},
		versions: map[int64]int64{9: 42},
	})
	result, err := svc.Resolve(context.Background(), 7, []models.CatalogReference{
		{SourceType: "dev_task", SourceIdentity: "9"}, {SourceType: "dev_task", SourceIdentity: "10"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 2 || !result.Results[0].Found || result.Results[1].Found || result.Results[0].Version != 42 ||
		result.Results[0].Summary["artifact_type"] != "query" || result.Results[0].Summary["engine_id"] != "31" ||
		result.Results[0].DetailPath != "/develop/sql?action=edit&id=9" {
		t.Fatalf("result = %#v", result)
	}
}

func TestCatalogResourceServiceRejectsNonCanonicalDevTaskIdentity(t *testing.T) {
	svc := NewCatalogResourceService(&catalogResourceRepositoryStub{})
	for _, identity := range []string{"0", "01", " 1", "-1", "task-1"} {
		if _, err := svc.Resolve(context.Background(), 7, []models.CatalogReference{{SourceType: "dev_task", SourceIdentity: identity}}); err == nil {
			t.Fatalf("identity %q accepted", identity)
		}
	}
}
