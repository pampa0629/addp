package builtin

import (
	"testing"

	"github.com/addp/common/format"
)

func TestDocumentFormatCapabilityViewsReflectBackendParsingBoundary(t *testing.T) {
	tests := []struct {
		formatType     format.FormatType
		wantInfo       bool
		wantTextReader bool
	}{
		{formatType: format.FormatPDF, wantInfo: true},
		{formatType: format.FormatDOCX, wantTextReader: true},
		{formatType: format.FormatPPTX},
		{formatType: format.FormatWPS},
	}

	for _, tt := range tests {
		t.Run(string(tt.formatType), func(t *testing.T) {
			view, ok := format.GetFormatCapabilityView(tt.formatType)
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
