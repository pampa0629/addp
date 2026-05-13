package pptx

import (
	"testing"

	"github.com/addp/common/format"
)

func TestPluginDescriptorKeepsRawRangeBoundary(t *testing.T) {
	plugin := NewPlugin()
	descriptor := plugin.Descriptor()
	if descriptor.Format != format.FormatPPTX {
		t.Fatalf("descriptor format = %q, want %q", descriptor.Format, format.FormatPPTX)
	}
	if descriptor.DataType != format.FormatDataTypeDocument {
		t.Fatalf("descriptor data type = %q, want document", descriptor.DataType)
	}
	if descriptor.Providers.DocumentInfo {
		t.Fatalf("pptx should not declare document info provider before backend parsing is defined")
	}
	if !contains(descriptor.ContentReaders, string(format.ContentReaderRawContent)) ||
		!contains(descriptor.ContentReaders, string(format.ContentReaderRangeContent)) {
		t.Fatalf("content readers = %#v, want raw and range", descriptor.ContentReaders)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
