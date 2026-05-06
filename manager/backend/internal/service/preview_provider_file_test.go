package service

import (
	"testing"

	"github.com/addp/common/format"
)

func TestFileTablePreviewProviderResolveFormatUsesMetaFormat(t *testing.T) {
	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Table: "fallback.bin",
		Attributes: map[string]interface{}{
			"item": map[string]interface{}{
				"format": "xlsx",
			},
		},
	}

	got := provider.resolveFormat(req)
	if got != format.FormatExcel {
		t.Fatalf("resolveFormat() = %q, want %q", got, format.FormatExcel)
	}
}

func TestFileTablePreviewProviderResolveFormatUsesContentType(t *testing.T) {
	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Table: "fallback.bin",
		Attributes: map[string]interface{}{
			"storage": map[string]interface{}{
				"content_type": "application/geo+json",
			},
		},
	}

	got := provider.resolveFormat(req)
	if got != format.FormatGeoJSON {
		t.Fatalf("resolveFormat() = %q, want %q", got, format.FormatGeoJSON)
	}
}

func TestFileTablePreviewProviderResolveFormatDoesNotFallbackToFilename(t *testing.T) {
	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Table: "roads.geojson",
	}

	got := provider.resolveFormat(req)
	if got != format.FormatUnknown {
		t.Fatalf("resolveFormat() = %q, want %q", got, format.FormatUnknown)
	}
}

func TestFileTablePreviewProviderBuildParseOptionsUsesResolvedFormat(t *testing.T) {
	provider := &FileTablePreviewProvider{}

	if got := provider.buildParseOptions(format.FormatTSV).Delimiter; got != '\t' {
		t.Fatalf("TSV delimiter = %q, want tab", got)
	}
	if got := provider.buildParseOptions(format.FormatCSV).Delimiter; got != ',' {
		t.Fatalf("CSV delimiter = %q, want comma", got)
	}
}
