package service

import (
	"reflect"
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

func boolPointer(value bool) *bool {
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
			version INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE model.entities (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL,
			domain_id INTEGER, name TEXT NOT NULL, code TEXT NOT NULL, description TEXT,
			status TEXT NOT NULL, created_by INTEGER NOT NULL, updated_by INTEGER,
			version INTEGER NOT NULL DEFAULT 1,
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
			version INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE model.logical_tables (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL,
			domain_id INTEGER, entity_id INTEGER, name TEXT NOT NULL, code TEXT NOT NULL,
			description TEXT, table_type TEXT NOT NULL, layer TEXT, status TEXT NOT NULL,
			grain_description TEXT, scd_type INTEGER, materialization TEXT,
			version INTEGER NOT NULL DEFAULT 1,
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
		`CREATE TABLE model.materialization_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL,
			code TEXT NOT NULL, name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
			version INTEGER NOT NULL DEFAULT 1, created_by INTEGER NOT NULL, updated_by INTEGER NOT NULL,
			created_at DATETIME, updated_at DATETIME
		)`,
		`CREATE TABLE model.materialization_group_members (
			group_id INTEGER NOT NULL, tenant_id INTEGER NOT NULL,
			logical_table_id INTEGER NOT NULL, position INTEGER NOT NULL,
			PRIMARY KEY (group_id, logical_table_id)
		)`,
		`CREATE TABLE model.standard_reference_guards (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant_id INTEGER NOT NULL,
			resource_type TEXT NOT NULL,
			resource_id INTEGER NOT NULL,
			state TEXT NOT NULL DEFAULT 'open',
			created_at DATETIME,
			updated_at DATETIME,
			UNIQUE (tenant_id, resource_type, resource_id)
		)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create lifecycle test table: %v", err)
		}
	}
	if err := db.Exec(`CREATE TABLE model.entity_model_revisions (tenant_id INTEGER PRIMARY KEY, revision INTEGER NOT NULL DEFAULT 1, updated_at DATETIME)`).Error; err != nil {
		t.Fatalf("create revision table: %v", err)
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
	if err := entityService.DeleteEntity(approved.ID, 1, approved.Version); err == nil {
		t.Fatal("delete approved entity error = nil")
	}
	_, err := entityService.ImportFromMermaid(1, 1, &models.MermaidImportRequest{
		MermaidCode: "erDiagram\n  product {\n    bigint id PK\n  }", Revision: 1,
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

	_, err := svc.ApproveEntity(entity.ID, 1, 1, entity.Version)
	requireDomainErrorCode(t, err, "entity_approval_attributes_required")

	attribute := models.EntityAttribute{EntityID: entity.ID, Name: "Title", ColumnName: "title", DataType: "string"}
	if err := db.Create(&attribute).Error; err != nil {
		t.Fatalf("create entity attribute: %v", err)
	}
	_, err = svc.ApproveEntity(entity.ID, 1, 1, entity.Version)
	requireDomainErrorCode(t, err, "entity_approval_primary_key_required")
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

	_, err := svc.ApproveLogicalTable(table.ID, 1, 1, table.Version)
	requireDomainErrorCode(t, err, "logical_table_approval_fields_required")

	field := models.LogicalField{TableID: table.ID, Name: "Name", ColumnName: "name", DataType: "string"}
	if err := db.Create(&field).Error; err != nil {
		t.Fatalf("create logical field: %v", err)
	}
	_, err = svc.ApproveLogicalTable(table.ID, 1, 1, table.Version)
	requireDomainErrorCode(t, err, "logical_table_approval_primary_key_required")
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
	_, err := metricService.AddMetric(fact.ID, 1, 1, &models.CreateFactMetricMappingRequest{Version: fact.Version, MetricID: 9})
	requireDomainErrorCode(t, err, "fact_metric_state_conflict")

	tableRelationService := NewTableRelationService(relationRepo, tableRepo)
	_, err = tableRelationService.AddDimensionRelation(fact.ID, 1, &models.CreateTableRelationRequest{
		Version: fact.Version, TargetTable: dimension.ID, SourceField: 1, TargetField: 2,
	})
	requireDomainErrorCode(t, err, "table_relation_state_conflict")
}

func TestTableRelationVersionConflictPrecedesLifecycleConflict(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	tableRepo := repository.NewLogicalTableRepository(db)
	relationRepo := repository.NewTableRelationRepository(db)
	fact := models.LogicalTable{
		TenantID: 1, Name: "Orders", Code: "orders", TableType: "fact",
		Status: "approved", CreatedBy: 1, Version: 2,
	}
	dimension := models.LogicalTable{
		TenantID: 1, Name: "Customer", Code: "customer", TableType: "dimension",
		Status: "draft", CreatedBy: 1, Version: 1,
	}
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

	_, err := NewTableRelationService(relationRepo, tableRepo).
		RemoveDimensionRelation(relation.ID, fact.ID, 1, 1)
	requireDomainErrorCode(t, err, "resource_version_conflict")

	var count int64
	if err := db.Model(&models.TableRelation{}).Where("id = ?", relation.ID).Count(&count).Error; err != nil {
		t.Fatalf("count table relation: %v", err)
	}
	if count != 1 {
		t.Fatalf("relation count = %d, want 1", count)
	}
}

func TestStaleVersionsDoNotMutateIndependentVersionSubjects(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	entityRepo := repository.NewEntityRepository(db)
	relationRepo := repository.NewEntityRelationRepository(db)
	tableRepo := repository.NewLogicalTableRepository(db)
	layerRepo := repository.NewDWLayerRepository(db)

	source := models.Entity{TenantID: 1, Name: "Order", Code: "order", Status: "draft", CreatedBy: 1, Version: 2}
	target := models.Entity{TenantID: 1, Name: "Customer", Code: "customer", Status: "draft", CreatedBy: 1, Version: 1}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("create source entity: %v", err)
	}
	if err := db.Create(&target).Error; err != nil {
		t.Fatalf("create target entity: %v", err)
	}
	relation := models.EntityRelation{
		TenantID: 1, SourceEntity: source.ID, TargetEntity: target.ID,
		RelationType: "one_to_many", Name: "places", Version: 2,
	}
	if err := db.Create(&relation).Error; err != nil {
		t.Fatalf("create entity relation: %v", err)
	}
	table := models.LogicalTable{
		TenantID: 1, Name: "Orders", Code: "orders", TableType: "fact",
		Status: "approved", CreatedBy: 1, Version: 2,
	}
	if err := db.Create(&table).Error; err != nil {
		t.Fatalf("create logical table: %v", err)
	}
	layer := models.DWLayer{TenantID: 1, LayerCode: "dwd", LayerName: "DWD", Version: 2}
	if err := db.Create(&layer).Error; err != nil {
		t.Fatalf("create dw layer: %v", err)
	}

	_, err := NewEntityService(entityRepo, relationRepo).UpdateEntity(source.ID, 1, 2, &models.UpdateEntityRequest{
		Version: 1, Name: "Changed",
	})
	requireDomainErrorCode(t, err, "resource_version_conflict")
	_, err = NewEntityRelationService(relationRepo, entityRepo).Update(relation.ID, 1, &models.UpdateEntityRelationRequest{
		Version: 1, SourceEntity: source.ID, TargetEntity: target.ID,
		RelationType: "one_to_one", Name: "changed",
	})
	requireDomainErrorCode(t, err, "resource_version_conflict")
	_, err = NewLogicalTableService(tableRepo, entityRepo, layerRepo).ReopenLogicalTable(table.ID, 1, 2, 1)
	requireDomainErrorCode(t, err, "resource_version_conflict")
	zero := 0
	_, err = NewDWLayerService(layerRepo).UpdateDWLayer(layer.ID, 1, &models.UpdateDWLayerRequest{
		Version: 1, LayerName: "Changed", SortOrder: &zero,
	})
	requireDomainErrorCode(t, err, "resource_version_conflict")

	reloadedSource, err := entityRepo.GetByID(source.ID, 1)
	if err != nil {
		t.Fatalf("reload source entity: %v", err)
	}
	if reloadedSource.Name != source.Name || reloadedSource.Version != 2 {
		t.Fatalf("source changed after stale update: %+v", reloadedSource)
	}
	reloadedRelation, err := relationRepo.GetByID(relation.ID, 1)
	if err != nil {
		t.Fatalf("reload entity relation: %v", err)
	}
	if reloadedRelation.RelationType != relation.RelationType || reloadedRelation.Name != relation.Name || reloadedRelation.Version != 2 {
		t.Fatalf("relation changed after stale update: %+v", reloadedRelation)
	}
	reloadedTable, err := tableRepo.GetByID(table.ID, 1)
	if err != nil {
		t.Fatalf("reload logical table: %v", err)
	}
	if reloadedTable.Status != "approved" || reloadedTable.Version != 2 {
		t.Fatalf("logical table changed after stale reopen: %+v", reloadedTable)
	}
	reloadedLayer, err := layerRepo.GetByID(layer.ID, 1)
	if err != nil {
		t.Fatalf("reload dw layer: %v", err)
	}
	if reloadedLayer.LayerName != layer.LayerName || reloadedLayer.Version != 2 {
		t.Fatalf("dw layer changed after stale update: %+v", reloadedLayer)
	}
}

func TestStaleAggregateVersionsDoNotMutateChildResources(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	entityRepo := repository.NewEntityRepository(db)
	relationRepo := repository.NewEntityRelationRepository(db)
	tableRepo := repository.NewLogicalTableRepository(db)

	entity := models.Entity{TenantID: 1, Name: "Order", Code: "order", Status: "draft", CreatedBy: 1, Version: 2}
	if err := db.Create(&entity).Error; err != nil {
		t.Fatalf("create entity: %v", err)
	}
	attribute := models.EntityAttribute{EntityID: entity.ID, Name: "ID", ColumnName: "id", DataType: "bigint"}
	if err := db.Create(&attribute).Error; err != nil {
		t.Fatalf("create attribute: %v", err)
	}
	table := models.LogicalTable{
		TenantID: 1, Name: "Orders", Code: "orders", TableType: "fact",
		Status: "draft", CreatedBy: 1, Version: 2,
	}
	if err := db.Create(&table).Error; err != nil {
		t.Fatalf("create logical table: %v", err)
	}
	field := models.LogicalField{TableID: table.ID, Name: "ID", ColumnName: "id", DataType: "bigint"}
	if err := db.Create(&field).Error; err != nil {
		t.Fatalf("create logical field: %v", err)
	}

	falseValue := false
	trueValue := true
	zero := 0
	_, err := NewEntityService(entityRepo, relationRepo).UpdateAttribute(attribute.ID, entity.ID, 1, &models.UpdateEntityAttributeRequest{
		Version: 1, Name: "Changed", ColumnName: "changed", DataType: "string",
		IsPK: &falseValue, Nullable: &trueValue, SortOrder: &zero,
	})
	requireDomainErrorCode(t, err, "resource_version_conflict")
	_, err = NewLogicalTableService(tableRepo, entityRepo, repository.NewDWLayerRepository(db)).UpdateField(
		field.ID,
		table.ID,
		1,
		&models.UpdateLogicalFieldRequest{
			Version: 1, Name: "Changed", ColumnName: "changed", DataType: "string",
			Nullable: &trueValue, IsPK: &falseValue, IsPartition: &falseValue,
			SortOrder: &zero, FieldRole: "regular",
		},
	)
	requireDomainErrorCode(t, err, "resource_version_conflict")

	attributes, err := entityRepo.GetAttributes(entity.ID)
	if err != nil {
		t.Fatalf("reload attributes: %v", err)
	}
	if len(attributes) != 1 || attributes[0].Name != attribute.Name || attributes[0].ColumnName != attribute.ColumnName {
		t.Fatalf("attribute changed after stale update: %+v", attributes)
	}
	reloadedField, err := tableRepo.GetFieldByID(field.ID, table.ID)
	if err != nil {
		t.Fatalf("reload logical field: %v", err)
	}
	if reloadedField.Name != field.Name || reloadedField.ColumnName != field.ColumnName {
		t.Fatalf("logical field changed after stale update: %+v", reloadedField)
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

	err := NewLogicalTableService(
		tableRepo,
		repository.NewEntityRepository(db),
		repository.NewDWLayerRepository(db),
	).DeleteLogicalTable(dimension.ID, 1, dimension.Version)
	requireDomainErrorCode(t, err, "logical_table_relation_state_conflict")
}

func TestLogicalTableDeleteAdvancesSurvivingFactVersion(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	tableRepo := repository.NewLogicalTableRepository(db)
	fact := models.LogicalTable{TenantID: 1, Name: "Orders", Code: "orders", TableType: "fact", Status: "draft", CreatedBy: 1, Version: 1}
	dimension := models.LogicalTable{TenantID: 1, Name: "Customer", Code: "customer", TableType: "dimension", Status: "draft", CreatedBy: 1, Version: 1}
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
	).DeleteLogicalTable(dimension.ID, 1, dimension.Version)
	if err != nil {
		t.Fatalf("delete dimension table: %v", err)
	}

	updatedFact, err := tableRepo.GetByID(fact.ID, 1)
	if err != nil {
		t.Fatalf("load surviving fact table: %v", err)
	}
	if updatedFact.Version != 2 {
		t.Fatalf("surviving fact version = %d, want 2", updatedFact.Version)
	}
	var relationCount int64
	if err := db.Model(&models.TableRelation{}).Where("id = ?", relation.ID).Count(&relationCount).Error; err != nil {
		t.Fatalf("count deleted relation: %v", err)
	}
	if relationCount != 0 {
		t.Fatalf("relation count = %d, want 0", relationCount)
	}
}

func TestMermaidExportImportPreservesEditableEntityModelFields(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	entityRepo := repository.NewEntityRepository(db)
	relationRepo := repository.NewEntityRelationRepository(db)
	svc := NewEntityService(entityRepo, relationRepo)
	domainID := int64(31)
	elementID := int64(41)
	source := models.Entity{
		TenantID: 1, DomainID: &domainID, Name: "Customer Display", Code: "customer",
		Description: "customer description", Status: "draft", CreatedBy: 9, Version: 1,
	}
	target := models.Entity{
		TenantID: 1, Name: "Order Display", Code: "order",
		Description: "order description", Status: "draft", CreatedBy: 9, Version: 1,
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("create source entity: %v", err)
	}
	if err := db.Create(&target).Error; err != nil {
		t.Fatalf("create target entity: %v", err)
	}
	attribute := models.EntityAttribute{
		EntityID: source.ID, ElementID: &elementID, Name: "Customer ID", ColumnName: "customer_id",
		DataType: "bigint", IsPK: true, Nullable: true, Description: "attribute description", SortOrder: 7,
	}
	if err := db.Create(&attribute).Error; err != nil {
		t.Fatalf("create attribute: %v", err)
	}
	relation := models.EntityRelation{
		TenantID: 1, SourceEntity: source.ID, TargetEntity: target.ID, RelationType: "one_to_many",
		Name: "places", Description: "relation description", Version: 1,
	}
	if err := db.Create(&relation).Error; err != nil {
		t.Fatalf("create relation: %v", err)
	}

	exported, err := svc.ExportToMermaid(1)
	if err != nil {
		t.Fatalf("export mermaid: %v", err)
	}
	parsed, err := ParseMermaidER(exported.MermaidCode)
	if err != nil {
		t.Fatalf("parse exported mermaid: %v", err)
	}
	var parsedSource *EntityDefinition
	for index := range parsed.Entities {
		if parsed.Entities[index].Name == source.Code {
			parsedSource = &parsed.Entities[index]
			break
		}
	}
	if parsedSource == nil || len(parsedSource.Attributes) != 1 || !parsedSource.Attributes[0].Nullable {
		t.Fatalf("parsed mermaid did not preserve nullable metadata: %+v", parsed.Entities)
	}
	if _, err := svc.ImportFromMermaid(1, 9, &models.MermaidImportRequest{
		MermaidCode: exported.MermaidCode,
		Revision:    exported.Revision,
	}); err != nil {
		t.Fatalf("import exported mermaid: %v", err)
	}

	reloadedSource, err := entityRepo.GetByCode(1, "customer")
	if err != nil {
		t.Fatalf("load imported source: %v", err)
	}
	if reloadedSource.Name != source.Name || !reflect.DeepEqual(reloadedSource.DomainID, source.DomainID) || reloadedSource.Description != source.Description {
		t.Fatalf("imported source = %+v, want editable fields from %+v", reloadedSource, source)
	}
	attributes, err := entityRepo.GetAttributes(reloadedSource.ID)
	if err != nil {
		t.Fatalf("load imported attributes: %v", err)
	}
	if len(attributes) != 1 {
		t.Fatalf("imported attribute count = %d, want 1", len(attributes))
	}
	actualAttribute := attributes[0]
	if actualAttribute.Name != attribute.Name || actualAttribute.ColumnName != attribute.ColumnName ||
		actualAttribute.DataType != attribute.DataType || actualAttribute.IsPK != attribute.IsPK ||
		actualAttribute.Nullable != attribute.Nullable || !reflect.DeepEqual(actualAttribute.ElementID, attribute.ElementID) ||
		actualAttribute.Description != attribute.Description || actualAttribute.SortOrder != attribute.SortOrder {
		t.Fatalf("imported attribute = %+v, want editable fields from %+v", actualAttribute, attribute)
	}
	relations, err := relationRepo.ListByTenantID(1)
	if err != nil {
		t.Fatalf("load imported relations: %v", err)
	}
	if len(relations) != 1 || relations[0].RelationType != relation.RelationType ||
		relations[0].Name != relation.Name || relations[0].Description != relation.Description {
		t.Fatalf("imported relations = %+v, want editable fields from %+v", relations, relation)
	}
}

func TestEntityWriteInvalidatesExportedMermaidRevision(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	entityRepo := repository.NewEntityRepository(db)
	svc := NewEntityService(entityRepo, repository.NewEntityRelationRepository(db))
	entity := models.Entity{
		TenantID: 1, Name: "Customer", Code: "customer", Description: "before",
		Status: "draft", CreatedBy: 1, Version: 1,
	}
	if err := db.Create(&entity).Error; err != nil {
		t.Fatalf("create entity: %v", err)
	}

	exported, err := svc.ExportToMermaid(1)
	if err != nil {
		t.Fatalf("export mermaid: %v", err)
	}
	updated, err := svc.UpdateEntity(entity.ID, 1, 2, &models.UpdateEntityRequest{
		Version: entity.Version, Name: entity.Name, Description: "after",
	})
	if err != nil {
		t.Fatalf("update entity: %v", err)
	}
	if updated.Description != "after" {
		t.Fatalf("updated description = %q, want after", updated.Description)
	}

	_, err = svc.ImportFromMermaid(1, 3, &models.MermaidImportRequest{
		MermaidCode: exported.MermaidCode,
		Revision:    exported.Revision,
	})
	requireDomainErrorCode(t, err, "resource_version_conflict")
	reloaded, err := entityRepo.GetByID(entity.ID, 1)
	if err != nil {
		t.Fatalf("reload entity: %v", err)
	}
	if reloaded.Description != "after" || reloaded.Version != 2 {
		t.Fatalf("entity changed after rejected import: %+v", reloaded)
	}
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
		Version: table.Version, EntityID: &foreignEntity.ID, Name: "Orders", TableType: "entity", Layer: "dwd", SCDType: intPointer(0), Materialization: map[string]interface{}{},
	})
	requireDomainErrorCode(t, err, "entity_not_found")

	_, err = svc.UpdateLogicalTable(table.ID, 1, 1, &models.UpdateLogicalTableRequest{
		Version: table.Version, Name: "Orders", TableType: "entity", Layer: "missing", SCDType: intPointer(0), Materialization: map[string]interface{}{},
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
		Version: entity.Version, Name: "Order", Description: "updated",
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
		Version: table.Version, Name: "Order Table", TableType: "entity", Layer: "dwd", SCDType: intPointer(0), Materialization: map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("update logical table: %v", err)
	}
	if updatedTable.DomainID != nil || updatedTable.EntityID != nil {
		t.Fatalf("references = domain:%v entity:%v, want nil", updatedTable.DomainID, updatedTable.EntityID)
	}
}

func TestLogicalTableWritesCanonicalizeUnpartitionedMaterialization(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	layer := models.DWLayer{TenantID: 1, LayerCode: "dwd", LayerName: "DWD", Version: 1}
	if err := db.Create(&layer).Error; err != nil {
		t.Fatalf("create layer: %v", err)
	}
	svc := NewLogicalTableService(
		repository.NewLogicalTableRepository(db),
		repository.NewEntityRepository(db),
		repository.NewDWLayerRepository(db),
	)
	table, err := svc.CreateLogicalTable(&models.CreateLogicalTableRequest{
		Name: "Orders", Code: "orders", TableType: "entity", Layer: "dwd",
		Materialization: map[string]interface{}{
			"target_parent_locator": " addp://engine/2/path/public?type=schema ",
			"target_name":           " orders ", "partition_by": "", "partition_type": "range",
		},
	}, 1, 1)
	if err != nil {
		t.Fatalf("create logical table: %v", err)
	}
	if _, exists := table.Materialization["partition_by"]; exists {
		t.Fatalf("created materialization retains partition_by: %#v", table.Materialization)
	}
	if _, exists := table.Materialization["partition_type"]; exists {
		t.Fatalf("created materialization retains partition_type: %#v", table.Materialization)
	}
	if table.Materialization["target_name"] != "orders" {
		t.Fatalf("created materialization target is not normalized: %#v", table.Materialization)
	}

	updated, err := svc.UpdateLogicalTable(table.ID, 1, 1, &models.UpdateLogicalTableRequest{
		Version: table.Version, Name: table.Name, TableType: table.TableType, Layer: table.Layer,
		SCDType: intPointer(0), Materialization: map[string]interface{}{
			"target_parent_locator": "addp://engine/2/path/public?type=schema",
			"target_name":           "orders", "partition_by": "   ", "partition_type": "list",
		},
	})
	if err != nil {
		t.Fatalf("update logical table: %v", err)
	}
	if _, exists := updated.Materialization["partition_by"]; exists {
		t.Fatalf("updated materialization retains partition_by: %#v", updated.Materialization)
	}
	if _, exists := updated.Materialization["partition_type"]; exists {
		t.Fatalf("updated materialization retains partition_type: %#v", updated.Materialization)
	}
	reloaded, err := repository.NewLogicalTableRepository(db).GetByID(table.ID, 1)
	if err != nil {
		t.Fatalf("reload logical table: %v", err)
	}
	if _, exists := reloaded.Materialization["partition_type"]; exists {
		t.Fatalf("persisted materialization is not canonical: %#v", reloaded.Materialization)
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
	_, err = entitySvc.UpdateAttribute(foreignAttribute.ID, localEntity.ID, 1, &models.UpdateEntityAttributeRequest{
		Version: localEntity.Version, Name: "ID", ColumnName: "id", DataType: "bigint", IsPK: boolPointer(false), Nullable: boolPointer(true), SortOrder: intPointer(0),
	})
	requireDomainErrorCode(t, err, "attribute_not_found")
	_, err = entitySvc.DeleteAttribute(foreignAttribute.ID, localEntity.ID, 1, localEntity.Version)
	requireDomainErrorCode(t, err, "attribute_not_found")

	entityRelationSvc := NewEntityRelationService(relationRepo, entityRepo)
	_, err = entityRelationSvc.Create(1, &models.CreateEntityRelationRequest{
		SourceEntity: localEntity.ID, TargetEntity: foreignEntity.ID, RelationType: "one_to_many",
	})
	requireDomainErrorCode(t, err, "entity_relation_target_not_found")

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
	_, err = logicalTableSvc.UpdateField(foreignField.ID, localFact.ID, 1, &models.UpdateLogicalFieldRequest{
		Version: localFact.Version, Name: "ID", ColumnName: "id", DataType: "bigint", Nullable: boolPointer(true), IsPK: boolPointer(false), IsPartition: boolPointer(false), SortOrder: intPointer(0), FieldRole: "regular",
	})
	requireDomainErrorCode(t, err, "logical_field_not_found")
	_, err = logicalTableSvc.DeleteField(foreignField.ID, localFact.ID, 1, localFact.Version)
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
	requireDomainErrorCode(t, err, "logical_table_not_found")
	_, err = tableRelationSvc.ListDimensionRelations(foreignFact.ID, 1)
	requireDomainErrorCode(t, err, "logical_table_not_found")
}
