package osgb

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestOSGBDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatOSGB {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatOSGB)
	}
	if descriptor.DataType != datatype.Model3D {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.Model3D)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("Layouts = %#v, want single", descriptor.Layouts)
	}
	if len(descriptor.Identification.Extensions) != 1 || descriptor.Identification.Extensions[0] != ".osgb" {
		t.Fatalf("Extensions = %#v, want .osgb", descriptor.Identification.Extensions)
	}
	if len(descriptor.Identification.FileNames) != 0 {
		t.Fatalf("FileNames = %#v, want no whole-scope manifest identifier", descriptor.Identification.FileNames)
	}
}
