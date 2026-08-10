package service

import (
	"context"
	"testing"

	"github.com/addp/common/events"
	"github.com/addp/model/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupModelCleanupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS model").Error; err != nil {
		t.Fatalf("attach model schema: %v", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE model.dw_layers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			layer_code TEXT NOT NULL,
			layer_name TEXT NOT NULL,
			description TEXT,
			naming_rule TEXT,
			quality_sla TEXT,
			sort_order INTEGER DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE model.entities (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			domain_id INTEGER,
			name TEXT NOT NULL,
			code TEXT NOT NULL,
			description TEXT,
			status TEXT DEFAULT 'draft',
			created_by INTEGER NOT NULL,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE model.entity_attributes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_id INTEGER NOT NULL,
			element_id INTEGER,
			name TEXT NOT NULL,
			column_name TEXT NOT NULL,
			data_type TEXT NOT NULL,
			is_pk BOOLEAN DEFAULT FALSE,
			nullable BOOLEAN DEFAULT TRUE,
			description TEXT,
			sort_order INTEGER DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE model.entity_relations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			source_entity INTEGER NOT NULL,
			target_entity INTEGER NOT NULL,
			relation_type TEXT NOT NULL,
			name TEXT,
			description TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE model.logical_tables (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			domain_id INTEGER,
			entity_id INTEGER,
			name TEXT NOT NULL,
			code TEXT NOT NULL,
			description TEXT,
			table_type TEXT NOT NULL,
			layer TEXT,
			status TEXT DEFAULT 'draft',
			grain_description TEXT,
			scd_type INTEGER DEFAULT 0,
			materialization TEXT,
			created_by INTEGER NOT NULL,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE model.logical_fields (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			table_id INTEGER NOT NULL,
			element_id INTEGER,
			name TEXT NOT NULL,
			column_name TEXT NOT NULL,
			data_type TEXT NOT NULL,
			length INTEGER,
			nullable BOOLEAN DEFAULT TRUE,
			is_pk BOOLEAN DEFAULT FALSE,
			is_partition BOOLEAN DEFAULT FALSE,
			default_value TEXT,
			description TEXT,
			sort_order INTEGER DEFAULT 0,
			field_role TEXT,
			hierarchy_id INTEGER,
			hierarchy_level INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE model.table_relations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			source_table INTEGER NOT NULL,
			source_field INTEGER NOT NULL,
			target_table INTEGER NOT NULL,
			target_field INTEGER NOT NULL,
			relation_type TEXT NOT NULL,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE model.fact_metric_mappings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			fact_table_id INTEGER NOT NULL,
			metric_id INTEGER NOT NULL,
			field_id INTEGER,
			note TEXT,
			created_by INTEGER NOT NULL,
			created_at DATETIME
		)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create cleanup test table: %v", err)
		}
	}
	return db
}

func TestModelCleanupScanWithoutTenantLifecycleContextReturnsNoCandidates(t *testing.T) {
	db := setupModelCleanupTestDB(t)
	seedModelCleanupTenantState(t, db, 1)

	svc := NewCleanupService(db, nil, nil)
	stats, err := svc.ScanReclaimCandidates(context.Background(), 1, nil)
	if err != nil {
		t.Fatalf("ScanReclaimCandidates: %v", err)
	}
	if modelCandidateRecordCount(stats) != 0 {
		t.Fatalf("expected no candidates without tenant lifecycle context, got %+v", stats)
	}
}

func TestModelCleanupTenantDeletedLogicalDraftsActiveDefinitions(t *testing.T) {
	db := setupModelCleanupTestDB(t)
	entityID, tableID := seedModelCleanupTenantState(t, db, 1)

	svc := NewCleanupService(db, nil, nil)
	stats, err := svc.ExecuteCleanup(context.Background(), 1, events.CleanupModeLogical, map[string]interface{}{"tenant_id": 1})
	if err != nil {
		t.Fatalf("ExecuteCleanup: %v", err)
	}
	if stats.DraftedEntities != 1 || stats.DraftedTables != 2 {
		t.Fatalf("unexpected logical cleanup stats: %+v", stats)
	}
	var entity models.Entity
	if err := db.First(&entity, entityID).Error; err != nil {
		t.Fatalf("load entity: %v", err)
	}
	if entity.Status != "draft" {
		t.Fatalf("expected entity draft, got %s", entity.Status)
	}
	var table models.LogicalTable
	if err := db.First(&table, tableID).Error; err != nil {
		t.Fatalf("load logical table: %v", err)
	}
	if table.Status != "draft" {
		t.Fatalf("expected logical table draft, got %s", table.Status)
	}
}

func TestModelCleanupTenantDeletedPhysicalDeletesOwnedState(t *testing.T) {
	db := setupModelCleanupTestDB(t)
	seedModelCleanupTenantState(t, db, 1)
	seedModelCleanupTenantState(t, db, 2)

	svc := NewCleanupService(db, nil, nil)
	stats, err := svc.ScanReclaimCandidates(context.Background(), 1, map[string]interface{}{"tenant_id": 1})
	if err != nil {
		t.Fatalf("ScanReclaimCandidates: %v", err)
	}
	if stats.DWLayers != 1 || stats.Entities != 2 || stats.EntityAttributes != 1 || stats.EntityRelations != 1 ||
		stats.LogicalTables != 2 || stats.LogicalFields != 2 || stats.TableRelations != 1 || stats.FactMetricMappings != 1 {
		t.Fatalf("unexpected tenant scan stats: %+v", stats)
	}

	stats, err = svc.ExecuteCleanup(context.Background(), 1, events.CleanupModePhysical, map[string]interface{}{"tenant_id": 1})
	if err != nil {
		t.Fatalf("ExecuteCleanup: %v", err)
	}
	if stats.DeletedRecords != 11 {
		t.Fatalf("expected 11 deleted records, got %+v", stats)
	}
	assertModelCleanupCount(t, db, modelCleanupCountExpectation{tenantID: 1})
	assertModelCleanupCount(t, db, modelCleanupCountExpectation{
		tenantID:           2,
		dwLayers:           1,
		entities:           2,
		entityAttributes:   1,
		entityRelations:    1,
		logicalTables:      2,
		logicalFields:      2,
		tableRelations:     1,
		factMetricMappings: 1,
	})
}

func seedModelCleanupTenantState(t *testing.T, db *gorm.DB, tenantID int64) (int64, int64) {
	t.Helper()
	layer := models.DWLayer{TenantID: tenantID, LayerCode: "dwd", LayerName: "DWD"}
	if err := db.Create(&layer).Error; err != nil {
		t.Fatalf("create dw layer: %v", err)
	}
	entity := models.Entity{TenantID: tenantID, Name: "Order", Code: "order", Status: "approved", CreatedBy: 1}
	if err := db.Create(&entity).Error; err != nil {
		t.Fatalf("create entity: %v", err)
	}
	otherEntity := models.Entity{TenantID: tenantID, Name: "Customer", Code: "customer", Status: "draft", CreatedBy: 1}
	if err := db.Create(&otherEntity).Error; err != nil {
		t.Fatalf("create other entity: %v", err)
	}
	attr := models.EntityAttribute{EntityID: entity.ID, Name: "ID", ColumnName: "id", DataType: "bigint", IsPK: true}
	if err := db.Create(&attr).Error; err != nil {
		t.Fatalf("create entity attribute: %v", err)
	}
	entityRelation := models.EntityRelation{TenantID: tenantID, SourceEntity: entity.ID, TargetEntity: otherEntity.ID, RelationType: "one_to_many"}
	if err := db.Create(&entityRelation).Error; err != nil {
		t.Fatalf("create entity relation: %v", err)
	}
	factTable := models.LogicalTable{TenantID: tenantID, Name: "Fact Order", Code: "fact_order", TableType: "fact", Status: "approved", CreatedBy: 1}
	if err := db.Create(&factTable).Error; err != nil {
		t.Fatalf("create fact table: %v", err)
	}
	dimTable := models.LogicalTable{TenantID: tenantID, Name: "Dim Customer", Code: "dim_customer", TableType: "dimension", Status: "approved", CreatedBy: 1}
	if err := db.Create(&dimTable).Error; err != nil {
		t.Fatalf("create dim table: %v", err)
	}
	factField := models.LogicalField{TableID: factTable.ID, Name: "Customer ID", ColumnName: "customer_id", DataType: "bigint"}
	if err := db.Create(&factField).Error; err != nil {
		t.Fatalf("create fact field: %v", err)
	}
	dimField := models.LogicalField{TableID: dimTable.ID, Name: "ID", ColumnName: "id", DataType: "bigint", IsPK: true}
	if err := db.Create(&dimField).Error; err != nil {
		t.Fatalf("create dim field: %v", err)
	}
	tableRelation := models.TableRelation{TenantID: tenantID, SourceTable: factTable.ID, SourceField: factField.ID, TargetTable: dimTable.ID, TargetField: dimField.ID, RelationType: "fk"}
	if err := db.Create(&tableRelation).Error; err != nil {
		t.Fatalf("create table relation: %v", err)
	}
	mapping := models.FactMetricMapping{TenantID: tenantID, FactTableID: factTable.ID, MetricID: 9, FieldID: &factField.ID, CreatedBy: 1}
	if err := db.Create(&mapping).Error; err != nil {
		t.Fatalf("create fact metric mapping: %v", err)
	}
	return entity.ID, factTable.ID
}

type modelCleanupCountExpectation struct {
	tenantID           int64
	dwLayers           int64
	entities           int64
	entityAttributes   int64
	entityRelations    int64
	logicalTables      int64
	logicalFields      int64
	tableRelations     int64
	factMetricMappings int64
}

func assertModelCleanupCount(t *testing.T, db *gorm.DB, expected modelCleanupCountExpectation) {
	t.Helper()
	for _, item := range []struct {
		name     string
		model    interface{}
		expected int64
	}{
		{name: "dw_layers", model: &models.DWLayer{}, expected: expected.dwLayers},
		{name: "entities", model: &models.Entity{}, expected: expected.entities},
		{name: "entity_relations", model: &models.EntityRelation{}, expected: expected.entityRelations},
		{name: "logical_tables", model: &models.LogicalTable{}, expected: expected.logicalTables},
		{name: "table_relations", model: &models.TableRelation{}, expected: expected.tableRelations},
		{name: "fact_metric_mappings", model: &models.FactMetricMapping{}, expected: expected.factMetricMappings},
	} {
		var count int64
		if err := db.Model(item.model).Where("tenant_id = ?", expected.tenantID).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", item.name, err)
		}
		if count != item.expected {
			t.Fatalf("expected tenant %d %s count %d, got %d", expected.tenantID, item.name, item.expected, count)
		}
	}
	var entities []models.Entity
	if err := db.Where("tenant_id = ?", expected.tenantID).Find(&entities).Error; err != nil {
		t.Fatalf("load entities: %v", err)
	}
	entityIDs := make([]int64, 0, len(entities))
	for _, item := range entities {
		entityIDs = append(entityIDs, item.ID)
	}
	var attrCount int64
	if len(entityIDs) > 0 {
		if err := db.Model(&models.EntityAttribute{}).Where("entity_id IN ?", entityIDs).Count(&attrCount).Error; err != nil {
			t.Fatalf("count entity attributes: %v", err)
		}
	}
	if attrCount != expected.entityAttributes {
		t.Fatalf("expected tenant %d entity_attributes count %d, got %d", expected.tenantID, expected.entityAttributes, attrCount)
	}
	var tables []models.LogicalTable
	if err := db.Where("tenant_id = ?", expected.tenantID).Find(&tables).Error; err != nil {
		t.Fatalf("load logical tables: %v", err)
	}
	tableIDs := make([]int64, 0, len(tables))
	for _, item := range tables {
		tableIDs = append(tableIDs, item.ID)
	}
	var fieldCount int64
	if len(tableIDs) > 0 {
		if err := db.Model(&models.LogicalField{}).Where("table_id IN ?", tableIDs).Count(&fieldCount).Error; err != nil {
			t.Fatalf("count logical fields: %v", err)
		}
	}
	if fieldCount != expected.logicalFields {
		t.Fatalf("expected tenant %d logical_fields count %d, got %d", expected.tenantID, expected.logicalFields, fieldCount)
	}
}
