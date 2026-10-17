package service

import (
	"os"
	"testing"

	commonexecution "github.com/addp/common/execution"
	"github.com/addp/security/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestProtectionAccessScopesAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SECURITY_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("SECURITY_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("DROP SCHEMA IF EXISTS security CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.Migrate(tx); err != nil {
		t.Fatal(err)
	}
	if err := commonexecution.EnsureStore(tx); err != nil {
		t.Fatal(err)
	}
	assertOneApprovalPublishesExplicitOutlets(t, tx)
}
