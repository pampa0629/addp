package plugin

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestFieldInfoFromNativePreservesNativeType(t *testing.T) {
	field := FieldInfoFromNative("id", "int4", false, true, "identifier")

	if field.Name != "id" || field.Type != datatype.FieldTypeUnknown || field.NativeType != "int4" {
		t.Fatalf("field identity = %#v", field)
	}
	if !field.PrimaryKey || field.Nullable || field.Comment != "identifier" {
		t.Fatalf("field facts = %#v", field)
	}
}

func TestNormalizeFieldInfosFillsNativeTypeAndDropsEmptyNames(t *testing.T) {
	fields := NormalizeFieldInfos([]datatype.FieldInfo{
		{Name: " id ", Type: datatype.FieldTypeInt, Nullable: false},
		{Name: "", NativeType: "text"},
		{Name: "name", NativeType: "string", Nullable: true},
	})

	if len(fields) != 2 {
		t.Fatalf("fields = %#v, want two fields", fields)
	}
	if fields[0].Name != "id" || fields[0].NativeType != "int" || fields[0].Type != datatype.FieldTypeInt {
		t.Fatalf("first field = %#v", fields[0])
	}
	if fields[1].Name != "name" || fields[1].NativeType != "string" || fields[1].Type != datatype.FieldTypeString {
		t.Fatalf("second field = %#v", fields[1])
	}
}
