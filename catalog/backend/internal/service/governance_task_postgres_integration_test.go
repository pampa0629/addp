package service

import (
	"os"
	"testing"

	"github.com/addp/catalog/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresResponsibilityRecoveryUsesEntryVisibility(t *testing.T) {
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
	assertResponsibilityRecoveryUsesCurrentEntryVisibility(t, tx)
	assertDeprecatedResponsibilityTransferAndWithdrawal(t, tx)
	assertBusinessResponsibilityEstablishment(t, tx)
}
