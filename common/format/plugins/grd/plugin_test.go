package grd

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestSurferGridIdentityAndNativeFactory(t *testing.T) {
	for _, name := range []string{"height.grd", "height.GRD"} {
		if got := format.DetectFormat(name, nil); got != format.FormatGRD {
			t.Fatal(name, got)
		}
	}
	for _, signature := range []string{"DSRB\x04\x00\x00\x00", "DSBB\x05\x00\x03\x00", "DSAA\n5 3\n"} {
		if got := format.DetectFormat("no-extension", []byte(signature)); got != format.FormatGRD {
			t.Fatal(got)
		}
	}
	d := (&Plugin{}).Descriptor()
	if d.DataType != datatype.Media || len(d.Layouts) != 1 || d.Layouts[0] != format.LayoutSingle {
		t.Fatal(d)
	}
	if _, err := format.GetRuntimeMediaInfoProviderFactory(format.FormatGRD); err != nil {
		t.Fatal(err)
	}
}
