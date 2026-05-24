package jsonmap

import (
	"testing"
	"time"
)

func TestMapFromStructUsesJSONTagsAndKeepsMapFacts(t *testing.T) {
	t.Parallel()

	type fieldInfo struct {
		Name     string `json:"name,omitempty"`
		Nullable bool   `json:"nullable"`
		Skipped  string `json:"skipped,omitempty"`
	}
	type tableInfo struct {
		Name      string                 `json:"name,omitempty"`
		CreatedAt *time.Time             `json:"created_at,omitempty"`
		Fields    []fieldInfo            `json:"fields,omitempty"`
		Native    map[string]interface{} `json:"native,omitempty"`
	}

	createdAt := time.Date(2026, 5, 24, 10, 30, 0, 0, time.UTC)
	attrs := MapFromStruct(&tableInfo{
		Name:      "orders",
		CreatedAt: &createdAt,
		Fields: []fieldInfo{{
			Name:     "id",
			Nullable: false,
		}},
		Native: map[string]interface{}{
			"is_temporary": false,
			"count":        0,
			"empty_text":   "",
			"nested": map[string]interface{}{
				"enabled": false,
			},
		},
	})

	if attrs["name"] != "orders" || attrs["created_at"] != createdAt {
		t.Fatalf("attrs = %#v", attrs)
	}
	fields := attrs["fields"].([]interface{})
	field := fields[0].(map[string]interface{})
	if field["nullable"] != false {
		t.Fatalf("field nullable should be kept: %#v", field)
	}
	if _, ok := field["skipped"]; ok {
		t.Fatalf("omitempty field should be skipped: %#v", field)
	}
	native := attrs["native"].(map[string]interface{})
	if native["is_temporary"] != false || native["count"] != 0 || native["empty_text"] != "" {
		t.Fatalf("native zero facts should be kept: %#v", native)
	}
	nested := native["nested"].(map[string]interface{})
	if nested["enabled"] != false {
		t.Fatalf("nested native zero facts should be kept: %#v", nested)
	}
}

func TestDecodeStructUsesJSONTags(t *testing.T) {
	t.Parallel()

	type tableInfo struct {
		Name      string    `json:"name,omitempty"`
		Count     *int64    `json:"count,omitempty"`
		UpdatedAt time.Time `json:"updated_at,omitempty"`
	}

	var info tableInfo
	err := DecodeStruct(map[string]interface{}{
		"name":       "orders",
		"count":      float64(12),
		"updated_at": "2026-05-24T10:30:00Z",
	}, &info)
	if err != nil {
		t.Fatalf("DecodeStruct error: %v", err)
	}
	if info.Name != "orders" || info.Count == nil || *info.Count != 12 {
		t.Fatalf("decoded info = %#v", info)
	}
	if info.UpdatedAt.IsZero() {
		t.Fatalf("updated_at should be decoded: %#v", info)
	}
}
