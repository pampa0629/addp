package extractor

import (
	"testing"

	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
)

func TestMergeStandardAttributesWritesStandardSections(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	setExtractionAttribute(attrs, "metadata_extracted", true)
	setExtractionAttribute(attrs, "extractor_available", true)
	setExtractionAttribute(attrs, "extracted_metadata", map[string]interface{}{
		"type_info": map[string]interface{}{"document": map[string]interface{}{"page_count": 12}},
	})
	mergeStandardAttributes(attrs, map[string]interface{}{
		"type_info": map[string]interface{}{
			"document": map[string]interface{}{
				"page_count":          12,
				"word_count":          2400,
				"file_type_friendly":  "PDF",
				"extraction_encoding": "UTF-8",
			},
		},
		"format_info": map[string]interface{}{
			"pdf": map[string]interface{}{"vendor_key": "kept"},
		},
	})

	if attrs["extensions"] != nil || attrs["metadata_extracted"] != nil || attrs["page_count"] != nil {
		t.Fatalf("metadata helpers should not write legacy fields: %#v", attrs)
	}
	capabilities := attrs["capabilities"].(map[string]interface{})
	extraction := capabilities["extraction"].(map[string]interface{})
	if extraction["metadata_extracted"] != true || extraction["extractor_available"] != true || extraction["extracted_metadata"] == nil {
		t.Fatalf("capabilities.extraction = %#v", extraction)
	}
	document := commonJSON.Section(attrs, "type_info.document")
	if document["page_count"] != 12 || document["word_count"] != 2400 || document["file_type_friendly"] != "PDF" {
		t.Fatalf("type_info.document = %#v", document)
	}
	pdfInfo := commonJSON.Section(attrs, "format_info.pdf")
	if pdfInfo["vendor_key"] != "kept" {
		t.Fatalf("format_info.pdf = %#v", pdfInfo)
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
			"srid":              4326,
			"extent":            []float64{100, 180, 120, 200},
			"has_spatial_index": false,
		},
	})

	media := commonJSON.Section(attrs, "type_info.media")
	if media["kind"] != "image" || media["width"] != 800 || media["height"] != 600 {
		t.Fatalf("type_info.media = %#v", media)
	}
	if media["duration_ms"] != duration || media["encoding"] != "tiff" || media["mime_type"] != "image/tiff" {
		t.Fatalf("type_info.media = %#v", media)
	}
	capabilities := attrs["capabilities"].(map[string]interface{})
	spatial := capabilities["spatial"].(map[string]interface{})
	if spatial["srid"] != 4326 || spatial["has_spatial_index"] != false {
		t.Fatalf("capabilities.spatial = %#v", spatial)
	}
	if attrs["spatial"] != nil {
		t.Fatalf("legacy flat spatial attr should not be written: %#v", attrs)
	}
}
