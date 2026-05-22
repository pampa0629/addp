package datatype

import (
	"reflect"
	"testing"
	"time"
)

func TestTableInfoHelpers(t *testing.T) {
	info := &TableInfo{
		Fields: []FieldInfo{
			{Name: "id", Type: FieldTypeInt, PrimaryKey: true},
			{Name: "name", Type: FieldTypeString, Nullable: true},
		},
	}

	if got := info.FieldNames(); !reflect.DeepEqual(got, []string{"id", "name"}) {
		t.Fatalf("FieldNames() = %#v", got)
	}
	if field := info.GetField("name"); field == nil || field.Type != FieldTypeString {
		t.Fatalf("GetField(name) = %#v", field)
	}
	if info.HasField("missing") {
		t.Fatalf("HasField(missing) = true, want false")
	}
}

func TestTableInfoCloneDeepCopiesMutableFields(t *testing.T) {
	rowCount := int64(10)
	sizeBytes := int64(20)
	createdAt := time.Unix(100, 0)
	updatedAt := time.Unix(200, 0)
	info := &TableInfo{
		Name:      "cities",
		RowCount:  &rowCount,
		SizeBytes: &sizeBytes,
		CreatedAt: &createdAt,
		UpdatedAt: &updatedAt,
		Fields: []FieldInfo{
			{Name: "id", Type: FieldTypeInt},
		},
		PrimaryKey: []string{"id"},
	}

	cloned := info.Clone()
	if cloned == nil || cloned == info {
		t.Fatalf("Clone() = %#v", cloned)
	}
	cloned.Fields[0].Name = "changed"
	cloned.PrimaryKey[0] = "changed"
	*cloned.RowCount = 99
	*cloned.SizeBytes = 88
	*cloned.CreatedAt = time.Unix(300, 0)
	*cloned.UpdatedAt = time.Unix(400, 0)

	if info.Fields[0].Name != "id" || info.PrimaryKey[0] != "id" {
		t.Fatalf("original fields changed: %#v %#v", info.Fields, info.PrimaryKey)
	}
	if *info.RowCount != 10 || *info.SizeBytes != 20 {
		t.Fatalf("original counts changed: row=%d size=%d", *info.RowCount, *info.SizeBytes)
	}
	if !info.CreatedAt.Equal(time.Unix(100, 0)) || !info.UpdatedAt.Equal(time.Unix(200, 0)) {
		t.Fatalf("original timestamps changed: %v %v", info.CreatedAt, info.UpdatedAt)
	}
}
