package datatype

import (
	"reflect"
	"testing"
)

func TestDeclaredMapClonePayloadAndJSON(t *testing.T) {
	leaf := FieldInfo{Type: FieldTypeBigInt, Nullable: true}
	mapping := FieldInfo{Name: "mapping", Type: FieldTypeJSON, NativeType: "map", MapValue: &leaf}
	original := map[string]interface{}{"": int64(9007199254740993), "a.b": nil, "中文 ": int64(-9007199254740993), "__proto__": int64(0)}
	rows, err := RowsForJSON([]FieldInfo{mapping}, []map[string]interface{}{{"mapping": original}, {"mapping": nil}, {"mapping": map[string]interface{}{}}, {"mapping": "protected"}})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"": "9007199254740993", "a.b": nil, "中文 ": "-9007199254740993", "__proto__": "0"}
	if !reflect.DeepEqual(rows[0]["mapping"], want) || rows[1]["mapping"] != nil || len(rows[2]["mapping"].(map[string]interface{})) != 0 || rows[3]["mapping"] != "protected" {
		t.Fatal(rows)
	}
	if original[""] != int64(9007199254740993) {
		t.Fatal("mutated provider")
	}
	info := &TableInfo{Fields: []FieldInfo{mapping}}
	restored := TableInfoFromPayload(TableInfoPayload(info), "")
	if !reflect.DeepEqual(info, restored) {
		t.Fatal("map value declaration lost")
	}
	clone := info.Clone()
	clone.Fields[0].MapValue.Type = FieldTypeString
	if leaf.Type != FieldTypeBigInt {
		t.Fatal("map value clone aliases original")
	}
	enum := &EnumInfo{Name: "example.State", Symbols: []string{"z", "A", "a"}}
	info.Fields[0].MapValue = &FieldInfo{Type: FieldTypeArray, Element: &FieldInfo{Type: FieldTypeString, Enum: enum}}
	clone = info.Clone()
	clone.Fields[0].MapValue.Element.Enum.Symbols[0] = "changed"
	if enum.Symbols[0] != "z" {
		t.Fatal("deep map declaration aliases enum")
	}
	if _, err := RowsForJSON([]FieldInfo{mapping}, []map[string]interface{}{{"mapping": []interface{}{1}}}); err == nil {
		t.Fatal("map accepted array")
	}
}

func TestMapValuePayloadNormalizationMatchesNestedDeclarations(t *testing.T) {
	source := &TableInfo{Fields: []FieldInfo{{Name: "mapping", Type: FieldTypeJSON, MapValue: &FieldInfo{Type: FieldTypeJSON, NativeType: " record ", Fields: []FieldInfo{{Name: " literal.key ", Type: FieldTypeBigInt, NativeType: " long "}}}}}}
	restored := TableInfoFromPayload(TableInfoPayload(source), "")
	if restored.Fields[0].MapValue.NativeType != "record" || restored.Fields[0].MapValue.Fields[0].NativeType != "long" || restored.Fields[0].MapValue.Fields[0].Name != " literal.key " {
		t.Fatal("map value declaration normalization differs from record/array")
	}
}
