package datatype

import (
	"reflect"
	"testing"
)

func TestEnumCloneAndPayloadPreserveDeclaration(t *testing.T) {
	e := &EnumInfo{Name: "example.Status", Symbols: []string{"z", "A", "a"}}
	info := &TableInfo{Fields: []FieldInfo{{Name: "status", Type: FieldTypeString, NativeType: "enum", Enum: e}, {Name: "object", Type: FieldTypeJSON, Fields: []FieldInfo{{Name: "status", Type: FieldTypeString, Enum: e}}}, {Name: "array", Type: FieldTypeArray, ElementType: FieldTypeString, Element: &FieldInfo{Type: FieldTypeString, Enum: e}}}}
	cloned := info.Clone()
	restored := TableInfoFromPayload(TableInfoPayload(info), "")
	if !reflect.DeepEqual(info, restored) {
		t.Fatalf("enum payload lost: %#v", restored)
	}
	for _, f := range []*FieldInfo{&cloned.Fields[0], &cloned.Fields[1].Fields[0], cloned.Fields[2].Element} {
		f.Enum.Name = "changed"
		f.Enum.Symbols[0] = "changed"
	}
	if e.Name != "example.Status" || e.Symbols[0] != "z" {
		t.Fatal("enum declaration shares mutable clone state")
	}
}
