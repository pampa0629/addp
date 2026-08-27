package service

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/addp/catalog/internal/models"
	"github.com/addp/catalog/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresBatchGovernanceUsesPerEntryVersionsAndRollsBackAtomically(t *testing.T) {
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

	first, _ := createEditableCatalogEntry(t, tx, 73)
	second, _ := createEditableCatalogEntry(t, tx, 73)
	svc := NewEntryService(tx, nil, &fakeSystemReferenceResolver{})
	result, err := svc.BatchGovernance(context.Background(), 73, BatchGovernanceInput{
		Entries:   []BatchGovernanceEntryInput{{ID: second.ID, Version: 1}, {ID: first.ID, Version: 1}},
		Operation: BatchGovernanceAssignAccountableDepartment, ReferenceID: 81,
	}, UpdateEntryActor{Type: "user", ID: "91"})
	if err != nil {
		t.Fatalf("BatchGovernance() error = %v", err)
	}
	if len(result.Entries) != 2 || result.Entries[0].ID != second.ID || result.Entries[0].Version != 2 {
		t.Fatalf("result = %#v", result)
	}

	third, _ := createEditableCatalogEntry(t, tx, 73)
	fourth, _ := createEditableCatalogEntry(t, tx, 73)
	_, err = svc.BatchGovernance(context.Background(), 73, BatchGovernanceInput{
		Entries:   []BatchGovernanceEntryInput{{ID: third.ID, Version: 1}, {ID: fourth.ID, Version: 2}},
		Operation: BatchGovernanceAssignAccountableDepartment, ReferenceID: 82,
	}, UpdateEntryActor{Type: "user", ID: "91"})
	if !errors.Is(err, ErrEntryVersionConflict) {
		t.Fatalf("conflicting BatchGovernance() error = %v", err)
	}
	var changedEntries int64
	if err := tx.Model(&models.Entry{}).Where("id IN ? AND version <> 1", []uuid.UUID{third.ID, fourth.ID}).Count(&changedEntries).Error; err != nil {
		t.Fatal(err)
	}
	var responsibilities int64
	if err := tx.Model(&models.Responsibility{}).Where("catalog_entry_id IN ?", []uuid.UUID{third.ID, fourth.ID}).Count(&responsibilities).Error; err != nil {
		t.Fatal(err)
	}
	if changedEntries != 0 || responsibilities != 0 {
		t.Fatalf("conflicting batch changed entries/responsibilities = %d/%d", changedEntries, responsibilities)
	}
}
