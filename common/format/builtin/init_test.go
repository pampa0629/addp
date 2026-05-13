package builtin

import (
	"testing"

	"github.com/addp/common/format"
)

func TestDocumentFormatCapabilityViewsReflectBackendParsingBoundary(t *testing.T) {
	tests := []struct {
		formatType       format.FormatType
		wantInfoProvider bool
	}{
		{format.FormatPDF, true},
		{format.FormatDOCX, false},
		{format.FormatPPTX, false},
		{format.FormatWPS, false},
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
			if view.Implementations.DocumentInfoProvider != tt.wantInfoProvider {
				t.Fatalf("%s document info provider = %v, want %v", tt.formatType, view.Implementations.DocumentInfoProvider, tt.wantInfoProvider)
			}
			if view.Implementations.DocumentTextReader {
				t.Fatalf("%s should not claim backend document text reader", tt.formatType)
			}
		})
	}
}
