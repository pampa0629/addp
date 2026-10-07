package sgm

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"testing"
)

func TestSGMIdentityHasNoGuessedGeometryProvider(t *testing.T) {
	d := NewPlugin().Descriptor()
	if d.Format != format.FormatSGM || d.DataType != datatype.Model3D || len(d.Layouts) != 1 || !format.HasLayout(d.Layouts, format.LayoutSingle) || len(d.Identification.Extensions) != 1 || d.Identification.Extensions[0] != ".sgm" {
		t.Fatalf("descriptor=%+v", d)
	}
	if _, err := format.GetModel3DInfoProvider(format.FormatSGM); err == nil {
		t.Fatal("identity-only plugin must not advertise a geometry parser")
	}
}
