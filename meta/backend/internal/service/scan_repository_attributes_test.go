package service

import (
	"testing"

	"github.com/addp/common/format"
	"github.com/addp/meta/internal/models"
)

func TestNormalizeMetaItemAttributesKeepsOnlyStandardSections(t *testing.T) {
	t.Parallel()

	normalized := normalizeMetaItemAttributes(models.JSONMap{
		"bucket":           "legacy",
		"composition_type": "single_file",
		"data_family":      "tabular",
		"entry_path":       "legacy/roads.geojson",
		"schema":           map[string]interface{}{"fields": []interface{}{"id"}},
		"extensions":       map[string]interface{}{"document": map[string]interface{}{"title": "old"}},
		"storage":          map[string]interface{}{"bucket": "addp", "path": "roads/"},
		"item":             map[string]interface{}{"organization": "single", "data_type": "table", "format": "geojson"},
		"type_info":        map[string]interface{}{"table": map[string]interface{}{"fields": []interface{}{"id"}}},
		"format_info":      map[string]interface{}{"geojson": map[string]interface{}{"feature_count": 10}},
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
	if item["organization"] != "single" || item["data_type"] != "table" || item["format"] != "geojson" {
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
	setStorageAttribute(attrs, "physical_path", "bucket/data.parquet")
	setItemAttribute(attrs, "organization", "single")
	setItemAttribute(attrs, "data_type", "table")
	setItemAttribute(attrs, "format", "parquet")
	setExtensionAttribute(attrs, "media", "width", 800)
	setExtensionAttribute(attrs, "document", "page_count", 12)
	setExtensionAttribute(attrs, "statistics", "row_count", int64(10))
	setExtensionAttribute(attrs, "extraction", "metadata_extracted", true)
	setExtensionAttribute(attrs, "unqualified", "vendor_key", "kept")

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

	upsertNestedSection(attrs, "type_info", "table", map[string]interface{}{"row_count": int64(10)})

	table := attrs["type_info"].(map[string]interface{})["table"].(map[string]interface{})
	if table["row_count"] != int64(10) {
		t.Fatalf("type_info.table.row_count = %v", table["row_count"])
	}
	if _, ok := table["fields"]; !ok {
		t.Fatalf("type_info.table.fields missing after merge: %#v", table)
	}
}

func TestExtractionMetadataWritesCapabilitiesAndTypeInfo(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	setExtractionAttribute(attrs, "metadata_extracted", true)
	setExtractionAttribute(attrs, "extractor_available", true)
	setExtractionAttribute(attrs, "extracted_metadata", map[string]interface{}{
		"custom_attrs": map[string]interface{}{"width": 800},
	})
	applyExtractedMetadataExtensions(attrs, &format.ExtractedMetadata{
		BasicInfo: format.BasicMetadata{FileType: "PDF", Encoding: "UTF-8"},
		CustomAttrs: map[string]interface{}{
			"page_count": 12,
			"word_count": 2400,
			"width":      800,
			"plain_text": "not stored",
			"vendor_key": "kept",
		},
		SchemaInfo: &format.SchemaMetadata{RowCount: 10, Columns: []format.ColumnMetadata{{Name: "id"}}},
	})

	if attrs["extensions"] != nil || attrs["metadata_extracted"] != nil || attrs["page_count"] != nil {
		t.Fatalf("metadata helpers should not write legacy fields: %#v", attrs)
	}
	capabilities := attrs["capabilities"].(map[string]interface{})
	extraction := capabilities["extraction"].(map[string]interface{})
	if extraction["metadata_extracted"] != true || extraction["extractor_available"] != true || extraction["extracted_metadata"] == nil {
		t.Fatalf("capabilities.extraction = %#v", extraction)
	}
	statistics := capabilities["statistics"].(map[string]interface{})
	if statistics["row_count"] != int64(10) || statistics["column_count"] != 1 {
		t.Fatalf("capabilities.statistics = %#v", statistics)
	}
	typeInfo := attrs["type_info"].(map[string]interface{})
	document := typeInfo["document"].(map[string]interface{})
	if document["page_count"] != 12 || document["word_count"] != 2400 || document["file_type_friendly"] != "PDF" {
		t.Fatalf("type_info.document = %#v", document)
	}
	media := typeInfo["media"].(map[string]interface{})
	if media["width"] != 800 {
		t.Fatalf("type_info.media = %#v", media)
	}
	unqualified := attrs["format_info"].(map[string]interface{})["unqualified"].(map[string]interface{})
	if unqualified["vendor_key"] != "kept" {
		t.Fatalf("format_info.unqualified = %#v", unqualified)
	}
	if _, ok := unqualified["plain_text"]; ok {
		t.Fatalf("plain_text should not be stored: %#v", unqualified)
	}
}

func TestSpatialMetadataWritesMinimalCapabilitiesSpatial(t *testing.T) {
	t.Parallel()

	values := spatialCapabilityFromMetadata(&models.SpatialMetadata{
		GeometryColumn:  "geom",
		SRID:            4326,
		ExtentSRID:      4326,
		Extent:          []float64{1, 2, 3, 4},
		GeometryTypes:   []string{"ST_MultiPolygon"},
		HasSpatialIndex: true,
	})

	if values["spatial_metadata"] != nil || values["geometry_types"] != nil {
		t.Fatalf("legacy spatial metadata should not be written: %#v", values)
	}
	if values["primary_geometry_column"] != "geom" || values["has_spatial_index"] != true {
		t.Fatalf("capabilities.spatial = %#v", values)
	}
	columns := values["geometry_columns"].([]map[string]interface{})
	if len(columns) != 1 {
		t.Fatalf("geometry_columns = %#v", columns)
	}
	if columns[0]["name"] != "geom" || columns[0]["geometry_type"] != "geometry" || columns[0]["srid"] != 4326 {
		t.Fatalf("geometry column = %#v", columns[0])
	}
}

func TestIndexerAttributeReadersPreferStandardTypeInfo(t *testing.T) {
	t.Parallel()

	attrs := map[string]interface{}{
		"title":      "legacy title",
		"page_count": 1,
		"type_info": map[string]interface{}{
			"document": map[string]interface{}{
				"title":      "standard title",
				"page_count": 12,
				"keywords":   []interface{}{"alpha", "beta"},
			},
		},
	}

	if got := stringFromAttributes(attrs, "document", "title"); got != "standard title" {
		t.Fatalf("title = %q, want standard title", got)
	}
	if got := intFromAttributes(attrs, "document", "page_count"); got != 12 {
		t.Fatalf("page_count = %d, want 12", got)
	}
	keywords := stringSliceFromAttributes(attrs, "document", "keywords")
	if len(keywords) != 2 || keywords[0] != "alpha" {
		t.Fatalf("keywords = %#v, want type_info keywords", keywords)
	}

	standardOnly := map[string]interface{}{
		"title":      "legacy title",
		"page_count": 1,
	}
	if got := stringFromAttributes(standardOnly, "document", "title"); got != "" {
		t.Fatalf("legacy title fallback = %q, want empty", got)
	}
	if got := intFromAttributes(standardOnly, "document", "page_count"); got != 0 {
		t.Fatalf("legacy page_count fallback = %d, want 0", got)
	}
}

func TestIndexerReadsPlainTextFromStandardExtractionPayload(t *testing.T) {
	t.Parallel()

	attrs := map[string]interface{}{
		"plain_text": "legacy text",
		"capabilities": map[string]interface{}{
			"extraction": map[string]interface{}{
				"extracted_metadata": map[string]interface{}{
					"custom_attrs": map[string]interface{}{
						"plain_text": "standard text",
					},
				},
			},
		},
	}

	if got := extractedPlainTextFromAttributes(attrs); got != "standard text" {
		t.Fatalf("plain text = %q, want standard text", got)
	}
	if got := extractedPlainTextFromAttributes(map[string]interface{}{"plain_text": "legacy text"}); got != "" {
		t.Fatalf("legacy plain text fallback = %q, want empty", got)
	}
}
