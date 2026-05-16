package format_test

import (
	"reflect"
	"testing"

	. "github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	formatregistry "github.com/addp/common/format/registry"
)

func TestListFormatCapabilitiesReturnsBuiltins(t *testing.T) {
	capabilities := ListFormatCapabilities()
	if len(capabilities) == 0 {
		t.Fatal("expected built-in format capabilities")
	}

	for i := 1; i < len(capabilities); i++ {
		if capabilities[i-1].Format > capabilities[i].Format {
			t.Fatalf("format capabilities are not sorted: %s before %s", capabilities[i-1].Format, capabilities[i].Format)
		}
	}
}

func TestListTransferFormatsForEngineFamilyReturnsObjectFormats(t *testing.T) {
	got := ListTransferFormatsForEngineFamily(EngineFamilyObject)
	want := []string{"avro", "csv", "json", "markdown", "orc", "parquet", "shapefile", "text", "tsv"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTransferFormatsForEngineFamily(%q) = %#v, want %#v", EngineFamilyObject, got, want)
	}
}

func TestListTransferFormatsForEngineFamilies(t *testing.T) {
	tests := []struct {
		name         string
		engineFamily string
		want         []string
	}{
		{name: "tabular", engineFamily: EngineFamilyTabular, want: []string{}},
		{name: "object", engineFamily: EngineFamilyObject, want: []string{"avro", "csv", "json", "markdown", "orc", "parquet", "shapefile", "text", "tsv"}},
		{name: "file", engineFamily: EngineFamilyFile, want: []string{"avro", "csv", "json", "markdown", "orc", "parquet", "shapefile", "text", "tsv"}},
		{name: "document", engineFamily: EngineFamilyDocument, want: []string{"json", "markdown", "text"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ListTransferFormatsForEngineFamily(tt.engineFamily)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("ListTransferFormatsForEngineFamily(%q) = %#v, want %#v", tt.engineFamily, got, tt.want)
			}
		})
	}
}

func TestMarkdownCapabilityIsDocumentTextFormat(t *testing.T) {
	capability, ok := GetFormatCapability(FormatMarkdown)
	if !ok {
		t.Fatal("expected markdown capability")
	}
	if capability.DataType != FormatDataTypeDocument {
		t.Fatalf("markdown DataType = %q, want %q", capability.DataType, FormatDataTypeDocument)
	}
	if !containsStringForCapabilityTest(capability.ProviderHints, FormatProviderDocument) {
		t.Fatalf("markdown ProviderHints = %#v, want document hint", capability.ProviderHints)
	}
	if !containsStringForCapabilityTest(capability.ContentReaders, formatregistry.ContentReaderDocumentText) {
		t.Fatalf("markdown ContentReaders = %#v, want document_text", capability.ContentReaders)
	}
	if capability.Parse {
		t.Fatal("markdown should not claim stable parse capability yet")
	}
}

func TestMediaCapabilitiesArePreviewOnly(t *testing.T) {
	for _, formatType := range []FormatType{FormatWebP, FormatSVG, FormatMP4, FormatMP3} {
		t.Run(string(formatType), func(t *testing.T) {
			capability, ok := GetFormatCapability(formatType)
			if !ok {
				t.Fatalf("expected %s capability", formatType)
			}
			if capability.DataType != FormatDataTypeMedia {
				t.Fatalf("DataType = %q, want %q", capability.DataType, FormatDataTypeMedia)
			}
			if !containsStringForCapabilityTest(capability.ProviderHints, FormatProviderMedia) {
				t.Fatalf("ProviderHints = %#v, want media", capability.ProviderHints)
			}
			if !containsStringForCapabilityTest(capability.ContentReaders, formatregistry.ContentReaderRawContent) ||
				!containsStringForCapabilityTest(capability.ContentReaders, formatregistry.ContentReaderRangeContent) {
				t.Fatalf("ContentReaders = %#v, want raw and range", capability.ContentReaders)
			}
			if capability.TransferRead || capability.TransferWrite {
				t.Fatalf("media capability should not declare transfer: %#v", capability)
			}
		})
	}
}

func TestJSONCapabilityCarriesSpatialProviderHint(t *testing.T) {
	capability, ok := GetFormatCapability(FormatJSON)
	if !ok {
		t.Fatal("expected json capability")
	}
	if capability.DataType != FormatDataTypeDocument {
		t.Fatalf("json DataType = %q, want %q", capability.DataType, FormatDataTypeDocument)
	}
	if !containsStringForCapabilityTest(capability.ProviderHints, FormatProviderSpatial) {
		t.Fatalf("json ProviderHints = %#v, want spatial hint", capability.ProviderHints)
	}
	if !containsStringForCapabilityTest(capability.Extensions, ".geojson") {
		t.Fatalf("json Extensions = %#v, want .geojson", capability.Extensions)
	}
	if !containsStringForCapabilityTest(capability.ContentReaders, formatregistry.ContentReaderTableSample) {
		t.Fatalf("json ContentReaders = %#v, want table_sample", capability.ContentReaders)
	}
	if !containsStringForCapabilityTest(capability.ContentReaders, formatregistry.ContentReaderDocumentText) {
		t.Fatalf("json ContentReaders = %#v, want document_text", capability.ContentReaders)
	}
}

func TestGetFormatCapabilityReturnsClone(t *testing.T) {
	capability, ok := GetFormatCapability(FormatCSV)
	if !ok {
		t.Fatal("expected csv capability")
	}
	capability.Extensions[0] = ".changed"
	capability.ContentReaders[0] = "changed"

	got, ok := GetFormatCapability(FormatCSV)
	if !ok {
		t.Fatal("expected csv capability")
	}
	if got.Extensions[0] == ".changed" {
		t.Fatal("format capability registry returned mutable internal slices")
	}
	if got.ContentReaders[0] == "changed" {
		t.Fatal("format capability registry returned mutable content reader slices")
	}
}

func containsStringForCapabilityTest(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
