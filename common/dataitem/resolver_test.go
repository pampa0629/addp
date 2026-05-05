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
