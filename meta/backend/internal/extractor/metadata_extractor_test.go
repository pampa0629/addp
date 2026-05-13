package extractor

import (
	"testing"

	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
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
			"page_count": 12,
			"word_count": 2400,
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
	unqualified := attrs["format_info"].(map[string]interface{})["unqualified"].(map[string]interface{})
	if unqualified["vendor_key"] != "kept" {
		t.Fatalf("format_info.unqualified = %#v", unqualified)
	}
	if _, ok := unqualified["plain_text"]; ok {
		t.Fatalf("plain_text should not be stored: %#v", unqualified)
	}
}

func TestMediaInfoAttributesWritesTypeInfoAndSpatial(t *testing.T) {
	t.Parallel()

	duration := int64(1234)
	size := int64(4567)
	attrs := MediaInfoAttributes(&format.MediaInfo{
		Format:     format.FormatTIFF,
		MediaType:  "image",
		MIMEType:   "image/tiff",
		Width:      800,
		Height:     600,
		DurationMS: &duration,
		Encoding:   "tiff",
		ColorSpace: "RGB",
		SizeBytes:  &size,
		SpatialAttrs: map[string]interface{}{
			"srid":   4326,
			"extent": []float64{1, 2, 3, 4},
		},
	})

	media := commonJSON.Section(attrs, "type_info.media")
	if media["kind"] != "image" || media["width"] != 800 || media["height"] != 600 {
		t.Fatalf("type_info.media = %#v", media)
	}
	if media["duration_ms"] != duration || media["encoding"] != "tiff" || media["mime_type"] != "image/tiff" {
		t.Fatalf("type_info.media = %#v", media)
	}
	spatial := commonJSON.Section(attrs, "capabilities.spatial")
	if spatial["srid"] != 4326 {
		t.Fatalf("capabilities.spatial = %#v", spatial)
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
