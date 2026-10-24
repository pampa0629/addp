package jpeg2000

import (
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestJPEG2000IdentityAndProvider(t *testing.T) {
	for _, name := range []string{"scene.jp2", "scene.JP2", "scene.j2k"} {
		if got := format.DetectFormat(name, nil); got != format.FormatJPEG2000 {
			t.Fatalf("%s: %s", name, got)
		}
	}
	for _, header := range [][]byte{{0, 0, 0, 12, 'j', 'P', ' ', ' ', 13, 10, 135, 10}, {255, 79, 255, 81}} {
		if got := format.DetectFormat("no-extension", header); got != format.FormatJPEG2000 {
			t.Fatal(got)
		}
	}
	if format.DetectFormat("ordinary.jpg", nil) == format.FormatJPEG2000 {
		t.Fatal("JPEG conflated with JPEG2000")
	}
	d := (&Plugin{}).Descriptor()
	if d.DataType != datatype.Media {
		t.Fatal(d)
	}
	if _, err := format.GetRuntimeMediaInfoProviderFactory(format.FormatJPEG2000); err != nil {
		t.Fatal(err)
	}
}
