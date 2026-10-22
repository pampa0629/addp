package service

import (
	"context"
	"github.com/addp/catalog/internal/models"
	"github.com/addp/catalog/internal/repository"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"testing"
	"time"
)

func TestPostgresDomainOverviewUsesExactViewAndApprovedReferenceEvidence(t *testing.T) {
	dsn := os.Getenv("CATALOG_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("CATALOG_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	defer tx.Rollback()
	if err := repository.Migrate(tx); err != nil {
		t.Fatal(err)
	}
	tenant := time.Now().UnixNano()
	svc := NewEntryService(tx, nil, nil)
	for index, kind := range []string{"metric", "element", "glossary", "code_set", "document"} {
		change := ProfessionalResourceChange{SourceType: kind, SourceIdentity: "9007199254740993", SourceVersion: "00000000000000000001", Operation: "upsert", ObservedAt: time.Now().UTC(), Snapshot: commonModels.JSONMap{"name": "Standard", "scope_type": "domain", "domain_id": "31", "definition_complete": true}}
		if err := applyProfessionalResourceChange(tx, tenant, "standard", change, map[string]*models.SourceBinding{}); err != nil {
			t.Fatal(err)
		}
		var source models.SourceBinding
		if err := tx.Where("tenant_id=? AND source_type=?", tenant, kind).First(&source).Error; err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			if err := tx.Model(&models.Entry{}).Where("id=?", source.CatalogEntryID).Updates(map[string]any{"governance_status": "curated", "visibility": "tenant"}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	inventory, err := svc.GetDomainResourceOverview(context.Background(), tenant, EntryAccess{Inventory: true}, 31, "inventory")
	if err != nil || inventory.TotalEntries != 5 || len(inventory.EntryTypes) != 5 {
		t.Fatalf("inventory=%+v err=%v", inventory, err)
	}
	for _, dimension := range inventory.Coverage.Dimensions {
		if dimension.Key == "glossary" && dimension.Applicable != 0 {
			t.Fatal("standards require self-association")
		}
	}
	governance, err := svc.GetDomainResourceOverview(context.Background(), tenant, EntryAccess{}, 31, "governance")
	if err != nil || governance.TotalEntries != 1 || governance.Coverage != nil {
		t.Fatalf("governance=%+v err=%v", governance, err)
	}
	if _, err := svc.GetDomainResourceOverview(context.Background(), tenant, EntryAccess{}, 31, "inventory"); err != ErrInventoryPermissionRequired {
		t.Fatalf("inventory permission=%v", err)
	}
	empty, err := svc.GetDomainResourceOverview(context.Background(), tenant, EntryAccess{Inventory: true}, 32, "inventory")
	if err != nil || empty.TotalEntries != 0 {
		t.Fatalf("domain filter=%+v err=%v", empty, err)
	}
	entry, component := createEditableCatalogEntry(t, tx, tenant)
	now := time.Now().UTC()
	association := models.SemanticAssociation{ID: uuid.New(), TenantID: tenant, CatalogEntryID: entry.ID, SemanticType: "domain", SemanticID: 31, RelationRole: "primary", ObservedVersion: 1, ObservedSnapshot: commonModels.JSONMap{}, VerifiedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := tx.Create(&association).Error; err != nil {
		t.Fatal(err)
	}
	revision := int64(123)
	mapping := models.StandardMapping{ID: uuid.New(), TenantID: tenant, CatalogEntryID: entry.ID, ComponentID: component.ID, ElementID: 9007199254740993, ElementRevisionID: &revision, Source: "manual", ReviewStatus: "proposed", Version: 1, ProposedByType: "user", ProposedByID: "1", Evidence: commonModels.JSONMap{}}
	if err := tx.Create(&mapping).Error; err != nil {
		t.Fatal(err)
	}
	page, err := svc.ListDomainStandardReferences(context.Background(), tenant, EntryAccess{Inventory: true}, 31, "inventory", 1, 5)
	if err != nil || page.Total != 0 {
		t.Fatalf("proposed mapping counted=%+v err=%v", page, err)
	}
	if err := tx.Model(&mapping).Update("review_status", "approved").Error; err != nil {
		t.Fatal(err)
	}
	page, err = svc.ListDomainStandardReferences(context.Background(), tenant, EntryAccess{Inventory: true}, 31, "inventory", 1, 5)
	if err != nil || page.Total != 1 || page.Data[0].Evidence[0].RevisionID != "123" {
		t.Fatalf("approved mapping=%+v err=%v", page, err)
	}
}
