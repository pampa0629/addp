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
			version INTEGER NOT NULL DEFAULT 1,
			created_by INTEGER NOT NULL,
			updated_by INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE model.logical_fields (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			table_id INTEGER NOT NULL,
			element_id INTEGER,
			element_revision_id INTEGER,
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
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE model.dimension_hierarchies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			table_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE model.dimension_hierarchy_levels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			hierarchy_id INTEGER NOT NULL,
			field_id INTEGER NOT NULL,
			level_num INTEGER NOT NULL,
			level_name TEXT NOT NULL,
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
		`CREATE TABLE model.metric_implementations (
 id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, fact_table_id INTEGER NOT NULL,
 metric_definition_id INTEGER NOT NULL, name TEXT NOT NULL, note TEXT,
 version INTEGER NOT NULL DEFAULT 1, created_by INTEGER NOT NULL, updated_by INTEGER,
 created_at DATETIME, updated_at DATETIME
 )`,
		`CREATE TABLE model.materialization_batches (
			id TEXT PRIMARY KEY, tenant_id INTEGER NOT NULL, logical_table_id INTEGER NOT NULL,
			logical_table_version INTEGER NOT NULL, engine_id INTEGER NOT NULL,
			target_parent_locator TEXT NOT NULL, target_name TEXT NOT NULL, staging_name TEXT NOT NULL,
			schema_fingerprint TEXT NOT NULL, expected_target_marker TEXT, status TEXT NOT NULL,
			prepare_execution_id TEXT NOT NULL, writer_execution_id TEXT, seal_execution_id TEXT,
			publish_execution_id TEXT, published_at DATETIME, created_at DATETIME, updated_at DATETIME
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

	err := NewLogicalTableRepository(db).Delete(table.ID, 1, table.Version)
	if err == nil {
		t.Fatal("cross-tenant delete error = nil")
	}

	assertRepositoryRecordCount(t, db, &models.LogicalTable{}, 1, "id = ?", table.ID)
	assertRepositoryRecordCount(t, db, &models.LogicalField{}, 1, "table_id = ?", table.ID)
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
