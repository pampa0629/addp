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

	for value, expected := range map[string][2]bool{
		"pgeo":    {true, false},
		"filegdb": {true, true},
	} {
		capability, ok := byValue[value]
		if !ok {
			t.Fatalf("missing runtime scope table format capability %q; got %#v", value, capabilities)
		}
		if capability.Read != expected[0] || capability.Write != expected[1] {
			t.Fatalf("%s read/write = %v/%v, want %v/%v", value, capability.Read, capability.Write, expected[0], expected[1])
		}
		if expected[1] && capability.ProviderKind != "runtime_scope" {
			t.Fatalf("%s provider_kind = %q, want runtime_scope", value, capability.ProviderKind)
		}
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
		"dwg":     "cad",
		"dxf":     "cad",
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

func TestBuildContinuousCapabilitiesPublishesDeadLetterAndBoundedReplay(t *testing.T) {
	continuous := buildContinuousCapabilities()
	if len(continuous.DatabaseCDC.Sources) != 3 || continuous.DatabaseCDC.Sources[0] != "postgresql" || continuous.DatabaseCDC.Sources[1] != "mysql" || continuous.DatabaseCDC.Sources[2] != "oracle" ||
		len(continuous.DatabaseCDC.Targets) != 3 || continuous.DatabaseCDC.Targets[0] != "postgresql" || continuous.DatabaseCDC.Targets[1] != "mysql" || continuous.DatabaseCDC.Targets[2] != "oracle" ||
		len(continuous.DatabaseCDC.Bootstrap) != 1 || continuous.DatabaseCDC.Bootstrap[0] != "initial_snapshot" ||
		continuous.DatabaseCDC.ApplyMode != "upsert_delete" {
		t.Fatalf("database CDC capability = %#v", continuous.DatabaseCDC)
	}
	capabilities := continuous.BusinessKafka
	if !containsString(capabilities.RecordFailureModes, "block") || !containsString(capabilities.RecordFailureModes, "dead_letter") {
		t.Fatalf("record failure modes = %#v", capabilities.RecordFailureModes)
	}
	if !capabilities.DeadLetters.Supported || capabilities.DeadLetters.ExposesPayload ||
		capabilities.DeadLetters.ListEndpoint != "/api/v1/transfer/task-definitions/{id}/dead-letters" ||
		capabilities.DeadLetters.DetailEndpoint != "/api/v1/transfer/task-definitions/{id}/dead-letters/{identity}" {
		t.Fatalf("dead-letter capability = %#v", capabilities.DeadLetters)
	}
	for _, filter := range []string{"source_partition", "error_category", "error_code", "payload_available"} {
		if !containsString(capabilities.DeadLetters.Filters, filter) {
			t.Fatalf("dead-letter filters = %#v, missing %q", capabilities.DeadLetters.Filters, filter)
		}
	}
	if !capabilities.BoundedReplay.Supported || capabilities.BoundedReplay.Endpoint != "/api/v1/transfer/task-definitions/{id}/replay" {
		t.Fatalf("bounded replay capability = %#v", capabilities.BoundedReplay)
	}
	if len(capabilities.BoundedReplay.OwnerRecordFailureModes) != 1 || capabilities.BoundedReplay.OwnerRecordFailureModes[0] != "block" {
		t.Fatalf("bounded replay owner modes = %#v", capabilities.BoundedReplay.OwnerRecordFailureModes)
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
