package repository

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/model/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresUniqueViolationTranslatesToConflict(t *testing.T) {
	tx := beginPostgresIntegrationTransaction(t)

	repo := NewEntityRepository(tx)
	tenantID := time.Now().UnixNano()
	code := fmt.Sprintf("conflict_%d", tenantID)
	first := &models.Entity{TenantID: tenantID, Name: "Conflict Test", Code: code, Status: "draft", CreatedBy: 1}
	second := &models.Entity{TenantID: tenantID, Name: "Conflict Test Duplicate", Code: code, Status: "draft", CreatedBy: 1}
	if err := repo.Create(first); err != nil {
		t.Fatalf("create first entity: %v", err)
	}
	if err := repo.Create(second); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("duplicate entity error = %v, want common conflict", err)
	}
}

func TestPostgresForeignKeyViolationTranslatesToConflict(t *testing.T) {
	tx := beginPostgresIntegrationTransaction(t)
	repo := NewEntityRepository(tx)
	attr := &models.EntityAttribute{
		EntityID:   time.Now().UnixNano(),
		Name:       "Missing Entity",
		ColumnName: "missing_entity",
		DataType:   "string",
	}
	if err := repo.CreateAttribute(attr); !errors.Is(err, commonapi.ErrConflict) {
		t.Fatalf("foreign key error = %v, want common conflict", err)
	}
}

func beginPostgresIntegrationTransaction(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_MODEL_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	t.Cleanup(func() { _ = tx.Rollback().Error })
	return tx
}
