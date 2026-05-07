package metaattr

import (
	"testing"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/models"
)

func TestSpatialMetadataWritesMinimalCapabilitiesSpatial(t *testing.T) {
	t.Parallel()

	values := SpatialCapabilityFromMetadata(&models.SpatialMetadata{
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

func TestBuildDocumentCollectionAttributesWritesTypeInfoTableSection(t *testing.T) {
	t.Parallel()

	attrs := BuildDocumentCollectionAttributes(&plugin.ItemMetadata{
		Fields: []plugin.FieldInfo{{
			Name: "name",
			Type: "string",
		}},
		Indexes: []plugin.IndexInfo{{
			Name:      "name_idx",
			Fields:    []string{"name"},
			IndexType: "btree",
		}},
	})

	typeInfo := attrs["type_info"].(map[string]interface{})
	table := typeInfo["table"].(map[string]interface{})
	if _, ok := table["fields"]; !ok {
		t.Fatalf("type_info.table.fields missing: %#v", table)
	}
	if _, ok := table["indexes"]; !ok {
		t.Fatalf("type_info.table.indexes missing: %#v", table)
	}
	if attrs["fields"] != nil || attrs["indexes"] != nil || attrs["schema"] != nil {
		t.Fatalf("legacy flat/schema fields should not be written: %#v", attrs)
	}
}

func TestApplyNoSQLDataItemAttributesDoesNotWriteEngineFormat(t *testing.T) {
	t.Parallel()

	attrs := models.JSONMap{}
	ApplyNoSQLDataItemAttributes(attrs, "collection")

	item := attrs["item"].(map[string]interface{})
	if item["organization"] != "single" || item["data_type"] != "table" {
		t.Fatalf("item attrs = %#v", item)
	}
	if item["format"] != nil {
		t.Fatalf("native NoSQL item should not write item.format: %#v", item)
	}
}
