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

func TestInferSingleFileUsesCanonicalFormatForFamily(t *testing.T) {
	item := InferSingleFile(SingleFileInput{
		Name:   "sheet.bin",
		Path:   "bucket/sheet.xlsx",
		Size:   42,
		Format: "xlsx",
	})

	if item.Format != string(format.FormatExcel) {
		t.Fatalf("Format = %q, want %q", item.Format, format.FormatExcel)
	}
	if item.DataFamily != DataFamilyTabular {
		t.Fatalf("DataFamily = %q, want %q", item.DataFamily, DataFamilyTabular)
	}
}

func TestInferSingleFileDetectsContainerComposition(t *testing.T) {
	item := InferSingleFile(SingleFileInput{
		Name: "data.gpkg",
		Path: "bucket/data.gpkg",
		Size: 42,
	})

	if item.CompositionType != CompositionTypeContainerFile {
		t.Fatalf("CompositionType = %q, want %q", item.CompositionType, CompositionTypeContainerFile)
	}
	if item.DataFamily != DataFamilyTabular {
		t.Fatalf("DataFamily = %q, want %q", item.DataFamily, DataFamilyTabular)
	}
}

func TestBuildAttributesWritesPartitionedItemAndStorage(t *testing.T) {
	item := InferSingleFile(SingleFileInput{
		Name:        "roads.geojson",
		Path:        "bucket/roads.geojson",
		Size:        42,
		ContentType: "application/geo+json",
	})

	attrs := BuildAttributes(item)
	itemAttrs := attrs["item"].(map[string]interface{})
	if itemAttrs["composition_type"] != string(CompositionTypeSingleFile) {
		t.Fatalf("item.composition_type = %v, want %s", itemAttrs["composition_type"], CompositionTypeSingleFile)
	}
	if itemAttrs["data_family"] != string(DataFamilyTabular) {
		t.Fatalf("item.data_family = %v, want %s", itemAttrs["data_family"], DataFamilyTabular)
	}
	if itemAttrs["format"] != string(format.FormatGeoJSON) {
		t.Fatalf("item.format = %v, want %s", itemAttrs["format"], format.FormatGeoJSON)
	}
	if attrs["data_family"] != itemAttrs["data_family"] {
		t.Fatalf("flat data_family = %v, want partition-compatible value", attrs["data_family"])
	}

	storageAttrs := attrs["storage"].(map[string]interface{})
	if storageAttrs["physical_path"] != "bucket/roads.geojson" {
		t.Fatalf("storage.physical_path = %v, want bucket/roads.geojson", storageAttrs["physical_path"])
	}
	if storageAttrs["total_size"] != int64(42) {
		t.Fatalf("storage.total_size = %v, want 42", storageAttrs["total_size"])
	}
}

func TestInferDataFamilyCanonicalizesCommonAliases(t *testing.T) {
	tests := []struct {
		formatName string
		want       DataFamily
	}{
		{formatName: "jpg", want: DataFamilyImage},
		{formatName: "xlsx", want: DataFamilyTabular},
		{formatName: "gpkg", want: DataFamilyTabular},
		{formatName: "orc", want: DataFamilyTabular},
	}

	for _, tt := range tests {
		t.Run(tt.formatName, func(t *testing.T) {
			got := InferDataFamily(tt.formatName, "")
			if got != tt.want {
				t.Fatalf("InferDataFamily() = %q, want %q", got, tt.want)
			}
		})
	}
}
