package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type fakeCatalogSearchResolver struct {
	ids    []uuid.UUID
	total  int64
	filter EntryListFilter
}

func (f *fakeCatalogSearchResolver) SearchCatalogEntries(_ context.Context, _ int64, _ EntryAccess, filter EntryListFilter) ([]uuid.UUID, int64, error) {
	f.filter = filter
	return f.ids, f.total, nil
}

func TestEntryListUsesSearchProjectionOrderAndStillReadsAuthoritativeFacts(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	first, _ := createEditableCatalogEntry(t, db, 7)
	second, _ := createEditableCatalogEntry(t, db, 7)
	search := &fakeCatalogSearchResolver{ids: []uuid.UUID{second.ID, first.ID}, total: 2}
	result, err := NewEntryService(db, nil, nil).WithSearch(search).List(context.Background(), 7, EntryAccess{Inventory: true}, EntryListFilter{
		View: EntryViewInventory, Search: "orders", GovernanceStatus: "discovered", Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(result.Data) != 2 || result.Data[0].ID != second.ID || result.Data[1].ID != first.ID || result.Total != 2 {
		t.Fatalf("result = %#v", result)
	}
	if search.filter.Search != "orders" || search.filter.GovernanceStatus != "discovered" {
		t.Fatalf("search filter = %#v", search.filter)
	}
}

func TestEntryListDoesNotFallbackToDatabaseLikeSearch(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	createEditableCatalogEntry(t, db, 7)
	_, err := NewEntryService(db, nil, nil).List(context.Background(), 7, EntryAccess{Inventory: true}, EntryListFilter{
		View: EntryViewInventory, Search: "orders", Page: 1, PageSize: 20,
	})
	if !errors.Is(err, ErrSearchUnavailable) {
		t.Fatalf("error = %v, want search unavailable", err)
	}
}
