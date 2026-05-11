package metaattr

import (
	"testing"

	"github.com/addp/meta/internal/models"
)

func TestNormalizeMetaItemAttributesKeepsOnlyStandardSections(t *testing.T) {
	t.Parallel()

	normalized := Normalize(models.JSONMap{
		"bucket":           "legacy",
		"composition_type": "single_file",
		"data_family":      "tabular",
		"entry_path":       "legacy/roads.geojson",
		"schema":           map[string]interface{}{"fields": []interface{}{"id"}},
		"extensions":       map[string]interface{}{"document": map[string]interface{}{"title": "old"}},
		"storage":          map[string]interface{}{"bucket": "addp", "path": "roads/"},
		"item":             map[string]interface{}{"organization": "single", "data_type": "table", "format": "json"},
		"type_info":        map[string]interface{}{"table": map[string]interface{}{"fields": []interface{}{"id"}}},
		"format_info":      map[string]interface{}{"json": map[string]interface{}{"feature_count": 10}},
		"capabilities":     map[string]interface{}{"spatial": map[string]interface{}{"primary_geometry_column": "geometry"}},
	})

	allowed := map[string]struct{}{
		"schema_version": {},
		"storage":        {},
		"item":           {},
		"type_info":      {},
		"format_info":    {},
		"content_index":  {},
		"capabilities":   {},
	}
	for key := range normalized {
		if _, ok := allowed[key]; !ok {
			t.Fatalf("unexpected top-level key %q in %#v", key, normalized)
		}
	}
	item := normalized["item"].(map[string]interface{})
	if item["organization"] != "single" || item["data_type"] != "table" || item["format"] != "json" {
		t.Fatalf("item section = %#v, want new item semantics", item)
	}
	if item["composition_type"] != nil || item["data_family"] != nil || item["entry_path"] != nil {
		t.Fatalf("legacy item keys should not be migrated: %#v", item)
	}
	if normalized["schema"] != nil || normalized["extensions"] != nil || normalized["bucket"] != nil {
		t.Fatalf("legacy top-level fields should not survive: %#v", normalized)
	}
}

func TestAttributeHelpersWriteStandardPartitions(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	SetStorage(attrs, "physical_path", "bucket/data.parquet")
	SetItem(attrs, "organization", "single")
	SetItem(attrs, "data_type", "table")
	SetItem(attrs, "format", "parquet")
	SetExtension(attrs, "media", "width", 800)
	SetExtension(attrs, "document", "page_count", 12)
	SetExtension(attrs, "statistics", "row_count", int64(10))
	SetExtension(attrs, "extraction", "metadata_extracted", true)
	SetExtension(attrs, "unqualified", "vendor_key", "kept")

	if attrs["physical_path"] != nil || attrs["format"] != nil || attrs["extensions"] != nil {
		t.Fatalf("helpers should not write flat or legacy sections: %#v", attrs)
	}
	storage := attrs["storage"].(map[string]interface{})
	if storage["physical_path"] != "bucket/data.parquet" {
		t.Fatalf("storage.physical_path = %v", storage["physical_path"])
	}
	item := attrs["item"].(map[string]interface{})
	if item["organization"] != "single" || item["data_type"] != "table" || item["format"] != "parquet" {
		t.Fatalf("item section = %#v", item)
	}
	typeInfo := attrs["type_info"].(map[string]interface{})
	if typeInfo["media"].(map[string]interface{})["width"] != 800 {
		t.Fatalf("type_info.media missing width: %#v", typeInfo)
	}
	if typeInfo["document"].(map[string]interface{})["page_count"] != 12 {
		t.Fatalf("type_info.document missing page_count: %#v", typeInfo)
	}
	capabilities := attrs["capabilities"].(map[string]interface{})
	if capabilities["statistics"].(map[string]interface{})["row_count"] != int64(10) {
		t.Fatalf("capabilities.statistics missing row_count: %#v", capabilities)
	}
	if capabilities["extraction"].(map[string]interface{})["metadata_extracted"] != true {
		t.Fatalf("capabilities.extraction missing metadata_extracted: %#v", capabilities)
	}
	formatInfo := attrs["format_info"].(map[string]interface{})
	if formatInfo["unqualified"].(map[string]interface{})["vendor_key"] != "kept" {
		t.Fatalf("format_info.unqualified missing vendor_key: %#v", formatInfo)
	}
}

func TestUpsertSectionMergesExistingSection(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{
		"type_info": map[string]interface{}{
			"table": map[string]interface{}{"fields": []interface{}{"id"}},
		},
	}

	UpsertNested(attrs, "type_info", "table", map[string]interface{}{"row_count": int64(10)})

	table := attrs["type_info"].(map[string]interface{})["table"].(map[string]interface{})
	if table["row_count"] != int64(10) {
		t.Fatalf("type_info.table.row_count = %v", table["row_count"])
	}
	if _, ok := table["fields"]; !ok {
		t.Fatalf("type_info.table.fields missing after merge: %#v", table)
	}
}

func TestSetSchemaFieldsWritesPartitionOnly(t *testing.T) {
	t.Parallel()

	fields := []map[string]interface{}{{"name": "id", "type": "integer"}}
	attrs := models.JSONMap{}

	SetSchemaFields(attrs, fields)

	if attrs["fields"] != nil {
		t.Fatalf("flat fields should not be written: %#v", attrs)
	}
	typeInfo := attrs["type_info"].(map[string]interface{})
	table := typeInfo["table"].(map[string]interface{})
	if table["fields"] == nil {
		t.Fatalf("type_info.table.fields missing: %#v", table)
	}
}
