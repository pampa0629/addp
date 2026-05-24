package datatype

import "testing"

func TestTableInfoFromAttributesRestoresCommonFacts(t *testing.T) {
	t.Parallel()

	attrs := map[string]interface{}{
		"type_info": map[string]interface{}{
			"table": map[string]interface{}{
				"kind":        "view",
				"comment":     "orders view",
				"row_count":   int64(12),
				"size_bytes":  int64(2048),
				"primary_key": []interface{}{"id"},
				"native":      map[string]interface{}{"engine": "MergeTree"},
				"fields": []interface{}{
					map[string]interface{}{
						"name":                  "id",
						"type":                  "int",
						"native_type":           "int4",
						"nullable":              false,
						"primary_key":           true,
						"ordinal_position":      int64(1),
						"default_expression":    "0",
						"generated":             true,
						"generation_expression": "identity",
					},
				},
			},
		},
	}

	info := TableInfoFromAttributes(attrs, "orders")
	if info == nil || info.Name != "orders" || info.Kind != "view" || info.Comment != "orders view" {
		t.Fatalf("table info = %#v", info)
	}
	if info.RowCount == nil || *info.RowCount != 12 || info.SizeBytes == nil || *info.SizeBytes != 2048 {
		t.Fatalf("table counts = %#v / %#v", info.RowCount, info.SizeBytes)
	}
	if len(info.PrimaryKey) != 1 || info.PrimaryKey[0] != "id" || info.Native["engine"] != "MergeTree" {
		t.Fatalf("table key/native = %#v / %#v", info.PrimaryKey, info.Native)
	}
	field := info.Fields[0]
	if field.Name != "id" || field.Type != FieldTypeInt || field.NativeType != "int4" || !field.PrimaryKey {
		t.Fatalf("field = %#v", field)
	}
	if field.OrdinalPosition != 1 || field.DefaultExpression != "0" || !field.Generated || field.GenerationExpression != "identity" {
		t.Fatalf("field extended facts = %#v", field)
	}
}
