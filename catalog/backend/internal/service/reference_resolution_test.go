package service

import (
	"context"
	"testing"

	"github.com/addp/catalog/internal/models"
	"github.com/google/uuid"
)

func TestResolveReferencesPreservesOrderAndDoesNotLeakOtherTenants(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	curated, _ := createEditableCatalogEntry(t, db, 7)
	if err := db.Model(&models.Entry{}).Where("id = ?", curated.ID).
		Updates(map[string]any{"business_name": "Customer Orders", "governance_status": models.GovernanceStatusCurated}).Error; err != nil {
		t.Fatal(err)
	}
	discovered, _ := createEditableCatalogEntry(t, db, 7)
	missing, _ := createEditableCatalogEntry(t, db, 7)
	if err := db.Model(&models.SourceBinding{}).Where("catalog_entry_id = ?", missing.ID).
		Update("source_status", models.SourceStatusMissing).Error; err != nil {
		t.Fatal(err)
	}
	otherTenant, _ := createEditableCatalogEntry(t, db, 8)
	unknown := uuid.New()

	results, err := NewEntryService(db, nil, nil).ResolveReferences(
		context.Background(), 7, []uuid.UUID{missing.ID, curated.ID, otherTenant.ID, unknown, discovered.ID},
	)
	if err != nil {
		t.Fatalf("ResolveReferences() error = %v", err)
	}
	if len(results) != 5 || results[0].ID != missing.ID || results[1].ID != curated.ID || results[4].ID != discovered.ID {
		t.Fatalf("results out of order: %#v", results)
	}
	if !results[0].Found || results[0].Selectable || results[0].Publishable {
		t.Fatalf("missing resolution = %#v", results[0])
	}
	if !results[1].Selectable || !results[1].Publishable || results[1].DisplayName != "Customer Orders" {
		t.Fatalf("curated resolution = %#v", results[1])
	}
	if results[2].Found || results[3].Found {
		t.Fatalf("cross-tenant/unknown leaked: %#v %#v", results[2], results[3])
	}
	if !results[4].Selectable || results[4].Publishable {
		t.Fatalf("discovered resolution = %#v", results[4])
	}
}

func TestResolveReferencesRejectsDuplicatesAndEmptyRequests(t *testing.T) {
	db := openCatalogServiceTestDB(t)
	id := uuid.New()
	for _, ids := range [][]uuid.UUID{nil, {}, {id, id}, {uuid.Nil}} {
		if _, err := NewEntryService(db, nil, nil).ResolveReferences(context.Background(), 7, ids); err == nil {
			t.Fatalf("ids %#v accepted", ids)
		}
	}
}
