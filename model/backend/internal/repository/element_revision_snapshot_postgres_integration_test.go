package repository

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/addp/model/internal/migration"
	"github.com/addp/model/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresApprovedAggregatesRequireFrozenElementRevisions(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_MODEL_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_MODEL_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := migration.Run(db); err != nil {
		t.Fatal(err)
	}
	tenantID := time.Now().UnixNano()
	suffix := fmt.Sprintf("_%d", tenantID)
	layerCode := fmt.Sprintf("l%x", uint64(tenantID))

	entity := models.Entity{TenantID: tenantID, Name: "Snapshot entity", Code: "snapshot_entity" + suffix, Status: "draft", Version: 1, CreatedBy: 1}
	if err := db.Create(&entity).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&entity)
	elementID := int64(7001)
	attribute := models.EntityAttribute{EntityID: entity.ID, ElementID: &elementID, Name: "ID", ColumnName: "id", DataType: "bigint", IsPK: true}
	if err := db.Create(&attribute).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Entity{}).Where("id = ?", entity.ID).Update("status", "approved").Error; err == nil {
		t.Fatal("approving entity without element_revision_id should fail")
	}
	if err := db.Model(&models.EntityAttribute{}).Where("id = ?", attribute.ID).Update("element_revision_id", 700101).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.Entity{}).Where("id = ?", entity.ID).Update("status", "approved").Error; err != nil {
		t.Fatalf("approve entity with frozen revision: %v", err)
	}
	if err := db.Model(&models.EntityAttribute{}).Where("id = ?", attribute.ID).Update("element_revision_id", nil).Error; err == nil {
		t.Fatal("clearing an approved entity snapshot should fail")
	}
	if err := db.Model(&models.Entity{}).Where("id = ?", entity.ID).Update("status", "draft").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.EntityAttribute{}).Where("id = ?", attribute.ID).Update("element_revision_id", nil).Error; err != nil {
		t.Fatalf("clear reopened entity snapshot: %v", err)
	}

	layer := models.DWLayer{TenantID: tenantID, LayerCode: layerCode, LayerName: "Snapshot", Version: 1}
	if err := db.Create(&layer).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&layer)
	table := models.LogicalTable{TenantID: tenantID, Name: "Snapshot table", Code: "snapshot_table" + suffix, TableType: "entity", Layer: layer.LayerCode, Status: "draft", Materialization: models.JSONB{}, Version: 1, CreatedBy: 1}
	if err := db.Create(&table).Error; err != nil {
		t.Fatal(err)
	}
	defer db.Delete(&table)
	field := models.LogicalField{TableID: table.ID, ElementID: &elementID, Name: "ID", ColumnName: "id", DataType: "bigint", IsPK: true, FieldRole: "regular"}
	if err := db.Create(&field).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.LogicalTable{}).Where("id = ?", table.ID).Update("status", "approved").Error; err == nil {
		t.Fatal("approving logical table without element_revision_id should fail")
	}
	if err := db.Model(&models.LogicalField{}).Where("id = ?", field.ID).Update("element_revision_id", 700101).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&models.LogicalTable{}).Where("id = ?", table.ID).Update("status", "approved").Error; err != nil {
		t.Fatalf("approve logical table with frozen revision: %v", err)
	}
	if err := db.Model(&models.LogicalField{}).Where("id = ?", field.ID).Update("element_revision_id", nil).Error; err == nil {
		t.Fatal("clearing an approved logical table snapshot should fail")
	}
}
