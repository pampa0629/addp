package rastermosaic

import (
	"strings"
	"testing"
)

func TestDecodeManifestValidatesSchema(t *testing.T) {
	payload := `{
		"schema_version":"addp.raster_mosaic.v1",
		"data_type":"media",
		"format":"raster_mosaic",
		"layout":"whole",
		"refs":{"index":"index/source-index.json","overview":"overviews/overview.cog.tif"},
		"summary":{"leaf_count":2,"source_count":2,"extent":[0,1,2,3],"source_crs":"EPSG:4326"}
	}`
	manifest, err := DecodeManifest(strings.NewReader(payload), 1<<20)
	if err != nil {
		t.Fatalf("DecodeManifest() error = %v", err)
	}
	if manifest.SchemaVersion != ManifestSchemaVersion || manifest.Refs.Index != SourceIndexRef {
		t.Fatalf("manifest = %#v", manifest)
	}
	if manifest.Summary.LeafCount != 2 || len(manifest.Summary.Extent) != 4 {
		t.Fatalf("manifest summary = %#v", manifest.Summary)
	}
}

func TestDecodeManifestRejectsWeakManifest(t *testing.T) {
	payload := `{"schema_version":"not-addp","format":"json"}`
	_, err := DecodeManifest(strings.NewReader(payload), 1<<20)
	if err == nil {
		t.Fatal("DecodeManifest() should reject non raster mosaic manifest")
	}
}

func TestNewManifestUsesCanonicalIdentity(t *testing.T) {
	manifest := NewManifest("srtm", "2026-06-26T00:00:00Z", SourceIndexRef, DefaultOverviewRef, ManifestSummary{LeafCount: 3}, map[string]interface{}{"leaf_cog": true})
	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("ValidateManifest() error = %v", err)
	}
	if manifest.DataType != "media" || manifest.Format != "raster_mosaic" || manifest.Layout != "whole" {
		t.Fatalf("manifest identity = %#v", manifest)
	}
}

func TestDecodeSourceIndexValidatesSchema(t *testing.T) {
	index, err := DecodeSourceIndex(strings.NewReader(`{
		"schema_version":"addp.raster_mosaic.source_index.v1",
		"leaf_count":1,
		"leaves":[{"id":"leaf-000001","leaf_ref":"leaf/a.cog.tif"}]
	}`), 0)
	if err != nil {
		t.Fatalf("DecodeSourceIndex() error = %v", err)
	}
	if index.LeafCount != 1 || len(index.Leaves) != 1 {
		t.Fatalf("index = %#v, want one leaf", index)
	}
}

func TestDecodeSourceIndexRejectsMissingLeafRef(t *testing.T) {
	_, err := DecodeSourceIndex(strings.NewReader(`{
		"schema_version":"addp.raster_mosaic.source_index.v1",
		"leaf_count":1,
		"leaves":[{"id":"leaf-000001"}]
	}`), 0)
	if err == nil {
		t.Fatal("DecodeSourceIndex() error = nil, want missing leaf_ref error")
	}
}
