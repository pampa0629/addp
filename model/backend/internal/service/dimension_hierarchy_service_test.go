package service

import (
	"testing"

	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
)

func TestDimensionHierarchyUsesLogicalTableAggregateVersion(t *testing.T) {
	exerciseDimensionHierarchyVersion(t, setupLifecycleServiceTestDB(t), 1)
}

func TestPostgresDimensionHierarchyUsesLogicalTableAggregateVersion(t *testing.T) {
	tx, tenantID := beginModelAggregatePostgresTransaction(t)
	exerciseDimensionHierarchyVersion(t, tx, tenantID)
}

func exerciseDimensionHierarchyVersion(t *testing.T, db *gorm.DB, tenantID int64) {
	t.Helper()
	layer := models.DWLayer{TenantID: tenantID, LayerCode: "hierarchy_test", LayerName: "Hierarchy test", Version: 1}
	if err := db.Create(&layer).Error; err != nil {
		t.Fatal(err)
	}
	table := models.LogicalTable{TenantID: tenantID, Name: "Date", Code: "dim_date", TableType: "dimension", Layer: layer.LayerCode, Status: "draft", Version: 1, CreatedBy: tenantID}
	if err := db.Create(&table).Error; err != nil {
		t.Fatalf("create dimension table: %v", err)
	}
	field := models.LogicalField{TableID: table.ID, Name: "Year", ColumnName: "year", DataType: "int"}
	if err := db.Create(&field).Error; err != nil {
		t.Fatalf("create dimension field: %v", err)
	}

	svc := NewDimensionHierarchyService(repository.NewDimensionHierarchyRepository(db), repository.NewLogicalTableRepository(db))
	created, err := svc.Create(table.ID, tenantID, &models.CreateDimensionHierarchyRequest{Version: 1, Name: "Calendar"})
	if err != nil {
		t.Fatalf("create hierarchy: %v", err)
	}
	if created.Version != 2 || created.Hierarchy.TableID != table.ID || created.Hierarchy.TenantID != tenantID {
		t.Fatalf("unexpected create result: %+v", created)
	}

	level, err := svc.CreateLevel(created.Hierarchy.ID, table.ID, tenantID, &models.UpsertDimensionHierarchyLevelRequest{
		Version: 2, FieldID: field.ID, LevelNum: 1, LevelName: "Year",
	})
	if err != nil {
		t.Fatalf("create hierarchy level: %v", err)
	}
	if level.Version != 3 || level.Level.FieldID != field.ID {
		t.Fatalf("unexpected level result: %+v", level)
	}

	items, err := svc.List(table.ID, tenantID)
	if err != nil {
		t.Fatalf("list hierarchies: %v", err)
	}
	if len(items) != 1 || len(items[0].Levels) != 1 || items[0].Levels[0].LevelName != "Year" {
		t.Fatalf("unexpected hierarchy list: %+v", items)
	}
	_, err = NewLogicalTableService(repository.NewLogicalTableRepository(db), nil).DeleteField(field.ID, table.ID, tenantID, level.Version)
	requireDomainErrorCode(t, err, "dimension_hierarchy_field_in_use")

	_, err = svc.Update(created.Hierarchy.ID, table.ID, tenantID, &models.UpdateDimensionHierarchyRequest{Version: 2, Name: "Stale"})
	requireDomainErrorCode(t, err, "resource_version_conflict")

	_, err = svc.DeleteLevel(level.Level.ID, created.Hierarchy.ID, table.ID, tenantID, created.Version)
	requireDomainErrorCode(t, err, "resource_version_conflict")
	unchanged, err := svc.List(table.ID, tenantID)
	if err != nil || len(unchanged) != 1 || unchanged[0].Name != "Calendar" ||
		len(unchanged[0].Levels) != 1 || unchanged[0].Levels[0].ID != level.Level.ID {
		t.Fatalf("stale updates changed hierarchy: %+v err=%v", unchanged, err)
	}

	removedLevel, err := svc.DeleteLevel(level.Level.ID, created.Hierarchy.ID, table.ID, tenantID, level.Version)
	if err != nil || removedLevel.Version != 4 {
		t.Fatalf("delete current level: %+v err=%v", removedLevel, err)
	}
	removedHierarchy, err := svc.Delete(created.Hierarchy.ID, table.ID, tenantID, removedLevel.Version)
	if err != nil || removedHierarchy.Version != 5 {
		t.Fatalf("delete current hierarchy: %+v err=%v", removedHierarchy, err)
	}
	items, err = svc.List(table.ID, tenantID)
	if err != nil || len(items) != 0 {
		t.Fatalf("unexpected hierarchy after removal: %+v err=%v", items, err)
	}
	storedTable, err := repository.NewLogicalTableRepository(db).GetByID(table.ID, tenantID)
	if err != nil || storedTable.Version != removedHierarchy.Version {
		t.Fatalf("unexpected persisted table version: %+v err=%v", storedTable, err)
	}
}

func TestDimensionHierarchyRejectsInvalidOwnerAndField(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	dimension := models.LogicalTable{TenantID: 1, Name: "Date", Code: "dim_date", TableType: "dimension", Status: "draft", Version: 1, CreatedBy: 1}
	fact := models.LogicalTable{TenantID: 1, Name: "Orders", Code: "fact_orders", TableType: "fact", Status: "draft", Version: 1, CreatedBy: 1}
	approved := models.LogicalTable{TenantID: 1, Name: "Region", Code: "dim_region", TableType: "dimension", Status: "approved", Version: 1, CreatedBy: 1}
	for _, table := range []*models.LogicalTable{&dimension, &fact, &approved} {
		if err := db.Create(table).Error; err != nil {
			t.Fatalf("create logical table: %v", err)
		}
	}
	foreignField := models.LogicalField{TableID: fact.ID, Name: "Amount", ColumnName: "amount", DataType: "decimal"}
	if err := db.Create(&foreignField).Error; err != nil {
		t.Fatalf("create foreign field: %v", err)
	}

	svc := NewDimensionHierarchyService(repository.NewDimensionHierarchyRepository(db), repository.NewLogicalTableRepository(db))
	_, err := svc.Create(fact.ID, 1, &models.CreateDimensionHierarchyRequest{Version: 1, Name: "Invalid"})
	requireDomainErrorCode(t, err, "dimension_table_required")
	_, err = svc.Create(approved.ID, 1, &models.CreateDimensionHierarchyRequest{Version: 1, Name: "Locked"})
	requireDomainErrorCode(t, err, "logical_table_state_conflict")

	created, err := svc.Create(dimension.ID, 1, &models.CreateDimensionHierarchyRequest{Version: 1, Name: "Calendar"})
	if err != nil {
		t.Fatalf("create hierarchy: %v", err)
	}
	_, err = svc.CreateLevel(created.Hierarchy.ID, dimension.ID, 1, &models.UpsertDimensionHierarchyLevelRequest{
		Version: created.Version, FieldID: foreignField.ID, LevelNum: 1, LevelName: "Foreign",
	})
	requireDomainErrorCode(t, err, "dimension_hierarchy_field_not_found")
}
