package service

import (
	"testing"

	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func requireDomainErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	domainErr, ok := apperrors.As(err)
	if !ok || domainErr.Code != code {
		t.Fatalf("error = %v, want domain code %q", err, code)
	}
}

func intPointer(value int) *int {
	return &value
}

func setupLifecycleServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{TranslateError: true})
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
		`CREATE TABLE model.dw_layers (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL,
			layer_code TEXT NOT NULL, layer_name TEXT NOT NULL, description TEXT,
			naming_rule TEXT, quality_sla TEXT, sort_order INTEGER,
			created_at DATETIME, updated_at DATETIME
		)`,
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
	requireDomainErrorCode(t, err, "entity_state_conflict")

	relationService := NewEntityRelationService(relationRepo, entityRepo)
	_, err = relationService.Create(1, &models.CreateEntityRelationRequest{
		SourceEntity: approved.ID, TargetEntity: draft.ID, RelationType: "one_to_many",
	})
	requireDomainErrorCode(t, err, "entity_relation_state_conflict")
}

func TestEntityApprovalReportsMissingAggregateRequirements(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	entityRepo := repository.NewEntityRepository(db)
	svc := NewEntityService(entityRepo, repository.NewEntityRelationRepository(db))
	entity := models.Entity{TenantID: 1, Name: "Activity", Code: "activity", Status: "draft", CreatedBy: 1}
	if err := db.Create(&entity).Error; err != nil {
		t.Fatalf("create entity: %v", err)
	}

	requireDomainErrorCode(t, svc.ApproveEntity(entity.ID, 1, 1), "entity_approval_attributes_required")

	attribute := models.EntityAttribute{EntityID: entity.ID, Name: "Title", ColumnName: "title", DataType: "string"}
	if err := db.Create(&attribute).Error; err != nil {
		t.Fatalf("create entity attribute: %v", err)
	}
	requireDomainErrorCode(t, svc.ApproveEntity(entity.ID, 1, 1), "entity_approval_primary_key_required")
}

func TestLogicalTableApprovalReportsMissingAggregateRequirements(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	tableRepo := repository.NewLogicalTableRepository(db)
	svc := NewLogicalTableService(tableRepo, repository.NewEntityRepository(db), repository.NewDWLayerRepository(db))
	table := models.LogicalTable{
		TenantID: 1, Name: "Customer", Code: "customer", TableType: "entity", Layer: "dwd",
		Status: "draft", CreatedBy: 1,
	}
	if err := db.Create(&table).Error; err != nil {
		t.Fatalf("create logical table: %v", err)
	}

	requireDomainErrorCode(t, svc.ApproveLogicalTable(table.ID, 1, 1), "logical_table_approval_fields_required")

	field := models.LogicalField{TableID: table.ID, Name: "Name", ColumnName: "name", DataType: "string"}
	if err := db.Create(&field).Error; err != nil {
		t.Fatalf("create logical field: %v", err)
	}
	requireDomainErrorCode(t, svc.ApproveLogicalTable(table.ID, 1, 1), "logical_table_approval_primary_key_required")
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
	requireDomainErrorCode(t, err, "fact_metric_state_conflict")

	tableRelationService := NewTableRelationService(relationRepo, tableRepo)
	_, err = tableRelationService.AddDimensionRelation(fact.ID, 1, &models.CreateTableRelationRequest{
		TargetTable: dimension.ID, SourceField: 1, TargetField: 2,
	})
	requireDomainErrorCode(t, err, "table_relation_state_conflict")
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

	err := NewLogicalTableService(
		tableRepo,
		repository.NewEntityRepository(db),
		repository.NewDWLayerRepository(db),
	).DeleteLogicalTable(dimension.ID, 1)
	requireDomainErrorCode(t, err, "logical_table_relation_state_conflict")
}

func TestLogicalTableRejectsCrossTenantEntityAndUnknownLayerReferences(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	tableRepo := repository.NewLogicalTableRepository(db)
	entityRepo := repository.NewEntityRepository(db)
	dwLayerRepo := repository.NewDWLayerRepository(db)
	svc := NewLogicalTableService(tableRepo, entityRepo, dwLayerRepo)

	layer := models.DWLayer{TenantID: 1, LayerCode: "dwd", LayerName: "DWD"}
	foreignEntity := models.Entity{TenantID: 2, Name: "Foreign", Code: "foreign", Status: "draft", CreatedBy: 1}
	if err := db.Create(&layer).Error; err != nil {
		t.Fatalf("create layer: %v", err)
	}
	if err := db.Create(&foreignEntity).Error; err != nil {
		t.Fatalf("create foreign entity: %v", err)
	}

	_, err := svc.CreateLogicalTable(&models.CreateLogicalTableRequest{
		EntityID: &foreignEntity.ID, Name: "Orders", Code: "orders", TableType: "entity", Layer: "dwd",
	}, 1, 1)
	requireDomainErrorCode(t, err, "entity_not_found")

	_, err = svc.CreateLogicalTable(&models.CreateLogicalTableRequest{
		Name: "Orders", Code: "orders", TableType: "entity", Layer: "missing",
	}, 1, 1)
	requireDomainErrorCode(t, err, "dw_layer_not_found")

	table := models.LogicalTable{TenantID: 1, Name: "Orders", Code: "orders", TableType: "entity", Layer: "dwd", Status: "draft", CreatedBy: 1}
	if err := db.Create(&table).Error; err != nil {
		t.Fatalf("create logical table: %v", err)
	}
	_, err = svc.UpdateLogicalTable(table.ID, 1, 1, &models.UpdateLogicalTableRequest{
		EntityID: &foreignEntity.ID, Name: "Orders", TableType: "entity", Layer: "dwd", SCDType: intPointer(0), Materialization: map[string]interface{}{},
	})
	requireDomainErrorCode(t, err, "entity_not_found")

	_, err = svc.UpdateLogicalTable(table.ID, 1, 1, &models.UpdateLogicalTableRequest{
		Name: "Orders", TableType: "entity", Layer: "missing", SCDType: intPointer(0), Materialization: map[string]interface{}{},
	})
	requireDomainErrorCode(t, err, "dw_layer_not_found")
}

func TestPutUpdatesClearNullableModelReferences(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	entityRepo := repository.NewEntityRepository(db)
	relationRepo := repository.NewEntityRelationRepository(db)
	tableRepo := repository.NewLogicalTableRepository(db)
	dwLayerRepo := repository.NewDWLayerRepository(db)

	domainID := int64(9)
	entity := models.Entity{TenantID: 1, DomainID: &domainID, Name: "Order", Code: "order", Status: "draft", CreatedBy: 1}
	if err := db.Create(&entity).Error; err != nil {
		t.Fatalf("create entity: %v", err)
	}
	updatedEntity, err := NewEntityService(entityRepo, relationRepo).UpdateEntity(entity.ID, 1, 1, &models.UpdateEntityRequest{
		Name: "Order", Description: "updated",
	})
	if err != nil {
		t.Fatalf("update entity: %v", err)
	}
	if updatedEntity.DomainID != nil {
		t.Fatalf("domain_id = %v, want nil", *updatedEntity.DomainID)
	}

	layer := models.DWLayer{TenantID: 1, LayerCode: "dwd", LayerName: "DWD"}
	if err := db.Create(&layer).Error; err != nil {
		t.Fatalf("create layer: %v", err)
	}
	table := models.LogicalTable{
		TenantID: 1, DomainID: &domainID, EntityID: &entity.ID, Name: "Order Table", Code: "order_table",
		TableType: "entity", Layer: "dwd", Status: "draft", Materialization: models.JSONB{}, CreatedBy: 1,
	}
	if err := db.Create(&table).Error; err != nil {
		t.Fatalf("create logical table: %v", err)
	}
	updatedTable, err := NewLogicalTableService(tableRepo, entityRepo, dwLayerRepo).UpdateLogicalTable(table.ID, 1, 1, &models.UpdateLogicalTableRequest{
		Name: "Order Table", TableType: "entity", Layer: "dwd", SCDType: intPointer(0), Materialization: map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("update logical table: %v", err)
	}
	if updatedTable.DomainID != nil || updatedTable.EntityID != nil {
		t.Fatalf("references = domain:%v entity:%v, want nil", updatedTable.DomainID, updatedTable.EntityID)
	}
}

func TestAggregateChildrenRejectCrossTenantParentsAndTargets(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	entityRepo := repository.NewEntityRepository(db)
	relationRepo := repository.NewEntityRelationRepository(db)
	tableRepo := repository.NewLogicalTableRepository(db)
	metricRepo := repository.NewFactMetricRepository(db)
	tableRelationRepo := repository.NewTableRelationRepository(db)

	localEntity := models.Entity{TenantID: 1, Name: "Local", Code: "local", Status: "draft", CreatedBy: 1}
	foreignEntity := models.Entity{TenantID: 2, Name: "Foreign", Code: "foreign", Status: "draft", CreatedBy: 1}
	if err := db.Create(&localEntity).Error; err != nil {
		t.Fatalf("create local entity: %v", err)
	}
	if err := db.Create(&foreignEntity).Error; err != nil {
		t.Fatalf("create foreign entity: %v", err)
	}
	foreignAttribute := models.EntityAttribute{EntityID: foreignEntity.ID, Name: "ID", ColumnName: "id", DataType: "bigint"}
	if err := db.Create(&foreignAttribute).Error; err != nil {
		t.Fatalf("create foreign attribute: %v", err)
	}

	entitySvc := NewEntityService(entityRepo, relationRepo)
	_, err := entitySvc.GetAttributes(foreignEntity.ID, 1)
	requireDomainErrorCode(t, err, "entity_not_found")
	_, err = entitySvc.UpdateAttribute(foreignAttribute.ID, localEntity.ID, 1, &models.UpdateEntityAttributeRequest{})
	requireDomainErrorCode(t, err, "attribute_not_found")
	err = entitySvc.DeleteAttribute(foreignAttribute.ID, localEntity.ID, 1)
	requireDomainErrorCode(t, err, "attribute_not_found")

	entityRelationSvc := NewEntityRelationService(relationRepo, entityRepo)
	_, err = entityRelationSvc.Create(1, &models.CreateEntityRelationRequest{
		SourceEntity: localEntity.ID, TargetEntity: foreignEntity.ID, RelationType: "one_to_many",
	})
	requireDomainErrorCode(t, err, "target_entity_not_found")

	localFact := models.LogicalTable{TenantID: 1, Name: "Local Fact", Code: "local_fact", TableType: "fact", Layer: "dwd", Status: "draft", CreatedBy: 1}
	foreignFact := models.LogicalTable{TenantID: 2, Name: "Foreign Fact", Code: "foreign_fact", TableType: "fact", Layer: "dwd", Status: "draft", CreatedBy: 1}
	foreignDimension := models.LogicalTable{TenantID: 2, Name: "Foreign Dimension", Code: "foreign_dimension", TableType: "dimension", Layer: "dwd", Status: "draft", CreatedBy: 1}
	for _, table := range []*models.LogicalTable{&localFact, &foreignFact, &foreignDimension} {
		if err := db.Create(table).Error; err != nil {
			t.Fatalf("create logical table %s: %v", table.Code, err)
		}
	}
	foreignField := models.LogicalField{TableID: foreignFact.ID, Name: "ID", ColumnName: "id", DataType: "bigint"}
	if err := db.Create(&foreignField).Error; err != nil {
		t.Fatalf("create foreign field: %v", err)
	}

	logicalTableSvc := NewLogicalTableService(
		tableRepo,
		entityRepo,
		repository.NewDWLayerRepository(db),
	)
	_, err = logicalTableSvc.GetFields(foreignFact.ID, 1)
	requireDomainErrorCode(t, err, "logical_table_not_found")
	_, err = logicalTableSvc.UpdateField(foreignField.ID, localFact.ID, 1, &models.UpdateLogicalFieldRequest{})
	requireDomainErrorCode(t, err, "logical_field_not_found")
	err = logicalTableSvc.DeleteField(foreignField.ID, localFact.ID, 1)
	requireDomainErrorCode(t, err, "logical_field_not_found")

	metricSvc := NewFactMetricService(metricRepo, tableRepo)
	_, err = metricSvc.ListMetrics(foreignFact.ID, 1)
	requireDomainErrorCode(t, err, "logical_table_not_found")
	_, err = metricSvc.AddMetric(foreignFact.ID, 1, 1, &models.CreateFactMetricMappingRequest{MetricID: 9})
	requireDomainErrorCode(t, err, "logical_table_not_found")

	tableRelationSvc := NewTableRelationService(tableRelationRepo, tableRepo)
	_, err = tableRelationSvc.AddDimensionRelation(localFact.ID, 1, &models.CreateTableRelationRequest{
		TargetTable: foreignDimension.ID, SourceField: 1, TargetField: 2,
	})
	requireDomainErrorCode(t, err, "dimension_table_not_found")
	_, err = tableRelationSvc.ListDimensionRelations(foreignFact.ID, 1)
	requireDomainErrorCode(t, err, "logical_table_not_found")
}
