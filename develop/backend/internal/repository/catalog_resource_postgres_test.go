package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/develop/backend/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCatalogDevTaskChangeFeedAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("DEVELOP_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("DEVELOP_POSTGRES_TEST_DSN is not set")
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
	if err := tx.Exec("DROP SCHEMA IF EXISTS develop CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("CREATE SCHEMA develop").Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.DevTask{}, &models.CatalogResourceChangeRow{}); err != nil {
		t.Fatal(err)
	}
	if err := normalizeDevTaskContent(tx); err != nil {
		t.Fatal(err)
	}
	if err := migrateCatalogDevTaskChanges(tx); err != nil {
		t.Fatal(err)
	}
	tenantID := uint(time.Now().UnixNano())
	queryTask := models.DevTask{TenantID: tenantID, Name: fmt.Sprintf("catalog_query_%d", tenantID), DisplayName: "Catalog query",
		DevType: "query", Content: models.DevTaskContent{"query_type": "sql", "query": "SELECT 1"},
		ExecutionConfig: models.DevTaskContent{"engine_id": 31}, EditorLayout: map[string]any{}, Status: "active"}
	if err := tx.Create(&queryTask).Error; err != nil {
		t.Fatal(err)
	}
	scriptTask := models.DevTask{TenantID: tenantID, Name: fmt.Sprintf("catalog_script_%d", tenantID), DevType: "script",
		Content: models.DevTaskContent{"notebook_path": "notebooks/a.ipynb"}, EditorLayout: map[string]any{}, Status: "active"}
	if err := tx.Create(&scriptTask).Error; err != nil {
		t.Fatal(err)
	}
	repository := NewCatalogResourceRepository(tx)
	changes, err := repository.ListChanges(context.Background(), int64(tenantID), 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Snapshot["artifact_type"] != "query" || changes[0].Snapshot["engine_id"] != "31" {
		t.Fatalf("initial changes = %#v", changes)
	}
	lastID := changes[0].ID
	if err := tx.Model(&queryTask).Updates(map[string]any{"display_name": "Catalog query current", "status": "archived"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Delete(&queryTask).Error; err != nil {
		t.Fatal(err)
	}
	changes, err = repository.ListChanges(context.Background(), int64(tenantID), lastID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].Snapshot["name"] != "Catalog query current" || changes[0].Snapshot["task_status"] != "archived" || changes[1].Operation != "missing" {
		t.Fatalf("lifecycle changes = %#v", changes)
	}
}
