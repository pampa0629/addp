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
