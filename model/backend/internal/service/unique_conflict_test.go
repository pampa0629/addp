package service

import (
	"testing"

	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

func TestAggregateChildWritesReturnStableUniqueConflictCodes(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	for _, ddl := range []string{
		"CREATE UNIQUE INDEX model.uq_test_entity_attribute_column ON entity_attributes(entity_id, column_name)",
		"CREATE UNIQUE INDEX model.uq_test_logical_field_column ON logical_fields(table_id, column_name)",
		"CREATE UNIQUE INDEX model.uq_test_entity_relation_identity ON entity_relations(tenant_id, source_entity, target_entity, relation_type, name)",
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create unique index: %v", err)
		}
	}

	entityRepo := repository.NewEntityRepository(db)
	relationRepo := repository.NewEntityRelationRepository(db)
	entityService := NewEntityService(entityRepo, relationRepo)
	source := models.Entity{TenantID: 1, Name: "Order", Code: "order", Status: "draft", CreatedBy: 1}
	target := models.Entity{TenantID: 1, Name: "Customer", Code: "customer", Status: "draft", CreatedBy: 1}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("create source entity: %v", err)
	}
	if err := db.Create(&target).Error; err != nil {
		t.Fatalf("create target entity: %v", err)
	}
	attributeRequest := &models.CreateEntityAttributeRequest{Name: "ID", ColumnName: "id", DataType: "bigint"}
	if _, err := entityService.CreateAttribute(source.ID, 1, attributeRequest); err != nil {
		t.Fatalf("create first attribute: %v", err)
	}
	_, err := entityService.CreateAttribute(source.ID, 1, attributeRequest)
	requireDomainErrorCode(t, err, "entity_attribute_column_conflict")

	tableRepo := repository.NewLogicalTableRepository(db)
	logicalTableService := NewLogicalTableService(tableRepo, entityRepo, repository.NewDWLayerRepository(db))
	table := models.LogicalTable{TenantID: 1, Name: "Orders", Code: "orders", TableType: "fact", Status: "draft", CreatedBy: 1}
	if err := db.Create(&table).Error; err != nil {
		t.Fatalf("create logical table: %v", err)
	}
	fieldRequest := &models.CreateLogicalFieldRequest{Name: "ID", ColumnName: "id", DataType: "bigint"}
	if _, err := logicalTableService.CreateField(table.ID, 1, fieldRequest); err != nil {
		t.Fatalf("create first logical field: %v", err)
	}
	_, err = logicalTableService.CreateField(table.ID, 1, fieldRequest)
	requireDomainErrorCode(t, err, "logical_field_column_conflict")

	entityRelationService := NewEntityRelationService(relationRepo, entityRepo)
	relationRequest := &models.CreateEntityRelationRequest{
		SourceEntity: source.ID, TargetEntity: target.ID, RelationType: "one_to_many", Name: "places",
	}
	if _, err := entityRelationService.Create(1, relationRequest); err != nil {
		t.Fatalf("create first entity relation: %v", err)
	}
	_, err = entityRelationService.Create(1, relationRequest)
	requireDomainErrorCode(t, err, "entity_relation_conflict")
}

func TestAggregateChildUpdatesReturnStableUniqueConflictCodes(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	for _, ddl := range []string{
		"CREATE UNIQUE INDEX model.uq_test_update_attribute_column ON entity_attributes(entity_id, column_name)",
		"CREATE UNIQUE INDEX model.uq_test_update_field_column ON logical_fields(table_id, column_name)",
		"CREATE UNIQUE INDEX model.uq_test_update_relation_identity ON entity_relations(tenant_id, source_entity, target_entity, relation_type, name)",
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("create unique index: %v", err)
		}
	}

	entityRepo := repository.NewEntityRepository(db)
	relationRepo := repository.NewEntityRelationRepository(db)
	entityService := NewEntityService(entityRepo, relationRepo)
	source := models.Entity{TenantID: 1, Name: "Order", Code: "order", Status: "draft", CreatedBy: 1}
	target := models.Entity{TenantID: 1, Name: "Customer", Code: "customer", Status: "draft", CreatedBy: 1}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("create source entity: %v", err)
	}
	if err := db.Create(&target).Error; err != nil {
		t.Fatalf("create target entity: %v", err)
	}
	firstAttribute, err := entityService.CreateAttribute(source.ID, 1, &models.CreateEntityAttributeRequest{Name: "ID", ColumnName: "id", DataType: "bigint"})
	if err != nil {
		t.Fatalf("create first attribute: %v", err)
	}
	secondAttribute, err := entityService.CreateAttribute(source.ID, 1, &models.CreateEntityAttributeRequest{Name: "Code", ColumnName: "code", DataType: "string"})
	if err != nil {
		t.Fatalf("create second attribute: %v", err)
	}
	_ = firstAttribute
	falseValue := false
	trueValue := true
	zero := 0
	_, err = entityService.UpdateAttribute(secondAttribute.ID, source.ID, 1, &models.UpdateEntityAttributeRequest{
		Name: "Code", ColumnName: "id", DataType: "string", IsPK: &falseValue, Nullable: &trueValue, SortOrder: &zero,
	})
	requireDomainErrorCode(t, err, "entity_attribute_column_conflict")

	tableRepo := repository.NewLogicalTableRepository(db)
	logicalTableService := NewLogicalTableService(tableRepo, entityRepo, repository.NewDWLayerRepository(db))
	table := models.LogicalTable{TenantID: 1, Name: "Orders", Code: "orders", TableType: "fact", Status: "draft", CreatedBy: 1}
	if err := db.Create(&table).Error; err != nil {
		t.Fatalf("create logical table: %v", err)
	}
	if _, err := logicalTableService.CreateField(table.ID, 1, &models.CreateLogicalFieldRequest{Name: "ID", ColumnName: "id", DataType: "bigint"}); err != nil {
		t.Fatalf("create first logical field: %v", err)
	}
	secondField, err := logicalTableService.CreateField(table.ID, 1, &models.CreateLogicalFieldRequest{Name: "Code", ColumnName: "code", DataType: "string"})
	if err != nil {
		t.Fatalf("create second logical field: %v", err)
	}
	_, err = logicalTableService.UpdateField(secondField.ID, table.ID, 1, &models.UpdateLogicalFieldRequest{
		Name: "Code", ColumnName: "id", DataType: "string", Nullable: &trueValue, IsPK: &falseValue,
		IsPartition: &falseValue, SortOrder: &zero, FieldRole: "regular",
	})
	requireDomainErrorCode(t, err, "logical_field_column_conflict")

	entityRelationService := NewEntityRelationService(relationRepo, entityRepo)
	if _, err := entityRelationService.Create(1, &models.CreateEntityRelationRequest{
		SourceEntity: source.ID, TargetEntity: target.ID, RelationType: "one_to_many", Name: "places",
	}); err != nil {
		t.Fatalf("create first entity relation: %v", err)
	}
	secondRelation, err := entityRelationService.Create(1, &models.CreateEntityRelationRequest{
		SourceEntity: source.ID, TargetEntity: target.ID, RelationType: "one_to_many", Name: "owns",
	})
	if err != nil {
		t.Fatalf("create second entity relation: %v", err)
	}
	_, err = entityRelationService.Update(secondRelation.ID, 1, &models.UpdateEntityRelationRequest{RelationType: "one_to_many", Name: "places"})
	requireDomainErrorCode(t, err, "entity_relation_conflict")
}
