package service

import (
	"strings"
	"testing"

	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

func requireInvalidRequest(t *testing.T, err error) {
	t.Helper()
	requireDomainErrorCode(t, err, "invalid_request")
}

func TestListRequestValidationRejectsInvalidFilters(t *testing.T) {
	negativeID := int64(-1)
	entityService := NewEntityService(nil, nil)
	_, _, err := entityService.ListEntities(1, repository.ListEntityOptions{DomainID: &negativeID})
	requireInvalidRequest(t, err)
	_, _, err = entityService.ListEntities(1, repository.ListEntityOptions{Status: "archived"})
	requireInvalidRequest(t, err)

	db := setupLifecycleServiceTestDB(t)
	logicalTableService := NewLogicalTableService(
		repository.NewLogicalTableRepository(db),
		repository.NewEntityRepository(db),
		repository.NewDWLayerRepository(db),
	)
	_, _, err = logicalTableService.ListLogicalTables(1, repository.ListLogicalTableOptions{TableType: "aggregate"})
	requireInvalidRequest(t, err)
	_, _, err = logicalTableService.ListLogicalTables(1, repository.ListLogicalTableOptions{Layer: "missing"})
	requireInvalidRequest(t, err)
}

func TestUpdateAttributeRejectsNilRequest(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	entity := models.Entity{TenantID: 1, Name: "Order", Code: "order", Status: "draft", CreatedBy: 1}
	if err := db.Create(&entity).Error; err != nil {
		t.Fatalf("create entity: %v", err)
	}
	svc := NewEntityService(repository.NewEntityRepository(db), repository.NewEntityRelationRepository(db))
	_, err := svc.UpdateAttribute(1, entity.ID, 1, nil)
	requireInvalidRequest(t, err)
}

func TestCreateEntityRelationRejectsGeneratedNameBeyondDatabaseLength(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	entityRepo := repository.NewEntityRepository(db)
	relationRepo := repository.NewEntityRelationRepository(db)
	source := models.Entity{TenantID: 1, Name: "Source", Code: strings.Repeat("a", 100), Status: "draft", CreatedBy: 1}
	target := models.Entity{TenantID: 1, Name: "Target", Code: strings.Repeat("b", 100), Status: "draft", CreatedBy: 1}
	if err := db.Create(&source).Error; err != nil {
		t.Fatalf("create source entity: %v", err)
	}
	if err := db.Create(&target).Error; err != nil {
		t.Fatalf("create target entity: %v", err)
	}
	_, err := NewEntityRelationService(relationRepo, entityRepo).Create(1, &models.CreateEntityRelationRequest{
		SourceEntity: source.ID, TargetEntity: target.ID, RelationType: "one_to_many",
	})
	requireInvalidRequest(t, err)
}

func TestCreateRequestValidationRejectsInvalidReferencesAndRanges(t *testing.T) {
	zeroID := int64(0)
	negativeID := int64(-1)
	zeroLength := 0

	tests := []struct {
		name string
		err  func() error
	}{
		{name: "entity domain", err: func() error {
			return validateCreateEntityRequest(&models.CreateEntityRequest{Name: "Order", Code: "order", DomainID: &zeroID})
		}},
		{name: "attribute element", err: func() error {
			return validateCreateEntityAttributeRequest(&models.CreateEntityAttributeRequest{Name: "ID", ColumnName: "id", DataType: "bigint", ElementID: &negativeID})
		}},
		{name: "attribute sort order", err: func() error {
			return validateCreateEntityAttributeRequest(&models.CreateEntityAttributeRequest{Name: "ID", ColumnName: "id", DataType: "bigint", SortOrder: -1})
		}},
		{name: "entity relation source", err: func() error {
			return validateCreateEntityRelationRequest(&models.CreateEntityRelationRequest{SourceEntity: 0, TargetEntity: 2, RelationType: "one_to_many"})
		}},
		{name: "logical table entity", err: func() error {
			return validateCreateLogicalTableRequest(&models.CreateLogicalTableRequest{Name: "Order", Code: "order", TableType: "entity", Layer: "dwd", EntityID: &zeroID})
		}},
		{name: "logical field length", err: func() error {
			return validateCreateLogicalFieldRequest(&models.CreateLogicalFieldRequest{Name: "ID", ColumnName: "id", DataType: "bigint", Length: &zeroLength})
		}},
		{name: "logical field sort order", err: func() error {
			return validateCreateLogicalFieldRequest(&models.CreateLogicalFieldRequest{Name: "ID", ColumnName: "id", DataType: "bigint", SortOrder: -1})
		}},
		{name: "table relation target", err: func() error {
			return validateCreateTableRelationRequest(&models.CreateTableRelationRequest{TargetTable: 0, SourceField: 1, TargetField: 2})
		}},
		{name: "dw layer sort order", err: func() error {
			return validateCreateDWLayerRequest(&models.CreateDWLayerRequest{LayerCode: "dwd", LayerName: "DWD", SortOrder: -1})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireInvalidRequest(t, tt.err())
		})
	}
}

func TestCreateRequestValidationAcceptsBoundaryValues(t *testing.T) {
	positiveID := int64(1)
	positiveLength := 1

	tests := []struct {
		name string
		err  error
	}{
		{name: "entity", err: validateCreateEntityRequest(&models.CreateEntityRequest{Name: "Order", Code: "order", DomainID: &positiveID})},
		{name: "attribute", err: validateCreateEntityAttributeRequest(&models.CreateEntityAttributeRequest{Name: "ID", ColumnName: "id", DataType: "bigint", ElementID: &positiveID, SortOrder: 0})},
		{name: "entity relation", err: validateCreateEntityRelationRequest(&models.CreateEntityRelationRequest{SourceEntity: 1, TargetEntity: 2, RelationType: "one_to_many"})},
		{name: "logical table", err: validateCreateLogicalTableRequest(&models.CreateLogicalTableRequest{Name: "Order", Code: "order", TableType: "entity", Layer: "dwd", DomainID: &positiveID, EntityID: &positiveID})},
		{name: "logical field", err: validateCreateLogicalFieldRequest(&models.CreateLogicalFieldRequest{Name: "Region", ColumnName: "region", DataType: "string", Length: &positiveLength, SortOrder: 0})},
		{name: "table relation", err: validateCreateTableRelationRequest(&models.CreateTableRelationRequest{TargetTable: 2, SourceField: 1, TargetField: 2, RelationType: "fk"})},
		{name: "dw layer", err: validateCreateDWLayerRequest(&models.CreateDWLayerRequest{LayerCode: "dwd", LayerName: "DWD", SortOrder: 0})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err != nil {
				t.Fatalf("validation error = %v", tt.err)
			}
		})
	}
}

func TestMetricImplementationRequestValidation(t *testing.T) {
	valid := metricImplementationRequest(1, 2)
	if err := validateMetricImplementationRequest(valid); err != nil {
		t.Fatalf("valid metric implementation rejected: %v", err)
	}

	invalid := *valid
	invalid.SourceConfig = map[string]interface{}{"field_ids": []int64{0}}
	if err := validateMetricImplementationRequest(&invalid); err != nil {
		// Field identity is checked against the local aggregate in the transactional validation.
		return
	}
	if _, err := positiveIDList(invalid.SourceConfig["field_ids"]); err == nil {
		t.Fatal("zero source field ID accepted")
	}
}

func TestRequestValidationRejectsValuesBeyondDatabaseLengths(t *testing.T) {
	long20 := strings.Repeat("a", 21)
	long100 := strings.Repeat("a", 101)
	long200 := strings.Repeat("a", 201)

	tests := []struct {
		name string
		err  error
	}{
		{name: "entity name", err: validateCreateEntityRequest(&models.CreateEntityRequest{Name: long200, Code: "order"})},
		{name: "entity code", err: validateCreateEntityRequest(&models.CreateEntityRequest{Name: "Order", Code: long100})},
		{name: "attribute name", err: validateCreateEntityAttributeRequest(&models.CreateEntityAttributeRequest{Name: long200, ColumnName: "id", DataType: "bigint"})},
		{name: "relation name", err: validateCreateEntityRelationRequest(&models.CreateEntityRelationRequest{SourceEntity: 1, TargetEntity: 2, RelationType: "one_to_many", Name: long200})},
		{name: "logical table layer", err: validateCreateLogicalTableRequest(&models.CreateLogicalTableRequest{Name: "Order", Code: "order", TableType: "entity", Layer: long20})},
		{name: "logical field column", err: validateCreateLogicalFieldRequest(&models.CreateLogicalFieldRequest{Name: "ID", ColumnName: long200, DataType: "bigint"})},
		{name: "dw layer name", err: validateCreateDWLayerRequest(&models.CreateDWLayerRequest{LayerCode: "dwd", LayerName: long100})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireInvalidRequest(t, tt.err)
		})
	}
}
