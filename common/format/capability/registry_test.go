package capability

import (
	"reflect"
	"testing"

	formatregistry "github.com/addp/common/format/registry"
)

func TestListSorted(t *testing.T) {
	capabilities := List()
	if len(capabilities) == 0 {
		t.Fatal("expected built-in format capabilities")
	}

	for i := 1; i < len(capabilities); i++ {
		if capabilities[i-1].Format > capabilities[i].Format {
			t.Fatalf("format capabilities are not sorted: %s before %s", capabilities[i-1].Format, capabilities[i].Format)
		}
	}
}

func TestListTransferFormatsForEngineFamily(t *testing.T) {
	tests := []struct {
		name         string
		engineFamily string
		want         []string
	}{
		{
			name:         "tabular",
			engineFamily: EngineFamilyTabular,
			want:         []string{"table"},
		},
		{
			name:         "object",
			engineFamily: EngineFamilyObject,
			want:         []string{"avro", "csv", "json", "markdown", "orc", "parquet", "shapefile", "text", "tsv"},
		},
		{
			name:         "file",
			engineFamily: EngineFamilyFile,
			want:         []string{"avro", "csv", "json", "markdown", "orc", "parquet", "shapefile", "text", "tsv"},
		},
		{
			name:         "document",
			engineFamily: EngineFamilyDocument,
			want:         []string{"document", "json", "markdown", "text"},
		},
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
	capability, ok := Get(FormatMarkdown)
	if !ok {
		t.Fatal("expected markdown capability")
	}
	if capability.DataType != DataTypeDocument {
		t.Fatalf("markdown DataType = %q, want %q", capability.DataType, DataTypeDocument)
	}
	if !containsString(capability.ProviderHints, ProviderDocument) {
		t.Fatalf("markdown ProviderHints = %#v, want document hint", capability.ProviderHints)
	}
	if !containsString(capability.ContentReaders, formatregistry.ContentReaderDocumentText) {
		t.Fatalf("markdown ContentReaders = %#v, want document_text", capability.ContentReaders)
	}
	if capability.Parse {
		t.Fatal("markdown should not claim stable parse capability yet")
	}
}

func TestTextCapabilityIsDocumentTextFormat(t *testing.T) {
	capability, ok := Get(FormatText)
	if !ok {
		t.Fatal("expected text capability")
	}
	if capability.DataType != DataTypeDocument {
		t.Fatalf("text DataType = %q, want %q", capability.DataType, DataTypeDocument)
	}
	if !containsString(capability.ProviderHints, ProviderDocument) {
		t.Fatalf("text ProviderHints = %#v, want document hint", capability.ProviderHints)
	}
	if !containsString(capability.ContentReaders, formatregistry.ContentReaderDocumentText) {
		t.Fatalf("text ContentReaders = %#v, want document_text", capability.ContentReaders)
	}
	if capability.Parse {
		t.Fatal("text should not claim stable parse capability yet")
	}
}

func TestMediaCapabilitiesArePreviewOnly(t *testing.T) {
	tests := []Format{
		FormatWebP,
		FormatSVG,
		FormatMP4,
		FormatMP3,
	}
	for _, format := range tests {
		t.Run(string(format), func(t *testing.T) {
			capability, ok := Get(format)
			if !ok {
				t.Fatalf("expected %s capability", format)
			}
			if capability.DataType != DataTypeMedia {
				t.Fatalf("DataType = %q, want %q", capability.DataType, DataTypeMedia)
			}
			if !containsString(capability.ProviderHints, ProviderMedia) {
				t.Fatalf("ProviderHints = %#v, want media", capability.ProviderHints)
			}
			if !containsString(capability.ContentReaders, formatregistry.ContentReaderRawContent) ||
				!containsString(capability.ContentReaders, formatregistry.ContentReaderRangeContent) {
				t.Fatalf("ContentReaders = %#v, want raw and range", capability.ContentReaders)
			}
			if capability.TransferRead || capability.TransferWrite {
				t.Fatalf("media capability should not declare transfer: %#v", capability)
			}
		})
	}
}

func TestJSONCapabilityCarriesSpatialProviderHint(t *testing.T) {
	capability, ok := Get(FormatJSON)
	if !ok {
		t.Fatal("expected json capability")
	}
	if capability.DataType != DataTypeDocument {
		t.Fatalf("json DataType = %q, want %q", capability.DataType, DataTypeDocument)
	}
	if !containsString(capability.ProviderHints, ProviderSpatial) {
		t.Fatalf("json ProviderHints = %#v, want spatial hint", capability.ProviderHints)
	}
	if !containsString(capability.Extensions, ".geojson") {
		t.Fatalf("json Extensions = %#v, want .geojson", capability.Extensions)
	}
	if !containsString(capability.ContentReaders, formatregistry.ContentReaderTableSample) {
		t.Fatalf("json ContentReaders = %#v, want table_sample", capability.ContentReaders)
	}
	if !containsString(capability.ContentReaders, formatregistry.ContentReaderDocumentText) {
		t.Fatalf("json ContentReaders = %#v, want document_text", capability.ContentReaders)
	}
}

func TestGetReturnsClone(t *testing.T) {
	capability, ok := Get(FormatCSV)
	if !ok {
		t.Fatal("expected csv capability")
	}
	capability.Extensions[0] = ".changed"
	capability.ContentReaders[0] = "changed"

	got, ok := Get(FormatCSV)
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
