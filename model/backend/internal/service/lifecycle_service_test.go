package service

import (
	"strings"
	"testing"

	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupLifecycleServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.Exec("ATTACH DATABASE ':memory:' AS model").Error; err != nil {
		t.Fatalf("attach model schema: %v", err)
	}
	for _, ddl := range []string{
		`CREATE TABLE model.entities (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL,
			domain_id INTEGER, name TEXT NOT NULL, code TEXT NOT NULL, description TEXT,
			status TEXT NOT NULL, created_by INTEGER NOT NULL, updated_by INTEGER,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE model.entity_attributes (
			id INTEGER PRIMARY KEY AUTOINCREMENT, entity_id INTEGER NOT NULL, element_id INTEGER,
			name TEXT NOT NULL, column_name TEXT NOT NULL, data_type TEXT NOT NULL,
			is_pk BOOLEAN, nullable BOOLEAN, description TEXT, sort_order INTEGER,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE model.entity_relations (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL,
			source_entity INTEGER NOT NULL, target_entity INTEGER NOT NULL,
			relation_type TEXT NOT NULL, name TEXT, description TEXT,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE model.logical_tables (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL,
			domain_id INTEGER, entity_id INTEGER, name TEXT NOT NULL, code TEXT NOT NULL,
			description TEXT, table_type TEXT NOT NULL, layer TEXT, status TEXT NOT NULL,
			grain_description TEXT, scd_type INTEGER, materialization TEXT,
			created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE model.logical_fields (
			id INTEGER PRIMARY KEY AUTOINCREMENT, table_id INTEGER NOT NULL, element_id INTEGER,
			name TEXT NOT NULL, column_name TEXT NOT NULL, data_type TEXT NOT NULL,
			length INTEGER, nullable BOOLEAN, is_pk BOOLEAN, is_partition BOOLEAN,
			default_value TEXT, description TEXT, sort_order INTEGER, field_role TEXT,
			hierarchy_id INTEGER, hierarchy_level INTEGER, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE model.table_relations (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL,
			source_table INTEGER NOT NULL, source_field INTEGER NOT NULL,
			target_table INTEGER NOT NULL, target_field INTEGER NOT NULL,
			relation_type TEXT NOT NULL, created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE model.fact_metric_mappings (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL,
			fact_table_id INTEGER NOT NULL, metric_id INTEGER NOT NULL, field_id INTEGER,
			note TEXT, created_by INTEGER NOT NULL, created_at DATETIME
		)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create lifecycle test table: %v", err)
		}
	}
	return db
}

func TestEntityLifecycleRejectsApprovedAggregateWrites(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	entityRepo := repository.NewEntityRepository(db)
	relationRepo := repository.NewEntityRelationRepository(db)
	approved := models.Entity{TenantID: 1, Name: "Customer", Code: "customer", Status: "approved", CreatedBy: 1}
	draft := models.Entity{TenantID: 1, Name: "Order", Code: "order", Status: "draft", CreatedBy: 1}
	if err := db.Create(&approved).Error; err != nil {
		t.Fatalf("create approved entity: %v", err)
	}
	if err := db.Create(&draft).Error; err != nil {
		t.Fatalf("create draft entity: %v", err)
	}

	entityService := NewEntityService(entityRepo, relationRepo)
	if err := entityService.DeleteEntity(approved.ID, 1); err == nil {
		t.Fatal("delete approved entity error = nil")
	}
	_, err := entityService.ImportFromMermaid(1, 1, &models.MermaidImportRequest{
		MermaidCode: "erDiagram\n  product {\n    bigint id PK\n  }",
	})
	if err == nil || !strings.Contains(err.Error(), "已审批实体") {
		t.Fatalf("import error = %v, want approved entity rejection", err)
	}

	relationService := NewEntityRelationService(relationRepo, entityRepo)
	_, err = relationService.Create(1, &models.CreateEntityRelationRequest{
		SourceEntity: approved.ID, TargetEntity: draft.ID, RelationType: "one_to_many",
	})
	if err == nil || !strings.Contains(err.Error(), "草稿状态") {
		t.Fatalf("create relation error = %v, want draft status rejection", err)
	}
}

func TestLogicalModelLifecycleRejectsApprovedIndirectWrites(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	tableRepo := repository.NewLogicalTableRepository(db)
	metricRepo := repository.NewFactMetricRepository(db)
	relationRepo := repository.NewTableRelationRepository(db)
	fact := models.LogicalTable{TenantID: 1, Name: "Orders", Code: "orders", TableType: "fact", Status: "approved", CreatedBy: 1}
	dimension := models.LogicalTable{TenantID: 1, Name: "Customer", Code: "customer", TableType: "dimension", Status: "draft", CreatedBy: 1}
	if err := db.Create(&fact).Error; err != nil {
		t.Fatalf("create fact table: %v", err)
	}
	if err := db.Create(&dimension).Error; err != nil {
		t.Fatalf("create dimension table: %v", err)
	}

	metricService := NewFactMetricService(metricRepo, tableRepo)
	_, err := metricService.AddMetric(fact.ID, 1, 1, &models.CreateFactMetricMappingRequest{MetricID: 9})
	if err == nil || !strings.Contains(err.Error(), "已审批事实表") {
		t.Fatalf("add metric error = %v, want approved fact rejection", err)
	}

	tableRelationService := NewTableRelationService(relationRepo, tableRepo)
	_, err = tableRelationService.AddDimensionRelation(fact.ID, 1, &models.CreateTableRelationRequest{TargetTable: dimension.ID})
	if err == nil || !strings.Contains(err.Error(), "草稿状态") {
		t.Fatalf("add dimension relation error = %v, want draft status rejection", err)
	}
}

func TestLogicalTableDeleteRejectsRelationToApprovedTable(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	tableRepo := repository.NewLogicalTableRepository(db)
	fact := models.LogicalTable{TenantID: 1, Name: "Orders", Code: "orders", TableType: "fact", Status: "approved", CreatedBy: 1}
	dimension := models.LogicalTable{TenantID: 1, Name: "Customer", Code: "customer", TableType: "dimension", Status: "draft", CreatedBy: 1}
	if err := db.Create(&fact).Error; err != nil {
		t.Fatalf("create fact table: %v", err)
	}
	if err := db.Create(&dimension).Error; err != nil {
		t.Fatalf("create dimension table: %v", err)
	}
	relation := models.TableRelation{
		TenantID: 1, SourceTable: fact.ID, SourceField: 10,
		TargetTable: dimension.ID, TargetField: 20, RelationType: "fk",
	}
	if err := db.Create(&relation).Error; err != nil {
		t.Fatalf("create table relation: %v", err)
	}

	err := NewLogicalTableService(tableRepo).DeleteLogicalTable(dimension.ID, 1)
	if err == nil || !strings.Contains(err.Error(), "已审批模型") {
		t.Fatalf("delete dimension error = %v, want approved relation rejection", err)
	}
}
