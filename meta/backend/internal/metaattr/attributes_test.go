package metaattr

import (
	"testing"

	"github.com/addp/common/datatype"
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
		"item":             map[string]interface{}{"layout": "single", "data_type": "table", "format": "json"},
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
		"capabilities":   {},
	}
	for key := range normalized {
		if _, ok := allowed[key]; !ok {
			t.Fatalf("unexpected top-level key %q in %#v", key, normalized)
		}
	}
	item := normalized["item"].(map[string]interface{})
	if item["layout"] != "single" || item["data_type"] != "table" || item["format"] != "json" {
		t.Fatalf("item section = %#v, want new item semantics", item)
	}
	if item["composition_type"] != nil || item["data_family"] != nil || item["entry_path"] != nil {
		t.Fatalf("legacy item keys should not be migrated: %#v", item)
	}
	if normalized["schema"] != nil || normalized["extensions"] != nil || normalized["bucket"] != nil {
		t.Fatalf("legacy top-level fields should not survive: %#v", normalized)
	}
	if _, ok := normalized["access_index"]; ok {
		t.Fatalf("empty access_index should not survive: %#v", normalized)
	}
}

func TestNormalizePrunesEmptyStandardSections(t *testing.T) {
	t.Parallel()

	normalized := Normalize(models.JSONMap{
		"item":         map[string]interface{}{"layout": "single", "format": ""},
		"storage":      map[string]interface{}{},
		"type_info":    map[string]interface{}{"table": map[string]interface{}{}},
		"format_info":  map[string]interface{}{"csv": map[string]interface{}{}},
		"access_index": map[string]interface{}{"table": map[string]interface{}{}},
		"capabilities": map[string]interface{}{"spatial": map[string]interface{}{"has_spatial_index": false}},
	})

	if _, ok := normalized["storage"]; ok {
		t.Fatalf("empty storage should be pruned: %#v", normalized)
	}
	if _, ok := normalized["type_info"]; ok {
		t.Fatalf("empty type_info should be pruned: %#v", normalized)
	}
	if _, ok := normalized["format_info"]; ok {
		t.Fatalf("empty format_info should be pruned: %#v", normalized)
	}
	if _, ok := normalized["access_index"]; ok {
		t.Fatalf("empty access_index should be pruned: %#v", normalized)
	}
	item := normalized["item"].(map[string]interface{})
	if item["layout"] != "single" || item["format"] != nil {
		t.Fatalf("item cleanup = %#v", item)
	}
	spatial := normalized["capabilities"].(map[string]interface{})["spatial"].(map[string]interface{})
	if spatial["has_spatial_index"] != false {
		t.Fatalf("false capability value must be kept: %#v", spatial)
	}
}

func TestAttributeHelpersWriteStandardPartitions(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	SetStorage(attrs, "physical_path", "bucket/data.parquet")
	SetItem(attrs, "layout", "single")
	SetItem(attrs, "data_type", "table")
	SetItem(attrs, "format", "parquet")
	SetExtension(attrs, "media", "width", 800)
	SetExtension(attrs, "document", "page_count", 12)
	SetExtension(attrs, "statistics", "sample_size", int64(10))
	SetExtension(attrs, "extraction", "extractor_available", true)
	SetExtension(attrs, "unqualified", "vendor_key", "kept")

	if attrs["physical_path"] != nil || attrs["format"] != nil || attrs["extensions"] != nil {
		t.Fatalf("helpers should not write flat or legacy sections: %#v", attrs)
	}
	storage := attrs["storage"].(map[string]interface{})
	if storage["physical_path"] != "bucket/data.parquet" {
		t.Fatalf("storage.physical_path = %v", storage["physical_path"])
	}
	item := attrs["item"].(map[string]interface{})
	if item["layout"] != "single" || item["data_type"] != "table" || item["format"] != "parquet" {
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
	if capabilities["statistics"].(map[string]interface{})["sample_size"] != int64(10) {
		t.Fatalf("capabilities.statistics missing sample_size: %#v", capabilities)
	}
	if capabilities["extraction"].(map[string]interface{})["extractor_available"] != true {
		t.Fatalf("capabilities.extraction missing extractor_available: %#v", capabilities)
	}
	formatInfo := attrs["format_info"].(map[string]interface{})
	if formatInfo["unqualified"].(map[string]interface{})["vendor_key"] != "kept" {
		t.Fatalf("format_info.unqualified missing vendor_key: %#v", formatInfo)
	}
}

func TestMergeStandardAttributesWritesStandardSections(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	SetExtraction(attrs, "extractor_available", true)
	MergeStandardAttributes(attrs, map[string]interface{}{
		"type_info": map[string]interface{}{
			"document": map[string]interface{}{
				"page_count": 12,
				"word_count": 2400,
			},
		},
		"format_info": map[string]interface{}{
			"pdf": map[string]interface{}{
				"file_type_friendly":  "PDF",
				"extraction_encoding": "UTF-8",
				"vendor_key":          "kept",
			},
		},
	})

	if attrs["extensions"] != nil || attrs["metadata_extracted"] != nil || attrs["page_count"] != nil {
		t.Fatalf("helpers should not write flat or legacy sections: %#v", attrs)
	}
	capabilities := attrs["capabilities"].(map[string]interface{})
	extraction := capabilities["extraction"].(map[string]interface{})
	if extraction["metadata_extracted"] != nil || extraction["extractor_available"] != true {
		t.Fatalf("capabilities.extraction = %#v", extraction)
	}
	document := attrs["type_info"].(map[string]interface{})["document"].(map[string]interface{})
	if document["page_count"] != 12 || document["word_count"] != 2400 || document["file_type_friendly"] != nil {
		t.Fatalf("type_info.document = %#v", document)
	}
	pdfInfo := attrs["format_info"].(map[string]interface{})["pdf"].(map[string]interface{})
	if pdfInfo["vendor_key"] != "kept" || pdfInfo["file_type_friendly"] != "PDF" {
		t.Fatalf("format_info.pdf = %#v", pdfInfo)
	}
}

func TestFormatInfoAttributesWrapsUnqualifiedProviderFacts(t *testing.T) {
	t.Parallel()

	attrs := FormatInfoAttributes("pdf", map[string]interface{}{"producer": "demo"})
	formatInfo := attrs["format_info"].(map[string]interface{})
	pdf := formatInfo["pdf"].(map[string]interface{})
	if pdf["producer"] != "demo" {
		t.Fatalf("format_info.pdf = %#v", pdf)
	}
}

func TestFormatInfoAttributesDropsStandardSections(t *testing.T) {
	t.Parallel()

	input := map[string]interface{}{
		"type_info": map[string]interface{}{
			"document": map[string]interface{}{"page_count": 12},
		},
		"producer": "demo",
	}
	attrs := FormatInfoAttributes("pdf", input)
	formatInfo := attrs["format_info"].(map[string]interface{})
	pdf := formatInfo["pdf"].(map[string]interface{})
	if pdf["producer"] != "demo" {
		t.Fatalf("format_info.pdf = %#v", pdf)
	}
	if pdf["type_info"] != nil || attrs["type_info"] != nil {
		t.Fatalf("standard sections should be dropped from format info attrs: %#v", attrs)
	}
}

func TestRemoveAccessIndexTableKeepsOtherIndexes(t *testing.T) {
	t.Parallel()

	attrs := map[string]interface{}{
		"access_index": map[string]interface{}{
			"table": map[string]interface{}{"kind": "sparse_row_index"},
			"text":  map[string]interface{}{"kind": "full_text"},
		},
	}

	RemoveAccessIndexTable(attrs)

	accessIndex := attrs["access_index"].(map[string]interface{})
	if accessIndex["table"] != nil || accessIndex["text"] == nil {
		t.Fatalf("access_index = %#v", accessIndex)
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

func TestSetTableFieldsWritesPartitionOnly(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}

	SetTableFields(attrs, []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeInt}})

	if attrs["fields"] != nil {
		t.Fatalf("flat fields should not be written: %#v", attrs)
	}
	typeInfo := attrs["type_info"].(map[string]interface{})
	table := typeInfo["table"].(map[string]interface{})
	if table["fields"] == nil {
		t.Fatalf("type_info.table.fields missing: %#v", table)
	}
}

func TestNormalizeRemovesPersistedContentDerivatives(t *testing.T) {
	t.Parallel()

	normalized := Normalize(models.JSONMap{
		"plain_text_preview": "legacy top-level preview",
		"capabilities": map[string]interface{}{
			"extraction": map[string]interface{}{
				"status": "completed", "plain_text_preview": "legacy preview", "text_excerpt": "legacy excerpt",
			},
		},
	})
	if _, exists := normalized["plain_text_preview"]; exists {
		t.Fatalf("top-level content derivative survived normalization: %#v", normalized)
	}
	capabilities := normalized["capabilities"].(map[string]interface{})
	extraction := capabilities["extraction"].(map[string]interface{})
	if extraction["status"] != "completed" || extraction["plain_text_preview"] != nil || extraction["text_excerpt"] != nil {
		t.Fatalf("normalized extraction = %#v", extraction)
	}
}
