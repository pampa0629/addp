package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/google/uuid"
)

func TestCollectionServiceMaintainsVersionedProjectGroupAggregate(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	entries := NewEntryService(db, nil, nil)
	collections := NewCollectionService(db, entries)
	now := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	collections.now = func() time.Time { return now }
	access := CollectionAccess{UserID: 40, ReadGroupIDs: []int64{9}, UpdateGroupIDs: []int64{9}, EntryAccess: EntryAccess{Inventory: true}}

	created, err := collections.Create(context.Background(), 7, access, CollectionInput{
		ProjectGroupID: 9, Name: " Critical Data ", Description: " Shared curation ", EntryIDs: []uuid.UUID{entry.ID},
	})
	if err != nil || created.Version != 1 || created.Name != "Critical Data" || len(created.Entries) != 1 || created.Entries[0].ID != entry.ID {
		t.Fatalf("Create() = %#v, %v", created, err)
	}
	listed, err := collections.List(context.Background(), 7, access, CollectionListFilter{Page: 1, PageSize: 20})
	if err != nil || listed.Total != 1 || len(listed.Data) != 1 || listed.Data[0].ID != created.ID {
		t.Fatalf("List() = %#v, %v", listed, err)
	}
	if _, err := collections.Update(context.Background(), 7, access, created.ID, CollectionInput{
		Version: 2, Name: "Wrong version",
	}); !errors.Is(err, ErrCollectionVersionConflict) {
		t.Fatalf("stale Update() error = %v", err)
	}
	updated, err := collections.Update(context.Background(), 7, access, created.ID, CollectionInput{
		Version: 1, Name: "Critical Data v2", Description: "", EntryIDs: nil,
	})
	if err != nil || updated.Version != 2 || updated.Name != "Critical Data v2" || len(updated.Entries) != 0 {
		t.Fatalf("Update() = %#v, %v", updated, err)
	}
	if err := collections.Delete(context.Background(), 7, access, created.ID, 1); !errors.Is(err, ErrCollectionVersionConflict) {
		t.Fatalf("stale Delete() error = %v", err)
	}
	if err := collections.Delete(context.Background(), 7, access, created.ID, 2); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := collections.Get(context.Background(), 7, access, created.ID); !errors.Is(err, ErrCollectionNotFound) {
		t.Fatalf("Get() after delete error = %v", err)
	}
	var audits []models.CollectionAuditEvent
	if err := db.Where("collection_id = ?", created.ID).Order("created_at ASC, event_type ASC").Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) != 3 {
		t.Fatalf("collection audits = %#v", audits)
	}
}

func TestCollectionServiceRequiresGroupScopeAndEntryVisibility(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	entry, _ := createEditableCatalogEntry(t, db, 7)
	collections := NewCollectionService(db, NewEntryService(db, nil, nil))
	allowed := CollectionAccess{UserID: 40, ReadGroupIDs: []int64{9}, UpdateGroupIDs: []int64{9}, EntryAccess: EntryAccess{Inventory: true}}
	created, err := collections.Create(context.Background(), 7, allowed, CollectionInput{ProjectGroupID: 9, Name: "Shared", EntryIDs: []uuid.UUID{entry.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collections.Get(context.Background(), 7, CollectionAccess{UserID: 41, ReadGroupIDs: []int64{10}, EntryAccess: EntryAccess{Inventory: true}}, created.ID); !errors.Is(err, ErrCollectionNotFound) {
		t.Fatalf("foreign group Get() error = %v", err)
	}
	if _, err := collections.Create(context.Background(), 7, CollectionAccess{UserID: 40, UpdateGroupIDs: []int64{9}}, CollectionInput{
		ProjectGroupID: 9, Name: "Write only group", EntryIDs: []uuid.UUID{entry.ID},
	}); !errors.Is(err, ErrInvalidCollection) {
		t.Fatalf("write-only group Create() error = %v", err)
	}
	if _, err := collections.Create(context.Background(), 7, CollectionAccess{UserID: 40, ReadGroupIDs: []int64{9}, UpdateGroupIDs: []int64{9}}, CollectionInput{
		ProjectGroupID: 9, Name: "Hidden entry", EntryIDs: []uuid.UUID{entry.ID},
	}); !errors.Is(err, ErrEntryNotFound) {
		t.Fatalf("invisible entry Create() error = %v", err)
	}
	filtered, err := collections.List(context.Background(), 7, allowed, CollectionListFilter{ProjectGroupID: 10, Page: 1, PageSize: 20})
	if err != nil || filtered.Total != 0 || len(filtered.Data) != 0 {
		t.Fatalf("foreign group List() = %#v, %v", filtered, err)
	}
}

func TestCollectionServiceDynamicallyResolvesAccessibleProjectGroups(t *testing.T) {
	resolver := &fakeSystemReferenceResolver{}
	collections := NewCollectionService(nil, nil).WithSystemReferenceResolver(resolver)
	result, err := collections.ListProjectGroups(context.Background(), 7, []CollectionProjectGroupAccess{
		{ProjectGroupID: 9, RelationRole: "leader", CanRead: true, CanUpdate: true},
		{ProjectGroupID: 10, RelationRole: "member", CanRead: true},
	})
	if err != nil {
		t.Fatalf("ListProjectGroups() error = %v", err)
	}
	if resolver.calls != 1 || len(result.Data) != 2 || result.Data[0].ProjectGroupID != 9 || result.Data[0].Name != "project_group name" || !result.Data[0].CanUpdate || result.Data[1].CanUpdate {
		t.Fatalf("result = %#v, calls=%d", result, resolver.calls)
	}
}
