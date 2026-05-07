package extractor

import (
	"testing"

	"github.com/addp/common/format"
	"github.com/addp/meta/internal/models"
)

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
			"kind":       "image",
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
	if media["kind"] != "image" || media["width"] != 800 {
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

func TestApplyExtractedMetadataExtensionsStoresSpatialCapability(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	applyExtractedMetadataExtensions(attrs, &format.ExtractedMetadata{
		CustomAttrs: map[string]interface{}{
			"spatial": map[string]interface{}{
				"extent":            []float64{100, 180, 120, 200},
				"srid":              4326,
				"has_spatial_index": false,
			},
		},
	})

	capabilities := attrs["capabilities"].(map[string]interface{})
	spatial := capabilities["spatial"].(map[string]interface{})
	if spatial["srid"] != 4326 || spatial["has_spatial_index"] != false {
		t.Fatalf("capabilities.spatial = %#v", spatial)
	}
	if attrs["spatial"] != nil {
		t.Fatalf("legacy flat spatial attr should not be written: %#v", attrs)
	}
}
