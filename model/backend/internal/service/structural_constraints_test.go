package service

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
)

func TestStructuralConstraintsKeepCompositeKeyAndOnlyOutgoingForeignKeys(t *testing.T) {
	fields := []models.LogicalField{
		{ID: 1, ColumnName: "person_id", IsPK: true, Nullable: true},
		{ID: 2, ColumnName: "activity_id", IsPK: true, Nullable: false},
		{ID: 3, ColumnName: "member_status", Nullable: false},
		{ID: 4, ColumnName: "nickname", Nullable: true},
	}
	relations := []models.TableRelationDetail{
		{ID: 11, SourceTable: 10, SourceField: 1, TargetTable: 20, RelationType: "fk"},
		{ID: 12, SourceTable: 10, SourceField: 2, TargetTable: 30, RelationType: "join"},
		{ID: 13, SourceTable: 40, TargetTable: 10, RelationType: "fk"},
	}
	got := deriveStructuralConstraints(10, fields, relations)
	key := []models.LogicalConstraintField{{FieldID: 1, ColumnName: "person_id"}, {FieldID: 2, ColumnName: "activity_id"}}
	if !reflect.DeepEqual(got.PrimaryKey, key) {
		t.Fatalf("composite key = %#v", got.PrimaryKey)
	}
	if len(got.RequiredFields) != 3 || got.RequiredFields[2].FieldID != 3 {
		t.Fatalf("required = %#v", got.RequiredFields)
	}
	if len(got.ForeignKeys) != 1 || got.ForeignKeys[0].ID != 11 {
		t.Fatalf("foreign keys = %#v", got.ForeignKeys)
	}
	if !fields[0].Nullable {
		t.Fatal("read-only projection changed source field")
	}
	empty, err := json.Marshal(deriveStructuralConstraints(10, nil, nil))
	if err != nil || string(empty) != `{"primary_key":[],"required_fields":[],"foreign_keys":[]}` {
		t.Fatalf("empty = %s, error = %v", empty, err)
	}
}

func TestLogicalTableDetailDerivesConstraintsWithoutCrossTenantAccess(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	table := models.LogicalTable{ID: 10, TenantID: 7, Name: "Participation", Code: "participation", TableType: "fact", Status: "draft", Version: 3}
	if err := db.Create(&table).Error; err != nil {
		t.Fatal(err)
	}
	for _, field := range []models.LogicalField{
		{ID: 1, TableID: 10, Name: "Person", ColumnName: "person_id", DataType: "string", IsPK: true, SortOrder: 1},
		{ID: 2, TableID: 10, Name: "Activity", ColumnName: "activity_id", DataType: "string", IsPK: true, SortOrder: 2},
	} {
		if err := db.Create(&field).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := NewLogicalTableService(repository.NewLogicalTableRepository(db), nil)
	detail, err := svc.GetLogicalTable(10, 7)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Version != 3 || len(detail.StructuralConstraints.PrimaryKey) != 2 {
		t.Fatalf("detail = %#v", detail)
	}
	if _, err := svc.GetLogicalTable(10, 8); err == nil {
		t.Fatal("cross-tenant detail allowed")
	}
	var stored models.LogicalTable
	if err := db.First(&stored, 10).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Version != 3 || stored.Status != "draft" {
		t.Fatal("reading constraints mutated model")
	}
}

func TestModelFieldsPreserveExplicitNotNull(t *testing.T) {
	db := setupLifecycleServiceTestDB(t)
	field := models.LogicalField{TableID: 10, Name: "Member status", ColumnName: "member_status", DataType: "string", Nullable: false}
	if err := db.Create(&field).Error; err != nil {
		t.Fatal(err)
	}
	var stored models.LogicalField
	if err := db.First(&stored, field.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Nullable {
		t.Fatal("logical field nullable=false was replaced by the ORM default")
	}
	attribute := models.EntityAttribute{EntityID: 20, Name: "Person ID", ColumnName: "person_id", DataType: "string", Nullable: false}
	if err := db.Create(&attribute).Error; err != nil {
		t.Fatal(err)
	}
	var storedAttribute models.EntityAttribute
	if err := db.First(&storedAttribute, attribute.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedAttribute.Nullable {
		t.Fatal("entity attribute nullable=false was replaced by the ORM default")
	}
}
