package service

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/catalog/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresRecommendedSuccessorUsesCatalogAggregateAndTenantBoundary(t *testing.T) {
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

	entry, _ := createEditableCatalogEntry(t, tx, 7)
	successor, _ := createEditableCatalogEntry(t, tx, 7)
	makeSuccessorEligible(t, tx, entry.ID)
	makeSuccessorEligible(t, tx, successor.ID)
	name, description, reason := "Legacy orders", "Legacy order facts", "Use the current order dataset"
	svc := NewEntryService(tx, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	result, err := svc.Update(context.Background(), 7, entry.ID, governedUpdateInput(
		1, name, description, models.GovernanceStatusDeprecated, &reason, &successor.ID,
	), UpdateEntryAuthorization{CanDeprecate: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if result.RecommendedSuccessorEntryID == nil || *result.RecommendedSuccessorEntryID != successor.ID || result.Version != 2 {
		t.Fatalf("result = %#v", result)
	}

	otherEntry, _ := createEditableCatalogEntry(t, tx, 7)
	crossTenantSuccessor, _ := createEditableCatalogEntry(t, tx, 8)
	makeSuccessorEligible(t, tx, otherEntry.ID)
	makeSuccessorEligible(t, tx, crossTenantSuccessor.ID)
	_, err = svc.Update(context.Background(), 7, otherEntry.ID, governedUpdateInput(
		1, name, description, models.GovernanceStatusDeprecated, &reason, &crossTenantSuccessor.ID,
	), UpdateEntryAuthorization{CanDeprecate: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if !errors.Is(err, ErrInvalidRecommendedSuccessor) {
		t.Fatalf("cross-tenant successor error = %v", err)
	}
	var reloaded models.Entry
	if err := tx.First(&reloaded, "tenant_id = ? AND id = ?", 7, otherEntry.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.Version != 1 || reloaded.GovernanceStatus != models.GovernanceStatusCurated || reloaded.RecommendedSuccessorEntryID != nil {
		t.Fatalf("cross-tenant rejection changed entry: %#v", reloaded)
	}
}
