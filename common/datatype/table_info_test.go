package datatype

import (
	"reflect"
	"testing"
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
