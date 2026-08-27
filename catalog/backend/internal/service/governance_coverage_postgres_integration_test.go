package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/catalog/internal/repository"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresGovernanceCoverageAndSourceResolution(t *testing.T) {
	dsn := os.Getenv("CATALOG_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("CATALOG_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	defer tx.Rollback()
	if err := repository.Migrate(tx); err != nil {
		t.Fatalf("migrate Catalog schema: %v", err)
	}

	metaEntry, component := createEditableCatalogEntry(t, tx, 71)
	modelEntry := createModelCatalogEntry(t, tx, 71, "31")
	now := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	if err := tx.Model(&models.Entry{}).Where("id = ?", metaEntry.ID).Updates(map[string]any{
		"business_name": "Orders", "business_description": "Enterprise order facts",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Create(&models.ComponentElementAssociation{
		ID: uuid.New(), TenantID: 71, CatalogEntryID: metaEntry.ID, ComponentID: component.ID,
		ElementID: 51, ObservedVersion: 1, ObservedSnapshot: commonModels.JSONMap{"name": "Order ID"},
		VerifiedAt: now, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	svc := NewEntryService(tx, nil, nil)
	coverage, err := svc.GetGovernanceCoverage(context.Background(), 71, EntryAccess{Inventory: true})
	if err != nil {
		t.Fatalf("GetGovernanceCoverage() error = %v", err)
	}
	if coverage.TotalEntries != 2 || coverage.Dimensions[1].Key != CoverageDimensionPrimaryDomain || coverage.Dimensions[1].Covered != 1 {
		t.Fatalf("coverage = %#v", coverage)
	}
	resolved, err := svc.ResolveSourceEntries(context.Background(), 71, EntryAccess{Inventory: true}, []CatalogSourceReference{
		{SourceModule: models.SourceModuleModel, SourceType: models.SourceTypeLogicalTable, SourceIdentity: "12"},
	})
	if err != nil {
		t.Fatalf("ResolveSourceEntries() error = %v", err)
	}
	if len(resolved.Results) != 1 || !resolved.Results[0].Found || resolved.Results[0].Entry == nil || resolved.Results[0].Entry.ID != modelEntry.ID {
		t.Fatalf("source resolution = %#v", resolved)
	}
}
