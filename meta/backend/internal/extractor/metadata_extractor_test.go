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
