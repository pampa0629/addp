package tilepyramid

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestDescriptor(t *testing.T) {
	descriptor := (&Plugin{}).Descriptor()
	if descriptor.Format != format.FormatTilePyramid || descriptor.DataType != datatype.Media {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	if len(descriptor.Layouts) != 1 || descriptor.Layouts[0] != format.LayoutWhole {
		t.Fatalf("layouts = %#v", descriptor.Layouts)
	}
	if len(descriptor.Identification.FileNames) != 1 || descriptor.Identification.FileNames[0] != "tiles.addp.json" {
		t.Fatalf("file names = %#v", descriptor.Identification.FileNames)
	}
}
