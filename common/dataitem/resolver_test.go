package dataitem

import (
	"testing"

	"github.com/addp/common/format"
)

func TestInferFormatCanonicalizesExplicitExtensionLikeValues(t *testing.T) {
	tests := []struct {
		name           string
		explicitFormat string
		want           string
	}{
		{name: "jpg to jpeg", explicitFormat: "jpg", want: string(format.FormatJPEG)},
		{name: "xlsx to excel", explicitFormat: "xlsx", want: string(format.FormatExcel)},
		{name: "gpkg to geopackage", explicitFormat: "gpkg", want: string(format.FormatGeoPackage)},
		{name: "dot extension", explicitFormat: ".tif", want: string(format.FormatTIFF)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferFormat("fallback.bin", "", tt.explicitFormat)
			if got != tt.want {
				t.Fatalf("InferFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInferFormatUsesMIMEBeforeFilenameFallback(t *testing.T) {
	got := InferFormat("unknown.bin", "image/png", "")
	if got != string(format.FormatPNG) {
		t.Fatalf("InferFormat() = %q, want %q", got, format.FormatPNG)
	}
}

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
	if item.DataType != DataTypeContainer {
		t.Fatalf("DataType = %q, want %q", item.DataType, DataTypeContainer)
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

	if item.Organization != OrganizationSingle {
		t.Fatalf("Organization = %q, want %q", item.Organization, OrganizationSingle)
	}
	if item.DataType != DataTypeContainer {
		t.Fatalf("DataType = %q, want %q", item.DataType, DataTypeContainer)
	}
	if item.ItemType != "file" {
		t.Fatalf("ItemType = %q, want file", item.ItemType)
	}
}

func TestInferSingleGeoJSONWritesSpatialCapability(t *testing.T) {
	item := InferSingleResource(SingleResourceInput{Name: "roads.geojson", Path: "roads.geojson"})
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

func TestInferDataTypeCanonicalizesCommonAliases(t *testing.T) {
	tests := []struct {
		formatName string
		want       DataType
	}{
		{formatName: "jpg", want: DataTypeMedia},
		{formatName: "xlsx", want: DataTypeContainer},
		{formatName: "gpkg", want: DataTypeContainer},
		{formatName: "orc", want: DataTypeTable},
	}

	for _, tt := range tests {
		t.Run(tt.formatName, func(t *testing.T) {
			got := InferDataType(tt.formatName, "")
			if got != tt.want {
				t.Fatalf("InferDataType() = %q, want %q", got, tt.want)
			}
		})
	}
}
