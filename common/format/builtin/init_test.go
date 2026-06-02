package builtin

import (
	"strings"
	"testing"

	"github.com/addp/common/format"
)

func TestDocumentFormatSupportViewsReflectBackendParsingBoundary(t *testing.T) {
	tests := []struct {
		formatType     format.FormatType
		wantInfo       bool
		wantTextReader bool
	}{
		{formatType: format.FormatPDF, wantInfo: true},
		{formatType: format.FormatDOCX, wantInfo: true, wantTextReader: true},
		{formatType: format.FormatPPTX, wantInfo: true, wantTextReader: true},
		{formatType: format.FormatWPS},
	}

	for _, tt := range tests {
		t.Run(string(tt.formatType), func(t *testing.T) {
			view, ok := format.GetFormatSupportView(tt.formatType)
			if !ok {
				t.Fatalf("expected capability view for %s", tt.formatType)
			}
			if !view.Implementations.FormatPlugin {
				t.Fatalf("%s implementations = %#v, want format plugin", tt.formatType, view.Implementations)
			}
			if view.Implementations.DocumentInfoProvider != tt.wantInfo {
				t.Fatalf("%s document info provider = %v, want %v", tt.formatType, view.Implementations.DocumentInfoProvider, tt.wantInfo)
			}
			if view.Implementations.DocumentTextReader != tt.wantTextReader {
				t.Fatalf("%s document text reader = %v, want %v", tt.formatType, view.Implementations.DocumentTextReader, tt.wantTextReader)
			}
		})
	}
}

func TestUnknownFormatSupportViewRegistersBinaryReader(t *testing.T) {
	reader, err := format.GetBinaryContentReader(format.FormatUnknown)
	if err != nil {
		t.Fatalf("GetBinaryContentReader(unknown) error = %v", err)
	}
	content, err := reader.ReadBinaryContent(t.Context(), strings.NewReader("abc"), 2, nil)
	if err != nil {
		t.Fatalf("ReadBinaryContent() error = %v", err)
	}
	if string(content.Bytes) != "ab" || !content.Truncated {
		t.Fatalf("content = %#v, want truncated ab", content)
	}

	view, ok := format.GetFormatSupportView(format.FormatUnknown)
	if !ok {
		t.Fatal("expected unknown capability view")
	}
	if !view.Implementations.FormatPlugin {
		t.Fatalf("unknown implementations = %#v, want format plugin", view.Implementations)
	}
	if !view.Implementations.BinaryContentReader {
		t.Fatalf("unknown implementations = %#v, want binary content reader", view.Implementations)
	}
	if len(view.ContentReaders) != 1 || view.ContentReaders[0] != string(format.ContentReaderBinaryContent) {
		t.Fatalf("unknown content readers = %#v, want binary_content only", view.ContentReaders)
	}
}

func TestDescriptorOnlyTableFormatsExposeMissingProviders(t *testing.T) {
	for _, formatType := range []format.FormatType{format.FormatAvro, format.FormatORC} {
		t.Run(string(formatType), func(t *testing.T) {
			view, ok := format.GetFormatSupportView(formatType)
			if !ok {
				t.Fatalf("expected capability view for %s", formatType)
			}
			if !view.Implementations.FormatPlugin {
				t.Fatalf("%s implementations = %#v, want descriptor plugin", formatType, view.Implementations)
			}
			if view.Implementations.TableInfoProvider ||
				view.Implementations.TableSampleReader ||
				view.Implementations.TableReaderProvider ||
				view.Implementations.TableWriterProvider ||
				view.Implementations.ScopeTableReader {
				t.Fatalf("%s should expose missing table providers until implemented: %#v", formatType, view.Implementations)
			}
		})
	}
}
