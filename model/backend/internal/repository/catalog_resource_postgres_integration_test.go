package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/model/internal/migration"
	"github.com/addp/model/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresCatalogResourceChangeFeedCapturesOwnerLifecycle(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_MODEL_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatalf("run model migrations: %v", err)
	}

	tenantID := time.Now().UnixNano()
	domainID := tenantID + 1
	entity := models.Entity{
		TenantID: tenantID, DomainID: &domainID, Name: "Catalog entity",
		Code: fmt.Sprintf("catalog_entity_%d", tenantID), Status: "draft", Version: 1, CreatedBy: 1,
	}
	if err := db.Create(&entity).Error; err != nil {
		t.Fatalf("create entity: %v", err)
	}
	layer := models.DWLayer{TenantID: tenantID, LayerCode: "dwd", LayerName: "Catalog DWD"}
	if err := db.Create(&layer).Error; err != nil {
		t.Fatalf("create layer: %v", err)
	}
	logicalTable := models.LogicalTable{
		TenantID: tenantID, DomainID: &domainID, EntityID: &entity.ID, Name: "Catalog table",
		Code: fmt.Sprintf("catalog_table_%d", tenantID), TableType: "fact", Layer: "dwd",
		Status: "draft", Version: 1, CreatedBy: 1,
	}
	if err := db.Create(&logicalTable).Error; err != nil {
		t.Fatalf("create logical table: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM model.logical_tables WHERE tenant_id = ?", tenantID).Error
		_ = db.Exec("DELETE FROM model.entities WHERE tenant_id = ?", tenantID).Error
		_ = db.Exec("DELETE FROM model.dw_layers WHERE tenant_id = ?", tenantID).Error
		_ = db.Exec("DELETE FROM model.catalog_resource_changes WHERE tenant_id = ?", tenantID).Error
	})

	repository := NewCatalogResourceRepository(db)
	changes, err := repository.ListChanges(context.Background(), tenantID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].SourceType != models.CatalogSourceTypeEntity ||
		changes[1].SourceType != models.CatalogSourceTypeLogicalTable {
		t.Fatalf("initial changes = %#v", changes)
	}
	assertCatalogSnapshotValue(t, changes[0].Snapshot, "domain_id", fmt.Sprint(domainID))
	assertCatalogSnapshotValue(t, changes[1].Snapshot, "table_type", "fact")

	if err := db.Model(&entity).Updates(map[string]any{"name": "Catalog entity current", "version": 2}).Error; err != nil {
		t.Fatalf("update entity: %v", err)
	}
	if err := db.Delete(&logicalTable).Error; err != nil {
		t.Fatalf("delete logical table: %v", err)
	}
	changes, err = repository.ListChanges(context.Background(), tenantID, changes[len(changes)-1].ID, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].Operation != "upsert" || changes[1].Operation != "missing" {
		t.Fatalf("lifecycle changes = %#v", changes)
	}
	assertCatalogSnapshotValue(t, changes[0].Snapshot, "name", "Catalog entity current")
}

func assertCatalogSnapshotValue(t *testing.T, snapshot models.JSONB, key string, want any) {
	t.Helper()
	raw, err := json.Marshal(snapshot[key])
	if err != nil {
		t.Fatalf("marshal snapshot %s: %v", key, err)
	}
	var got any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal snapshot %s: %v", key, err)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("snapshot[%s] = %v, want %v", key, got, want)
	}
}
