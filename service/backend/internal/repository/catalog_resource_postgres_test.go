package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/service/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCatalogQueryServiceChangeFeedAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("SERVICE_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("SERVICE_POSTGRES_TEST_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("DROP SCHEMA IF EXISTS service CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("CREATE SCHEMA service").Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(tx); err != nil {
		t.Fatal(err)
	}
	tenantID := uint(time.Now().UnixNano())
	queryService := models.QueryService{
		TenantID: tenantID, ServiceName: fmt.Sprintf("catalog_service_%d", tenantID), Title: "Catalog query service",
		ConfigType: "sql", SqlQuery: "SELECT 1", DataConfig: models.JSONB{}, Protocols: models.JSONB{},
		Status: "active", CreatedBy: 1,
	}
	if err := tx.Create(&queryService).Error; err != nil {
		t.Fatalf("create QueryService: %v", err)
	}
	repository := NewCatalogResourceRepository(tx)
	changes, err := repository.ListChanges(context.Background(), int64(tenantID), 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].SourceType != models.CatalogSourceTypeQueryService || changes[0].Operation != "upsert" || changes[0].Snapshot["service_status"] != "active" {
		t.Fatalf("initial changes = %#v", changes)
	}
	lastID := changes[0].ID
	if err := tx.Model(&queryService).Updates(map[string]any{"title": "Catalog query service current", "status": "inactive", "public_access": true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Delete(&queryService).Error; err != nil {
		t.Fatal(err)
	}
	changes, err = repository.ListChanges(context.Background(), int64(tenantID), lastID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].Operation != "upsert" || changes[0].Snapshot["name"] != "Catalog query service current" || changes[0].Snapshot["access_mode"] != "public" || changes[1].Operation != "missing" {
		t.Fatalf("lifecycle changes = %#v", changes)
	}
}
