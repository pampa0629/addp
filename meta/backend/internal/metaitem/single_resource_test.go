package metaitem

import (
	"testing"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/format"
)

func TestInferSingleResourceUsesCanonicalFormatForFamily(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{
		Name:   "sheet.bin",
		Path:   "bucket/sheet.xlsx",
		Size:   42,
		Format: "xlsx",
	})

	if item.Format != string(format.FormatExcel) {
		t.Fatalf("Format = %q, want %q", item.Format, format.FormatExcel)
	}
	if item.DataType != dataitem.DataTypeContainer {
		t.Fatalf("DataType = %q, want %q", item.DataType, dataitem.DataTypeContainer)
	}
	if item.ItemType != "file" {
		t.Fatalf("ItemType = %q, want file", item.ItemType)
	}
}

func TestInferSingleResourceDetectsContainerComposition(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{
		Name: "data.gpkg",
		Path: "bucket/data.gpkg",
		Size: 42,
	})

	if item.Organization != dataitem.OrganizationSingle {
		t.Fatalf("Organization = %q, want %q", item.Organization, dataitem.OrganizationSingle)
	}
	if item.DataType != dataitem.DataTypeContainer {
		t.Fatalf("DataType = %q, want %q", item.DataType, dataitem.DataTypeContainer)
	}
	if item.ItemType != "file" {
		t.Fatalf("ItemType = %q, want file", item.ItemType)
	}
}

func TestInferSingleGeoJSONWritesSpatialCapability(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{Name: "roads.geojson", Path: "roads.geojson"})
	if item.Format != string(format.FormatJSON) {
		t.Fatalf("Format = %q, want %q", item.Format, format.FormatJSON)
	}
	if item.DataType != dataitem.DataTypeTable {
		t.Fatalf("DataType = %q, want %q", item.DataType, dataitem.DataTypeTable)
	}
	if item.ItemType != "table" {
		t.Fatalf("ItemType = %q, want table", item.ItemType)
	}
	spatial := item.Attributes["capabilities"].(map[string]interface{})["spatial"].(map[string]interface{})
	if spatial["primary_geometry_column"] != "geometry" || spatial["has_spatial_index"] != false {
		t.Fatalf("capabilities.spatial = %#v", spatial)
	}
	columns := spatial["geometry_columns"].([]map[string]interface{})
	if len(columns) != 1 || columns[0]["srid"] != 4326 || columns[0]["geometry_type"] != "geometry" {
		t.Fatalf("geometry_columns = %#v", columns)
	}
}

func TestInferSingleTIFFWritesRasterSpatialShell(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{Name: "scene.tif", Path: "scene.tif"})
	spatial := item.Attributes["capabilities"].(map[string]interface{})["spatial"].(map[string]interface{})
	if _, ok := spatial["extent"]; !ok {
		t.Fatalf("capabilities.spatial.extent missing: %#v", spatial)
	}
	if spatial["has_spatial_index"] != false {
		t.Fatalf("capabilities.spatial = %#v", spatial)
	}
}
