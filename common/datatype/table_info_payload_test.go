package datatype

import (
	"testing"
	"time"
)

func TestTableInfoFromPayloadRestoresCommonFacts(t *testing.T) {
	t.Parallel()

	payload := map[string]interface{}{
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
	}

	info := TableInfoFromPayload(payload, "orders")
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

func TestTableInfoFromPayloadRestoresRowCountOnlyFacts(t *testing.T) {
	t.Parallel()

	info := TableInfoFromPayload(map[string]interface{}{"row_count": int64(12)}, "")
	if info == nil || info.RowCount == nil || *info.RowCount != 12 {
		t.Fatalf("TableInfoFromPayload() = %#v, want row count", info)
	}

	if empty := TableInfoFromPayload(map[string]interface{}{"row_count": int64(0)}, ""); empty != nil {
		t.Fatalf("TableInfoFromPayload() = %#v, want nil for no stable table facts", empty)
	}
}

func TestTableInfoPayloadUsesJSONTagsAndKeepsNativeFacts(t *testing.T) {
	t.Parallel()

	rowCount := int64(7)
	createdAt := time.Date(2026, 5, 24, 10, 30, 0, 0, time.UTC)
	info := &TableInfo{
		Name:      "orders",
		Kind:      "table",
		RowCount:  &rowCount,
		CreatedAt: &createdAt,
		Fields: []FieldInfo{{
			Name:            "id",
			Type:            FieldTypeInt,
			Nullable:        false,
			PrimaryKey:      true,
			OrdinalPosition: 1,
		}},
		PrimaryKey: []string{"id"},
		Native: map[string]interface{}{
			"engine":        "MergeTree",
			"is_temporary":  false,
			"partition_num": 0,
			"empty_text":    "",
			"nested": map[string]interface{}{
				"enabled": false,
				"count":   0,
			},
		},
	}

	payload := TableInfoPayload(info)
	native := payload["native"].(map[string]interface{})
	if native["is_temporary"] != false || native["partition_num"] != 0 || native["empty_text"] != "" {
		t.Fatalf("native facts lost zero values: %#v", native)
	}
	nested := native["nested"].(map[string]interface{})
	if nested["enabled"] != false || nested["count"] != 0 {
		t.Fatalf("nested native facts lost zero values: %#v", nested)
	}
	fields := payload["fields"].([]interface{})
	field := fields[0].(map[string]interface{})
	if field["nullable"] != false || field["primary_key"] != true {
		t.Fatalf("field payload = %#v", field)
	}
	if payload["created_at"] != createdAt {
		t.Fatalf("created_at = %#v, want %#v", payload["created_at"], createdAt)
	}
}
