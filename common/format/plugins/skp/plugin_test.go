package skp

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestSketchUpDescriptorDoesNotClaimRuntimeParsing(t *testing.T) {
	d := NewPlugin().Descriptor()
	if d.Format != format.FormatSKP || d.DataType != datatype.Model3D || len(d.Layouts) != 1 || d.Layouts[0] != format.LayoutSingle || len(d.Identification.Extensions) != 1 || d.Identification.Extensions[0] != ".skp" {
		t.Fatalf("unexpected SKP descriptor: %#v", d)
	}
	if _, err := format.GetModel3DInfoProvider(format.FormatSKP); err == nil {
		t.Fatal("SKP identity must not claim Go geometry parsing")
	}
}
