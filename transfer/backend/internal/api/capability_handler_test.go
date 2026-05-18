package api

import (
	"testing"

	_ "github.com/addp/common/format/builtin"
)

func TestBuildTableFormatCapabilitiesExposeUserFacingFormats(t *testing.T) {
	capabilities := buildTableFormatCapabilities()
	byValue := make(map[string]TransferTableFormatCapability, len(capabilities))
	for _, capability := range capabilities {
		byValue[capability.Value] = capability
	}

	for _, value := range []string{"csv", "tsv", "json", "jsonl", "geojson", "parquet", "shapefile"} {
		if _, ok := byValue[value]; !ok {
			t.Fatalf("missing table format capability %q; got %#v", value, capabilities)
		}
	}

	jsonl := byValue["jsonl"]
	if jsonl.BackendType != "json" {
		t.Fatalf("jsonl backend_type = %q, want json", jsonl.BackendType)
	}
	if got := jsonl.Options["json_mode"]; got != "jsonl" {
		t.Fatalf("jsonl options[json_mode] = %#v, want jsonl", got)
	}
	if !jsonl.Read || !jsonl.Write {
		t.Fatalf("jsonl read/write = %v/%v, want true/true", jsonl.Read, jsonl.Write)
	}

	geojson := byValue["geojson"]
	if geojson.BackendType != "json" {
		t.Fatalf("geojson backend_type = %q, want json", geojson.BackendType)
	}
	if got := geojson.Options["spatial.target_encoding"]; got != "geojson" {
		t.Fatalf("geojson options[spatial.target_encoding] = %#v, want geojson", got)
	}
	if !geojson.Spatial {
		t.Fatal("geojson spatial = false, want true")
	}

	shapefile := byValue["shapefile"]
	if shapefile.ProviderKind != "multi_table" {
		t.Fatalf("shapefile provider_kind = %q, want multi_table", shapefile.ProviderKind)
	}
	if !shapefile.MultiFile {
		t.Fatal("shapefile multi_file = false, want true")
	}
	if !shapefile.Spatial {
		t.Fatal("shapefile spatial = false, want true")
	}
}
