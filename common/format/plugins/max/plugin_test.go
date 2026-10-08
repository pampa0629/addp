package max

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestMaxDescriptorDoesNotClaimRuntimeParsing(t *testing.T) {
	d := NewPlugin().Descriptor()
	if d.Format != format.FormatMAX || d.DataType != datatype.Model3D || len(d.Layouts) != 1 || d.Layouts[0] != format.LayoutSingle || len(d.Identification.Extensions) != 1 || d.Identification.Extensions[0] != ".max" {
		t.Fatalf("unexpected MAX descriptor: %#v", d)
	}
	if _, err := format.GetModel3DInfoProvider(format.FormatMAX); err == nil {
		t.Fatal("MAX identity must not claim Go geometry parsing")
	}
}
