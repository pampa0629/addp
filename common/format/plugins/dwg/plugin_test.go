package dwg

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"testing"
)

func TestDescriptorAndSniffer(t *testing.T) {
	plugin := NewPlugin()
	descriptor := plugin.Descriptor()
	if descriptor.Format != format.FormatDWG || descriptor.DataType != datatype.CAD {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	if len(descriptor.Layouts) != 1 || descriptor.Layouts[0] != format.LayoutSingle {
		t.Fatalf("layouts = %#v", descriptor.Layouts)
	}
	if !plugin.SniffFormat([]byte("AC1032")) || plugin.SniffFormat([]byte("not-dwg")) {
		t.Fatal("DWG header sniff mismatch")
	}
}
