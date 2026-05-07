package service

import (
	"testing"

	"github.com/addp/meta/internal/models"
)

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
