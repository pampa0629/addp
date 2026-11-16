package datatype

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"
)

func TestFixedClonePayloadAndPublicBytes(t *testing.T) {
	f := &FixedInfo{Name: "example.Token4", Size: 4}
	info := &TableInfo{Fields: []FieldInfo{
		{Name: "token", Type: FieldTypeBytes, NativeType: "fixed", Nullable: true, Fixed: f},
		{Name: "object", Type: FieldTypeJSON, Fields: []FieldInfo{{Name: "token", Type: FieldTypeBytes, Fixed: f}}},
		{Name: "array", Type: FieldTypeArray, ElementType: FieldTypeBytes, Element: &FieldInfo{Type: FieldTypeBytes, Fixed: f}},
		{Name: "map", Type: FieldTypeJSON, MapValue: &FieldInfo{Type: FieldTypeBytes, Fixed: f}},
	}}
	if got := TableInfoFromPayload(TableInfoPayload(info), ""); !reflect.DeepEqual(info, got) {
		t.Fatalf("fixed payload lost: %#v", got)
	}
	cloned := info.Clone()
	for _, field := range []*FieldInfo{&cloned.Fields[0], &cloned.Fields[1].Fields[0], cloned.Fields[2].Element, cloned.Fields[3].MapValue} {
		field.Fixed.Name = "changed"
		field.Fixed.Size = 8
	}
	if f.Name != "example.Token4" || f.Size != 4 {
		t.Fatal("clone shares fixed declaration")
	}
	raw := []byte{0, 255, 128, 0}
	values := []map[string]interface{}{{"token": raw, "object": map[string]interface{}{"token": raw}, "array": []interface{}{nil, raw}, "map": map[string]interface{}{"中文 ": raw}}, {"token": nil}, {"token": "protected"}}
	rows, err := RowsForJSON(info.Fields, values)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	var public []map[string]interface{}
	if err := json.Unmarshal(encoded, &public); err != nil {
		t.Fatal(err)
	}
	text := public[0]["token"].(string)
	decoded, err := base64.StdEncoding.DecodeString(text)
	if err != nil || !bytes.Equal(decoded, raw) || text != "AP+AAA==" {
		t.Fatalf("raw bytes lost: %s %x %v", text, decoded, err)
	}
	if public[0]["object"].(map[string]interface{})["token"] != text || public[0]["array"].([]interface{})[1] != text || public[0]["map"].(map[string]interface{})["中文 "] != text || public[1]["token"] != nil || public[2]["token"] != "protected" {
		t.Fatal("nested bytes or protection changed")
	}
}
