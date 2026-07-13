package dxf

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestDescriptorAndSniffer(t *testing.T) {
	plugin := NewPlugin()
	descriptor := plugin.Descriptor()
	if descriptor.Format != format.FormatDXF || descriptor.DataType != datatype.CAD {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	if len(descriptor.Layouts) != 1 || descriptor.Layouts[0] != format.LayoutSingle {
		t.Fatalf("layouts = %#v", descriptor.Layouts)
	}
	if !plugin.SniffFormat([]byte("  0\r\nSECTION\r\n  2\r\nHEADER")) {
		t.Fatal("ASCII DXF header was not recognized")
	}
	if !plugin.SniffFormat([]byte("AutoCAD Binary DXF\r\n\x1a\x00")) {
		t.Fatal("binary DXF header was not recognized")
	}
	if plugin.SniffFormat([]byte("not-dxf")) {
		t.Fatal("invalid DXF content was recognized")
	}
}
