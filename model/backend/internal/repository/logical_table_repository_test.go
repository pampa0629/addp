package repository

import (
	"testing"

	"github.com/addp/model/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupLogicalTableRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS model").Error; err != nil {
		t.Fatalf("attach model schema: %v", err)
	}
	for _, ddl := range []string{
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
			status TEXT NOT NULL,
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
			t.Fatalf("create repository test table: %v", err)
		}
	}
	return db
}

func TestLogicalTableDeleteRejectsCrossTenantChildDeletion(t *testing.T) {
	db := setupLogicalTableRepositoryTestDB(t)
	table := models.LogicalTable{TenantID: 2, Name: "Orders", Code: "orders", TableType: "fact", Status: "draft", CreatedBy: 1}
	if err := db.Create(&table).Error; err != nil {
		t.Fatalf("create logical table: %v", err)
	}
	field := models.LogicalField{TableID: table.ID, Name: "ID", ColumnName: "id", DataType: "bigint"}
	if err := db.Create(&field).Error; err != nil {
		t.Fatalf("create logical field: %v", err)
	}

	err := NewLogicalTableRepository(db).Delete(table.ID, 1)
	if err == nil {
		t.Fatal("cross-tenant delete error = nil")
	}

	assertRepositoryRecordCount(t, db, &models.LogicalTable{}, 1, "id = ?", table.ID)
	assertRepositoryRecordCount(t, db, &models.LogicalField{}, 1, "table_id = ?", table.ID)
}

func TestLogicalTableDeleteRemovesAggregateInOneTenant(t *testing.T) {
	db := setupLogicalTableRepositoryTestDB(t)
	table := models.LogicalTable{TenantID: 1, Name: "Orders", Code: "orders", TableType: "fact", Status: "draft", CreatedBy: 1}
	dimension := models.LogicalTable{TenantID: 1, Name: "Customer", Code: "customer", TableType: "dimension", Status: "draft", CreatedBy: 1}
	if err := db.Create(&table).Error; err != nil {
		t.Fatalf("create fact table: %v", err)
	}
	if err := db.Create(&dimension).Error; err != nil {
		t.Fatalf("create dimension table: %v", err)
	}
	factField := models.LogicalField{TableID: table.ID, Name: "Customer ID", ColumnName: "customer_id", DataType: "bigint"}
	dimensionField := models.LogicalField{TableID: dimension.ID, Name: "ID", ColumnName: "id", DataType: "bigint"}
	if err := db.Create(&factField).Error; err != nil {
		t.Fatalf("create fact field: %v", err)
	}
	if err := db.Create(&dimensionField).Error; err != nil {
		t.Fatalf("create dimension field: %v", err)
	}
	relation := models.TableRelation{TenantID: 1, SourceTable: table.ID, SourceField: factField.ID, TargetTable: dimension.ID, TargetField: dimensionField.ID, RelationType: "fk"}
	mapping := models.FactMetricMapping{TenantID: 1, FactTableID: table.ID, MetricID: 9, CreatedBy: 1}
	if err := db.Create(&relation).Error; err != nil {
		t.Fatalf("create table relation: %v", err)
	}
	if err := db.Create(&mapping).Error; err != nil {
		t.Fatalf("create metric mapping: %v", err)
	}

	if err := NewLogicalTableRepository(db).Delete(table.ID, 1); err != nil {
		t.Fatalf("delete logical table: %v", err)
	}

	assertRepositoryRecordCount(t, db, &models.LogicalTable{}, 0, "id = ?", table.ID)
	assertRepositoryRecordCount(t, db, &models.LogicalField{}, 0, "table_id = ?", table.ID)
	assertRepositoryRecordCount(t, db, &models.TableRelation{}, 0, "source_table = ? OR target_table = ?", table.ID, table.ID)
	assertRepositoryRecordCount(t, db, &models.FactMetricMapping{}, 0, "fact_table_id = ?", table.ID)
	assertRepositoryRecordCount(t, db, &models.LogicalTable{}, 1, "id = ?", dimension.ID)
}

func assertRepositoryRecordCount(t *testing.T, db *gorm.DB, model any, want int64, query string, args ...any) {
	t.Helper()
	var count int64
	if err := db.Model(model).Where(query, args...).Count(&count).Error; err != nil {
		t.Fatalf("count records: %v", err)
	}
	if count != want {
		t.Fatalf("record count = %d, want %d", count, want)
	}
}
