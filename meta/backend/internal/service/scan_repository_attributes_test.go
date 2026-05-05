package service

import (
	"testing"

	"github.com/addp/common/format"
	"github.com/addp/meta/internal/models"
)

func TestNormalizeMetaItemAttributesBuildsPartitionedCore(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{
		"bucket":           "addp",
		"path":             "roads/",
		"name":             "roads.geojson",
		"content_type":     "application/geo+json",
		"composition_type": "single_file",
		"data_family":      "tabular",
		"format":           "geojson",
		"entry_path":       "addp/roads/roads.geojson",
		"component_files":  []string{"addp/roads/roads.geojson"},
		"fields":           []map[string]interface{}{{"name": "id", "data_type": "integer"}},
	}

	normalized := normalizeMetaItemAttributes(attrs)

	if normalized["schema_version"] != 1 {
		t.Fatalf("schema_version = %v, want 1", normalized["schema_version"])
	}
	item := normalized["item"].(map[string]interface{})
	if item["data_family"] != "tabular" || item["format"] != "geojson" {
		t.Fatalf("item section = %#v, want tabular geojson", item)
	}
	storage := normalized["storage"].(map[string]interface{})
	if storage["bucket"] != "addp" || storage["path"] != "roads/" {
		t.Fatalf("storage section = %#v, want bucket/path", storage)
	}
	schema := normalized["schema"].(map[string]interface{})
	if _, ok := schema["fields"]; !ok {
		t.Fatalf("schema section = %#v, want fields", schema)
	}
}

func TestNormalizeMetaItemAttributesMigratesLegacySchemaName(t *testing.T) {
	t.Parallel()

	normalized := normalizeMetaItemAttributes(models.JSONMap{
		"schema":     "public",
		"table_type": "BASE TABLE",
	})

	if normalized["schema"] == "public" {
		t.Fatalf("top-level schema should be partition map, got legacy string")
	}
	if normalized["schema_name"] != nil {
		t.Fatalf("flat schema_name should be removed: %#v", normalized)
	}
	storage := normalized["storage"].(map[string]interface{})
	if storage["schema_name"] != "public" {
		t.Fatalf("storage.schema_name = %v, want public", storage["schema_name"])
	}
	item := normalized["item"].(map[string]interface{})
	if item["namespace"] != "public" {
		t.Fatalf("item.namespace = %v, want public", item["namespace"])
	}
	schema := normalized["schema"].(map[string]interface{})
	if schema["table_type"] != "BASE TABLE" {
		t.Fatalf("schema.table_type = %v, want BASE TABLE", schema["table_type"])
	}
}

func TestNormalizeMetaItemAttributesBuildsSpatialExtension(t *testing.T) {
	t.Parallel()

	spatial := map[string]interface{}{"geometry_column": "shape"}
	normalized := normalizeMetaItemAttributes(models.JSONMap{
		"spatial_metadata": spatial,
	})

	extensions := normalized["extensions"].(map[string]interface{})
	spatialExtension := extensions["spatial"].(map[string]interface{})
	gotSpatial := spatialExtension["spatial_metadata"].(map[string]interface{})
	if gotSpatial["geometry_column"] != "shape" {
		t.Fatalf("extensions.spatial = %#v, want spatial metadata", spatialExtension)
	}
}

func TestNormalizeMetaItemAttributesProtectsPartitionedCoreFromFlatConflicts(t *testing.T) {
	t.Parallel()

	normalized := normalizeMetaItemAttributes(models.JSONMap{
		"data_family": "document",
		"format":      "pdf",
		"item": map[string]interface{}{
			"data_family": "tabular",
			"format":      "geojson",
		},
	})

	item := normalized["item"].(map[string]interface{})
	if item["data_family"] != "tabular" || item["format"] != "geojson" {
		t.Fatalf("item section = %#v, want partitioned core to win", item)
	}
	if normalized["data_family"] != nil || normalized["format"] != nil {
		t.Fatalf("flat core fields should be removed: %#v", normalized)
	}
}

func TestNormalizeMetaItemAttributesConstrainsExtensionNamespaces(t *testing.T) {
	t.Parallel()

	normalized := normalizeMetaItemAttributes(models.JSONMap{
		"extensions": map[string]interface{}{
			"vendor": map[string]interface{}{"unsafe": true},
			"com.acme.parser": map[string]interface{}{
				"custom_value": "ok",
			},
			"MEDIA": map[string]interface{}{
				"width": 640,
			},
		},
	})

	extensions := normalized["extensions"].(map[string]interface{})
	if _, ok := extensions["vendor"]; ok {
		t.Fatalf("unqualified namespace should not stay at top-level extensions: %#v", extensions)
	}
	if _, ok := extensions["com.acme.parser"]; !ok {
		t.Fatalf("private namespace missing: %#v", extensions)
	}
	media := extensions["media"].(map[string]interface{})
	if media["width"] != 640 {
		t.Fatalf("media extension = %#v, want normalized standard namespace", media)
	}
	unqualified := extensions["unqualified"].(map[string]interface{})
	if _, ok := unqualified["vendor"]; !ok {
		t.Fatalf("unqualified extension = %#v, want original invalid namespace captured", unqualified)
	}
}

func TestUpsertSectionMergesExistingSection(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{
		"schema": map[string]interface{}{
			"fields": []interface{}{"id"},
		},
	}

	upsertSection(attrs, "schema", map[string]interface{}{
		"table_type": "BASE TABLE",
	})

	schema := attrs["schema"].(map[string]interface{})
	if schema["table_type"] != "BASE TABLE" {
		t.Fatalf("schema.table_type = %v, want BASE TABLE", schema["table_type"])
	}
	if _, ok := schema["fields"]; !ok {
		t.Fatalf("schema.fields missing after merge: %#v", schema)
	}
}

func TestSetAttributeHelpersWritePartitionOnly(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	setStorageAttribute(attrs, "physical_path", "bucket/data.parquet")
	setItemAttribute(attrs, "format", "parquet")

	if attrs["physical_path"] != nil || attrs["format"] != nil {
		t.Fatalf("flat compatibility fields should not be written: %#v", attrs)
	}
	storage := attrs["storage"].(map[string]interface{})
	if storage["physical_path"] != "bucket/data.parquet" {
		t.Fatalf("storage.physical_path = %v, want bucket/data.parquet", storage["physical_path"])
	}
	item := attrs["item"].(map[string]interface{})
	if item["format"] != "parquet" {
		t.Fatalf("item.format = %v, want parquet", item["format"])
	}
}

func TestSetExtensionAttributeWritesNamespaceOnly(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	spatial := map[string]interface{}{"geometry_column": "geom"}

	setExtensionAttribute(attrs, "spatial", "spatial_metadata", spatial)

	if attrs["spatial_metadata"] != nil {
		t.Fatalf("flat spatial_metadata should not be written: %#v", attrs)
	}
	extensions := attrs["extensions"].(map[string]interface{})
	spatialExt := extensions["spatial"].(map[string]interface{})
	if spatialExt["spatial_metadata"] == nil {
		t.Fatalf("extensions.spatial.spatial_metadata missing: %#v", spatialExt)
	}
}

func TestSetExtensionAttributeDoesNotWriteNonSpatialFlatCompatibility(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}

	setExtensionAttribute(attrs, "media", "width", 800)
	setExtensionAttribute(attrs, "document", "page_count", 12)
	setExtensionAttribute(attrs, "unqualified", "vendor_key", "kept")

	if attrs["width"] != nil || attrs["page_count"] != nil || attrs["vendor_key"] != nil {
		t.Fatalf("non-spatial extension fields should not be flat: %#v", attrs)
	}
	extensions := attrs["extensions"].(map[string]interface{})
	if extensions["media"].(map[string]interface{})["width"] != 800 {
		t.Fatalf("media extension missing width: %#v", extensions)
	}
	if extensions["document"].(map[string]interface{})["page_count"] != 12 {
		t.Fatalf("document extension missing page_count: %#v", extensions)
	}
	if extensions["unqualified"].(map[string]interface{})["vendor_key"] != "kept" {
		t.Fatalf("unqualified extension missing vendor_key: %#v", extensions)
	}
}

func TestNormalizeMetaItemAttributesMovesUnregisteredFlatKeysToUnqualified(t *testing.T) {
	t.Parallel()

	normalized := normalizeMetaItemAttributes(models.JSONMap{
		"bucket":     "addp",
		"width":      800,
		"page_count": 12,
		"vendor_key": "kept",
	})

	if normalized["width"] != nil || normalized["page_count"] != nil || normalized["vendor_key"] != nil {
		t.Fatalf("unregistered flat keys should be removed: %#v", normalized)
	}
	extensions := normalized["extensions"].(map[string]interface{})
	unqualified := extensions["unqualified"].(map[string]interface{})
	if unqualified["width"] != 800 || unqualified["page_count"] != 12 || unqualified["vendor_key"] != "kept" {
		t.Fatalf("unqualified extension = %#v, want moved flat keys", unqualified)
	}
	if normalized["bucket"] != nil {
		t.Fatalf("registered flat compatibility field should be removed after section migration: %#v", normalized)
	}
	storage := normalized["storage"].(map[string]interface{})
	if storage["bucket"] != "addp" {
		t.Fatalf("storage.bucket missing after migration: %#v", storage)
	}
}

func TestNormalizeMetaItemAttributesOnlyEmitsStandardTopLevelKeys(t *testing.T) {
	t.Parallel()

	normalized := normalizeMetaItemAttributes(models.JSONMap{
		"bucket":         "addp",
		"format":         "pdf",
		"fields":         []map[string]interface{}{{"name": "id"}},
		"width":          800,
		"page_count":     12,
		"custom_private": "kept",
	})

	allowedTopLevel := map[string]struct{}{
		"schema_version": {},
		"storage":        {},
		"item":           {},
		"schema":         {},
		"extensions":     {},
	}
	for key := range normalized {
		if _, allowed := allowedTopLevel[key]; !allowed {
			t.Fatalf("unexpected flat attribute key %q in %#v", key, normalized)
		}
	}
}

func TestExtractionExtensionIsStandardAndNotFlat(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	setExtractionAttribute(attrs, "metadata_extracted", true)
	setExtractionAttribute(attrs, "extractor_available", true)
	setExtractionAttribute(attrs, "extracted_metadata", map[string]interface{}{
		"custom_attrs": map[string]interface{}{"width": 800},
	})
	setExtractionAttribute(attrs, "schema_info", map[string]interface{}{"row_count": 10})
	attrs["plain_text_preview"] = "hello"

	normalized := normalizeMetaItemAttributes(attrs)
	if normalized["metadata_extracted"] != nil || normalized["extractor_available"] != nil ||
		normalized["extracted_metadata"] != nil || normalized["schema_info"] != nil ||
		normalized["plain_text_preview"] != nil {
		t.Fatalf("extraction fields should not be flat: %#v", normalized)
	}

	extensions := normalized["extensions"].(map[string]interface{})
	extraction := extensions["extraction"].(map[string]interface{})
	if extraction["metadata_extracted"] != true || extraction["extractor_available"] != true {
		t.Fatalf("extraction status missing: %#v", extraction)
	}
	if extraction["extracted_metadata"] == nil || extraction["schema_info"] == nil {
		t.Fatalf("extraction payload missing: %#v", extraction)
	}
	if extraction["plain_text_preview"] != "hello" {
		t.Fatalf("plain text preview missing from extraction: %#v", extraction)
	}
	if _, ok := extensions["unqualified"]; ok {
		t.Fatalf("extraction should be standard namespace, got extensions %#v", extensions)
	}
}

func TestApplyExtractedMetadataExtensionsWritesStandardSections(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	applyExtractedMetadataExtensions(attrs, &format.ExtractedMetadata{
		BasicInfo: format.BasicMetadata{FileType: "PDF", Encoding: "UTF-8"},
		CustomAttrs: map[string]interface{}{
			"page_count": 12,
			"word_count": 2400,
			"width":      800,
			"plain_text": "not stored in extensions",
			"vendor_key": "kept",
		},
		SchemaInfo: &format.SchemaMetadata{RowCount: 10, Columns: []format.ColumnMetadata{{Name: "id"}}},
	})

	extensions := attrs["extensions"].(map[string]interface{})
	document := extensions["document"].(map[string]interface{})
	if document["page_count"] != 12 || document["word_count"] != 2400 {
		t.Fatalf("document extension = %#v, want page and word counts", document)
	}
	media := extensions["media"].(map[string]interface{})
	if media["width"] != 800 {
		t.Fatalf("media extension = %#v, want width", media)
	}
	statistics := extensions["statistics"].(map[string]interface{})
	if statistics["row_count"] != int64(10) || statistics["column_count"] != 1 {
		t.Fatalf("statistics extension = %#v, want schema stats", statistics)
	}
	unqualified := extensions["unqualified"].(map[string]interface{})
	if unqualified["vendor_key"] != "kept" {
		t.Fatalf("unqualified extension = %#v, want custom key", unqualified)
	}
	if _, ok := unqualified["plain_text"]; ok {
		t.Fatalf("plain_text should not be stored in extensions: %#v", unqualified)
	}
	if attrs["page_count"] != nil || attrs["width"] != nil || attrs["vendor_key"] != nil {
		t.Fatalf("extracted extension fields should not be flat: %#v", attrs)
	}
}

func TestIndexerAttributeReadersPreferStandardExtensions(t *testing.T) {
	t.Parallel()

	attrs := map[string]interface{}{
		"title":      "legacy title",
		"page_count": 1,
		"extensions": map[string]interface{}{
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
		t.Fatalf("keywords = %#v, want extension keywords", keywords)
	}
}
