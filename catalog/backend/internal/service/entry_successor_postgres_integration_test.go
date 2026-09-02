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
	reason := "Use the current order dataset"
	svc := NewEntryService(tx, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	result, err := svc.UpdateGovernance(context.Background(), 7, entry.ID, UpdateEntryGovernanceInput{
		Version: 1, GovernanceStatus: models.GovernanceStatusDeprecated,
		Reason: &reason, RecommendedSuccessorEntryID: &successor.ID,
	}, UpdateEntryGovernanceAuthorization{CanDeprecate: true}, UpdateEntryActor{Type: "user", ID: "99"})
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
	_, err = svc.UpdateGovernance(context.Background(), 7, otherEntry.ID, UpdateEntryGovernanceInput{
		Version: 1, GovernanceStatus: models.GovernanceStatusDeprecated,
		Reason: &reason, RecommendedSuccessorEntryID: &crossTenantSuccessor.ID,
	}, UpdateEntryGovernanceAuthorization{CanDeprecate: true}, UpdateEntryActor{Type: "user", ID: "99"})
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

func TestPostgresEntryGovernanceCertificationLifecycle(t *testing.T) {
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

	entry, component := createEditableCatalogEntry(t, tx, 7)
	svc := NewEntryService(tx, &fakeStandardReferenceResolver{}, &fakeSystemReferenceResolver{})
	curated := curateCompleteEntry(t, svc, entry, component)
	certified, err := svc.UpdateGovernance(context.Background(), 7, entry.ID, UpdateEntryGovernanceInput{
		Version: curated.Version, GovernanceStatus: models.GovernanceStatusCertified,
	}, UpdateEntryGovernanceAuthorization{CanCertify: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatalf("certify: %v", err)
	}
	reason := "Business definition requires a governed revision"
	withdrawn, err := svc.UpdateGovernance(context.Background(), 7, entry.ID, UpdateEntryGovernanceInput{
		Version: certified.Version, GovernanceStatus: models.GovernanceStatusCurated, Reason: &reason,
	}, UpdateEntryGovernanceAuthorization{CanCertify: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatalf("withdraw certification: %v", err)
	}
	recertified, err := svc.UpdateGovernance(context.Background(), 7, entry.ID, UpdateEntryGovernanceInput{
		Version: withdrawn.Version, GovernanceStatus: models.GovernanceStatusCertified,
	}, UpdateEntryGovernanceAuthorization{CanCertify: true}, UpdateEntryActor{Type: "user", ID: "99"})
	if err != nil {
		t.Fatalf("recertify: %v", err)
	}
	if certified.Version != curated.Version+1 || withdrawn.Version != certified.Version+1 ||
		recertified.Version != withdrawn.Version+1 || recertified.GovernanceStatus != models.GovernanceStatusCertified {
		t.Fatalf("lifecycle versions/status = curated:%d certified:%d withdrawn:%d recertified:%d/%s",
			curated.Version, certified.Version, withdrawn.Version, recertified.Version, recertified.GovernanceStatus)
	}
	assertAuditEvent(t, tx, entry.ID, "catalog.entry.certification_withdrawn")
	var certificationCount int64
	if err := tx.Model(&models.AuditEvent{}).Where(
		"catalog_entry_id = ? AND event_type = ?", entry.ID, "catalog.entry.certified",
	).Count(&certificationCount).Error; err != nil {
		t.Fatal(err)
	}
	if certificationCount != 2 {
		t.Fatalf("certification audit count = %d", certificationCount)
	}
}
