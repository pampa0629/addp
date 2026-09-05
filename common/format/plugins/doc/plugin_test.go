package doc

import (
	"testing"

	"github.com/addp/common/format"
)

func TestDescriptor(t *testing.T) {
	plugin := NewPlugin()
	descriptor := plugin.Descriptor()

	if descriptor.Format != format.FormatDOC {
		t.Fatalf("descriptor format = %q, want %q", descriptor.Format, format.FormatDOC)
	}
	if len(descriptor.Identification.Extensions) != 1 || descriptor.Identification.Extensions[0] != ".doc" {
		t.Fatalf("descriptor extensions = %#v, want [.doc]", descriptor.Identification.Extensions)
	}
	if _, ok := any(plugin).(format.DocumentInfoProvider); ok {
		t.Fatal("doc should not implement DocumentInfoProvider before backend parsing is defined")
	}
	if _, ok := any(plugin).(format.DocumentTextReader); ok {
		t.Fatal("doc should not implement DocumentTextReader before backend parsing is defined")
	}
}
