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
			"format": "xlsx",
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
			"content_type": "application/geo+json",
		},
	}

	got := provider.resolveFormat(req)
	if got != format.FormatGeoJSON {
		t.Fatalf("resolveFormat() = %q, want %q", got, format.FormatGeoJSON)
	}
}

func TestFileTablePreviewProviderResolveFormatFallsBackToFilename(t *testing.T) {
	provider := &FileTablePreviewProvider{}
	req := &PreviewRequest{
		Table: "roads.geojson",
	}

	got := provider.resolveFormat(req)
	if got != format.FormatGeoJSON {
		t.Fatalf("resolveFormat() = %q, want %q", got, format.FormatGeoJSON)
	}
}
