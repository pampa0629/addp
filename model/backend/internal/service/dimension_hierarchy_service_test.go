package service

import (
	"testing"

	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

func TestDimensionHierarchyUsesLogicalTableAggregateVersion(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	table := models.LogicalTable{TenantID: 1, Name: "Date", Code: "dim_date", TableType: "dimension", Status: "draft", Version: 1, CreatedBy: 1}
	if err := db.Create(&table).Error; err != nil {
		t.Fatalf("create dimension table: %v", err)
	}
	field := models.LogicalField{TableID: table.ID, Name: "Year", ColumnName: "year", DataType: "int"}
	if err := db.Create(&field).Error; err != nil {
		t.Fatalf("create dimension field: %v", err)
	}

	svc := NewDimensionHierarchyService(repository.NewDimensionHierarchyRepository(db), repository.NewLogicalTableRepository(db))
	created, err := svc.Create(table.ID, 1, &models.CreateDimensionHierarchyRequest{Version: 1, Name: "Calendar"})
	if err != nil {
		t.Fatalf("create hierarchy: %v", err)
	}
	if created.Version != 2 || created.Hierarchy.TableID != table.ID || created.Hierarchy.TenantID != 1 {
		t.Fatalf("unexpected create result: %+v", created)
	}

	level, err := svc.CreateLevel(created.Hierarchy.ID, table.ID, 1, &models.UpsertDimensionHierarchyLevelRequest{
		Version: 2, FieldID: field.ID, LevelNum: 1, LevelName: "Year",
	})
	if err != nil {
		t.Fatalf("create hierarchy level: %v", err)
	}
	if level.Version != 3 || level.Level.FieldID != field.ID {
		t.Fatalf("unexpected level result: %+v", level)
	}

	items, err := svc.List(table.ID, 1)
	if err != nil {
		t.Fatalf("list hierarchies: %v", err)
	}
	if len(items) != 1 || len(items[0].Levels) != 1 || items[0].Levels[0].LevelName != "Year" {
		t.Fatalf("unexpected hierarchy list: %+v", items)
	}
	_, err = NewLogicalTableService(repository.NewLogicalTableRepository(db), nil, nil).DeleteField(field.ID, table.ID, 1, level.Version)
	requireDomainErrorCode(t, err, "dimension_hierarchy_field_in_use")

	_, err = svc.Update(created.Hierarchy.ID, table.ID, 1, &models.UpdateDimensionHierarchyRequest{Version: 2, Name: "Stale"})
	requireDomainErrorCode(t, err, "resource_version_conflict")
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
