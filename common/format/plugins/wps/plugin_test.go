package wps

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestPluginDescriptorKeepsBackendParsingBoundary(t *testing.T) {
	plugin := NewPlugin()
	descriptor := plugin.Descriptor()
	if descriptor.Format != format.FormatWPS {
		t.Fatalf("descriptor format = %q, want %q", descriptor.Format, format.FormatWPS)
	}
	if descriptor.DataType != datatype.Document {
		t.Fatalf("descriptor data type = %q, want document", descriptor.DataType)
	}
	if _, ok := any(plugin).(format.DocumentInfoProvider); ok {
		t.Fatal("wps should not implement DocumentInfoProvider before backend parsing is defined")
	}
	if _, ok := any(plugin).(format.DocumentTextReader); ok {
		t.Fatal("wps should not implement DocumentTextReader before backend parsing is defined")
	}
}
