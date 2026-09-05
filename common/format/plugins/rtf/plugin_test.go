package rtf

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestPluginDescriptorKeepsFrontendParsingBoundary(t *testing.T) {
	plugin := NewPlugin()
	descriptor := plugin.Descriptor()
	if descriptor.Format != format.FormatRTF {
		t.Fatalf("descriptor format = %q, want %q", descriptor.Format, format.FormatRTF)
	}
	if descriptor.DataType != datatype.Document || !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("descriptor = %#v, want single document", descriptor)
	}
	if _, ok := any(plugin).(format.DocumentInfoProvider); ok {
		t.Fatal("rtf should not implement DocumentInfoProvider while parsing is frontend-only")
	}
	if _, ok := any(plugin).(format.DocumentTextReader); ok {
		t.Fatal("rtf should not implement DocumentTextReader while parsing is frontend-only")
	}
}
