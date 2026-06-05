package api

import (
	"context"
	"io"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
)

func TestBuildTableFormatCapabilitiesExposeUserFacingFormats(t *testing.T) {
	capabilities := buildTableFormatCapabilities()
	byValue := make(map[string]TransferTableFormatSupport, len(capabilities))
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
	if jsonl.Extension != "jsonl" {
		t.Fatalf("jsonl extension = %q, want jsonl", jsonl.Extension)
	}
	if jsonl.ProviderKind != "table" {
		t.Fatalf("jsonl provider_kind = %q, want table", jsonl.ProviderKind)
	}

	json := byValue["json"]
	if json.Spatial {
		t.Fatal("json spatial = true, want false")
	}

	geojson := byValue["geojson"]
	if geojson.BackendType != "geojson" {
		t.Fatalf("geojson backend_type = %q, want geojson", geojson.BackendType)
	}
	if len(geojson.Options) != 0 {
		t.Fatalf("geojson options = %#v, want empty", geojson.Options)
	}
	if !geojson.Spatial {
		t.Fatal("geojson spatial = false, want true")
	}
	if geojson.Extension != "geojson" {
		t.Fatalf("geojson extension = %q, want geojson", geojson.Extension)
	}
	if !geojson.Read || !geojson.Write {
		t.Fatalf("geojson read/write = %v/%v, want true/true from geojson provider implementations", geojson.Read, geojson.Write)
	}

	for value, want := range map[string]string{
		"csv":       "csv",
		"tsv":       "tsv",
		"json":      "json",
		"parquet":   "parquet",
		"shapefile": "shp",
	} {
		if got := byValue[value].Extension; got != want {
			t.Fatalf("%s extension = %q, want %s", value, got, want)
		}
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

func TestBuildTableFormatCapabilitiesIncludesRegisteredTableReaders(t *testing.T) {
	formatType := format.FormatType("transfer_capability_reader_only")
	if err := format.RegisterFormatPlugin(transferCapabilityReaderOnlyPlugin{formatType: formatType}); err != nil {
		t.Fatalf("RegisterFormatPlugin() error = %v", err)
	}

	capabilities := buildTableFormatCapabilities()
	byValue := make(map[string]TransferTableFormatSupport, len(capabilities))
	for _, capability := range capabilities {
		byValue[capability.Value] = capability
	}

	capability, ok := byValue[string(formatType)]
	if !ok {
		t.Fatalf("missing descriptor-derived table format capability %q; got %#v", formatType, capabilities)
	}
	if capability.BackendType != string(formatType) {
		t.Fatalf("backend_type = %q, want %s", capability.BackendType, formatType)
	}
	if !capability.Read || capability.Write {
		t.Fatalf("read/write = %v/%v, want true/false", capability.Read, capability.Write)
	}
	if capability.Extension != "tct" {
		t.Fatalf("extension = %q, want tct", capability.Extension)
	}
}

func TestBuildRawCopyFormatCapabilitiesExposeNonTableSingleFormats(t *testing.T) {
	capabilities := buildRawCopyFormatCapabilities()
	byValue := make(map[string]TransferRawCopyFormatSupport, len(capabilities))
	for _, capability := range capabilities {
		byValue[capability.Value] = capability
	}

	for value, dataType := range map[string]string{
		"pdf":     "document",
		"docx":    "document",
		"png":     "media",
		"jpeg":    "media",
		"mp4":     "media",
		"unknown": "unknown",
	} {
		capability, ok := byValue[value]
		if !ok {
			t.Fatalf("missing raw copy format capability %q; got %#v", value, capabilities)
		}
		if capability.DataType != dataType {
			t.Fatalf("%s data_type = %q, want %q", value, capability.DataType, dataType)
		}
		if !containsString(capability.Layouts, "single") {
			t.Fatalf("%s layouts = %#v, want single", value, capability.Layouts)
		}
	}

	if _, ok := byValue["csv"]; ok {
		t.Fatalf("raw copy capabilities include table format csv: %#v", byValue["csv"])
	}
	if byValue["pdf"].Extension != "pdf" {
		t.Fatalf("pdf extension = %q, want pdf", byValue["pdf"].Extension)
	}
}

type transferCapabilityReaderOnlyPlugin struct {
	formatType format.FormatType
}

func (p transferCapabilityReaderOnlyPlugin) Format() format.FormatType {
	return p.formatType
}

func (p transferCapabilityReaderOnlyPlugin) Descriptor() format.FormatDescriptor {
	return format.FormatDescriptor{
		ID:       "transfer-capability-reader-only",
		Format:   p.formatType,
		DataType: datatype.Table,
		Layouts:  []string{format.LayoutSingle},
		Identification: format.FormatIdentification{
			Extensions: []string{".tct"},
			MimeTypes:  []string{"application/x-transfer-capability-test"},
		},
	}
}

func (p transferCapabilityReaderOnlyPlugin) OpenTableReader(context.Context, io.Reader, *format.ParseOptions) (format.TableReader, error) {
	return nil, nil
}
