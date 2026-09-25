package service

import (
	"reflect"
	"testing"

	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"gorm.io/gorm"
)

func TestDimensionRelationOwnershipAndUpdates(t *testing.T) {
	testDimensionRelationOwnershipAndUpdates(t, setupLifecycleServiceTestDB(t), 1)
}

func TestPostgresDimensionRelationOwnershipAndUpdates(t *testing.T) {
	tx, tenantID := beginModelAggregatePostgresTransaction(t)
	testDimensionRelationOwnershipAndUpdates(t, tx, tenantID)
}

func testDimensionRelationOwnershipAndUpdates(t *testing.T, db *gorm.DB, tenantID int64) {
	t.Helper()
	layer := models.DWLayer{TenantID: tenantID, LayerCode: "relation_test", LayerName: "Relations", Version: 1}
	if err := db.Create(&layer).Error; err != nil {
		t.Fatal(err)
	}
	fact := models.LogicalTable{TenantID: tenantID, Name: "Participation", Code: "relation_fact", TableType: "fact", Layer: layer.LayerCode, Status: "draft", Version: 1, CreatedBy: tenantID, Materialization: models.JSONB{}}
	dimension := models.LogicalTable{TenantID: tenantID, Name: "Person", Code: "relation_dimension", TableType: "dimension", Layer: layer.LayerCode, Status: "approved", Version: 9, CreatedBy: tenantID, Materialization: models.JSONB{}}
	for _, table := range []*models.LogicalTable{&fact, &dimension} {
		if err := db.Create(table).Error; err != nil {
			t.Fatal(err)
		}
	}
	source := models.LogicalField{TableID: fact.ID, Name: "Person ID", ColumnName: "person_id", DataType: "string", FieldRole: "dimension_fk"}
	target := models.LogicalField{TableID: dimension.ID, Name: "Person ID", ColumnName: "person_id", DataType: "string", IsPK: true, FieldRole: "regular"}
	alternate := models.LogicalField{TableID: dimension.ID, Name: "Person code", ColumnName: "person_code", DataType: "string", FieldRole: "regular"}
	wrongType := models.LogicalField{TableID: dimension.ID, Name: "Age", ColumnName: "age", DataType: "int", FieldRole: "regular"}
	for _, field := range []*models.LogicalField{&source, &target, &alternate, &wrongType} {
		if err := db.Create(field).Error; err != nil {
			t.Fatal(err)
		}
	}
	tableRepo := repository.NewLogicalTableRepository(db)
	repo := repository.NewTableRelationRepository(db)
	svc := NewTableRelationService(repo, tableRepo)
	dimensionBefore, err := tableRepo.GetByID(dimension.ID, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	fieldsBefore, err := tableRepo.GetFields(dimension.ID)
	if err != nil {
		t.Fatal(err)
	}
	req := models.SaveTableRelationRequest{Version: fact.Version, SourceField: source.ID, TargetTable: dimension.ID, TargetField: target.ID, RelationType: "fk"}
	added, err := svc.AddDimensionRelation(fact.ID, tenantID, &req)
	if err != nil {
		t.Fatalf("draft fact can reference approved dimension: %v", err)
	}
	if added.Version != 2 {
		t.Fatalf("version = %d", added.Version)
	}
	created, err := repo.GetByID(added.Relation.ID, fact.ID, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{fact.ID, dimension.ID} {
		rows, err := svc.ListDimensionRelations(id, tenantID)
		if err != nil || len(rows) != 1 {
			t.Fatalf("outgoing/incoming list: %+v %v", rows, err)
		}
		row := rows[0]
		if row.SourceTableName != fact.Name || row.SourceTableCode != fact.Code || row.SourceFieldCode != source.ColumnName || row.TargetTableCode != dimension.Code || row.TargetFieldCode != target.ColumnName {
			t.Fatalf("incomplete relation details: %+v", row)
		}
	}
	req.Version = added.Version
	req.TargetField = alternate.ID
	_, err = svc.UpdateDimensionRelation(added.Relation.ID, fact.ID, tenantID, &req)
	requireDomainErrorCode(t, err, "table_relation_invalid") // FK must point to PK.
	req.TargetField = wrongType.ID
	req.RelationType = "join"
	_, err = svc.UpdateDimensionRelation(added.Relation.ID, fact.ID, tenantID, &req)
	requireDomainErrorCode(t, err, "table_relation_invalid")
	req.TargetField = alternate.ID
	updated, err := svc.UpdateDimensionRelation(added.Relation.ID, fact.ID, tenantID, &req)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Relation.ID != created.ID || !updated.Relation.CreatedAt.Equal(created.CreatedAt) || updated.Version != 3 || updated.Relation.TargetField != alternate.ID {
		t.Fatalf("update replaced identity or did not update mapping: %+v", updated)
	}
	_, err = svc.UpdateDimensionRelation(added.Relation.ID, fact.ID, tenantID, &req)
	requireDomainErrorCode(t, err, "resource_version_conflict")
	_, err = svc.RemoveDimensionRelation(added.Relation.ID, fact.ID, tenantID, req.Version)
	requireDomainErrorCode(t, err, "resource_version_conflict")
	req.Version = updated.Version
	_, err = svc.AddDimensionRelation(fact.ID, tenantID, &req)
	requireDomainErrorCode(t, err, "table_relation_conflict")
	// All operations are scoped to tenant and source aggregate.
	_, err = svc.ListDimensionRelations(dimension.ID, tenantID+100)
	requireDomainErrorCode(t, err, "logical_table_not_found")
	_, err = svc.UpdateDimensionRelation(added.Relation.ID, fact.ID, tenantID+100, &req)
	requireDomainErrorCode(t, err, "logical_table_not_found")
	req.SourceField = target.ID
	_, err = svc.UpdateDimensionRelation(added.Relation.ID, fact.ID, tenantID, &req)
	requireDomainErrorCode(t, err, "source_field_not_found")
	req.SourceField = source.ID
	req.TargetField = source.ID
	_, err = svc.UpdateDimensionRelation(added.Relation.ID, fact.ID, tenantID, &req)
	requireDomainErrorCode(t, err, "target_field_not_found")
	req.TargetField = alternate.ID
	// Source approval blocks every mutation, even if target is already approved.
	if err := db.Model(&fact).Update("status", "approved").Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.AddDimensionRelation(fact.ID, tenantID, &req)
	requireDomainErrorCode(t, err, "table_relation_state_conflict")
	_, err = svc.UpdateDimensionRelation(added.Relation.ID, fact.ID, tenantID, &req)
	requireDomainErrorCode(t, err, "table_relation_state_conflict")
	_, err = svc.RemoveDimensionRelation(added.Relation.ID, fact.ID, tenantID, req.Version)
	requireDomainErrorCode(t, err, "table_relation_state_conflict")
	if err := db.Model(&fact).Update("status", "draft").Error; err != nil {
		t.Fatal(err)
	}
	removed, err := svc.RemoveDimensionRelation(added.Relation.ID, fact.ID, tenantID, req.Version)
	if err != nil || removed.Version != 4 {
		t.Fatalf("remove approved dimension reference: %+v %v", removed, err)
	}
	rows, err := svc.ListDimensionRelations(dimension.ID, tenantID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("incoming references after deletion: %+v %v", rows, err)
	}
	dimensionAfter, err := tableRepo.GetByID(dimension.ID, tenantID)
	if err != nil {
		t.Fatal(err)
	}
	fieldsAfter, err := tableRepo.GetFields(dimension.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dimensionBefore, dimensionAfter) || !reflect.DeepEqual(fieldsBefore, fieldsAfter) {
		t.Fatal("relation mutation changed approved dimension or its field definitions")
	}
}
