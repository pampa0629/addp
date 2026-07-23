package tilepyramid

import (
	"strings"
	"testing"
)

func TestDecodeManifestAndResolveTilePath(t *testing.T) {
	manifest, err := DecodeManifest(strings.NewReader(`{
      "schema_version":"addp.tile_pyramid.v1",
      "data_type":"media",
      "format":"tile_pyramid",
      "layout":"whole",
      "tile_kind":"vector",
      "tile_format":"mvt",
      "scheme":"xyz",
      "tile_matrix_set":"WebMercatorQuad",
      "tile_template":"tiles/z{z}/{x}_{y}.mvt.gz",
      "content_encoding":"gzip",
      "min_zoom":9,
      "max_zoom":18
    }`), 0)
	if err != nil {
		t.Fatalf("DecodeManifest() error = %v", err)
	}
	if manifest.ContentType != "application/vnd.mapbox-vector-tile" {
		t.Fatalf("content_type = %q", manifest.ContentType)
	}
	got, err := ResolveTilePath(manifest.TileTemplate, 9, 427, 209)
	if err != nil {
		t.Fatalf("ResolveTilePath() error = %v", err)
	}
	if got != "tiles/z9/427_209.mvt.gz" {
		t.Fatalf("path = %q", got)
	}
}

func TestValidateManifestRejectsEscapingTemplate(t *testing.T) {
	manifest := Manifest{
		SchemaVersion: ManifestSchemaVersion, DataType: "media", Format: "tile_pyramid", Layout: "whole",
		TileKind: TileKindVector, TileFormat: "mvt", Scheme: SchemeXYZ, TileMatrixSet: DefaultMatrixSet,
		TileTemplate: "../tiles/{z}/{x}/{y}.mvt", MinZoom: 0, MaxZoom: 1,
	}
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("ValidateManifest() accepted escaping tile template")
	}
}
