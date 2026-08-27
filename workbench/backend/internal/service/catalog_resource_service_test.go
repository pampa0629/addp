package service

import (
	"context"
	"strings"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/workbench/internal/models"
)

type fakeCatalogResourceRepository struct {
	changes      []models.CatalogResourceChangeRow
	applications []models.CatalogDataApplicationRecord
	versions     map[string]int64
}

func (r *fakeCatalogResourceRepository) ListChanges(context.Context, int64, int64, int) ([]models.CatalogResourceChangeRow, error) {
	return r.changes, nil
}

func (r *fakeCatalogResourceRepository) ListDataApplications(context.Context, int64, []string) ([]models.CatalogDataApplicationRecord, error) {
	return r.applications, nil
}

func (r *fakeCatalogResourceRepository) LatestChangeVersions(context.Context, int64, []string) (map[string]int64, error) {
	return r.versions, nil
}

func TestCatalogResourceServiceListsAndResolvesPublishedApplication(t *testing.T) {
	id := "d6c30859-15c8-4b88-964b-f2dd315fb923"
	now := time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC)
	repository := &fakeCatalogResourceRepository{
		changes: []models.CatalogResourceChangeRow{{
			ID: 42, TenantID: 7, SourceType: models.CatalogSourceTypeDataApplication,
			SourceIdentity: id, Operation: "upsert", Snapshot: commonModels.JSONMap{"name": "Application"}, ObservedAt: now,
		}},
		applications: []models.CatalogDataApplicationRecord{{
			ID: id, PublicationStatus: models.PublicationStatusPublished, CurrentRevisionNumber: 3,
			RevisionName: "Application", RevisionDescription: "Description", PublishedAt: now,
		}},
		versions: map[string]int64{id: 42},
	}
	service := NewCatalogResourceService(repository)

	changes, err := service.ListChanges(context.Background(), 7, "", 200)
	if err != nil || changes.SchemaVersion != models.CatalogResourceChangesSchemaVersion || len(changes.Changes) != 1 || changes.Changes[0].SourceVersion != "00000000000000000042" {
		t.Fatalf("ListChanges() result=%#v error=%v", changes, err)
	}
	resolved, err := service.Resolve(context.Background(), 7, []models.CatalogReference{{SourceType: models.CatalogSourceTypeDataApplication, SourceIdentity: id}})
	if err != nil || len(resolved.Results) != 1 || !resolved.Results[0].Found || resolved.Results[0].Version != 42 || resolved.Results[0].DetailPath != "/data-apps/"+id {
		t.Fatalf("Resolve() result=%#v error=%v", resolved, err)
	}
	if resolved.Results[0].Summary["revision_number"] != int64(3) {
		t.Fatalf("Resolve() summary=%#v", resolved.Results[0].Summary)
	}
}

func TestCatalogResourceServiceRejectsNonCanonicalApplicationIdentity(t *testing.T) {
	service := NewCatalogResourceService(&fakeCatalogResourceRepository{})
	for _, identity := range []string{"", "D6C30859-15C8-4B88-964B-F2DD315FB923", " d6c30859-15c8-4b88-964b-f2dd315fb923", "not-a-uuid"} {
		_, err := service.Resolve(context.Background(), 7, []models.CatalogReference{{SourceType: models.CatalogSourceTypeDataApplication, SourceIdentity: identity}})
		if err == nil || !strings.Contains(err.Error(), "invalid Workbench catalog resource request") {
			t.Fatalf("identity %q error=%v", identity, err)
		}
	}
}
