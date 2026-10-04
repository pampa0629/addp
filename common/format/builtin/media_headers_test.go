package builtin

import (
	"reflect"
	"testing"

	"github.com/addp/common/format"
	imageplugin "github.com/addp/common/format/plugins/image"
)

func TestWebPAndBMPHaveOneImagePluginOwner(t *testing.T) {
	for _, tt := range []struct {
		kind              format.FormatType
		extensions, mimes []string
	}{
		{format.FormatWebP, []string{".webp"}, []string{"image/webp"}},
		{format.FormatBMP, []string{".bmp"}, []string{"image/bmp", "image/x-ms-bmp"}},
	} {
		t.Run(string(tt.kind), func(t *testing.T) {
			owner, err := format.GetFormatPlugin(tt.kind)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := owner.(*imageplugin.Plugin); !ok {
				t.Fatalf("unexpected format owner: %T", owner)
			}
			snapshot, ok := format.GetFormatCapabilitySnapshot(tt.kind)
			if !ok || !snapshot.Implementations.MediaInfoProvider {
				t.Fatalf("missing media provider: %#v", snapshot)
			}
			d := snapshot.Descriptor
			if d.ID != "builtin-"+string(tt.kind) || d.DataType != "media" ||
				!reflect.DeepEqual(d.Layouts, []string{format.LayoutSingle}) ||
				!reflect.DeepEqual(d.Identification.Extensions, tt.extensions) ||
				!reflect.DeepEqual(d.Identification.MimeTypes, tt.mimes) {
				t.Fatalf("format identity changed: %#v", d)
			}
			if _, ok := owner.(format.RelatedRefSpecProvider); ok {
				t.Fatalf("single image acquired sidecar ownership: %#v", snapshot)
			}
		})
	}
	for _, kind := range []format.FormatType{format.FormatSVG, format.FormatAVIF, format.FormatHEIC} {
		snapshot, ok := format.GetFormatCapabilitySnapshot(kind)
		if !ok || snapshot.Implementations.MediaInfoProvider {
			t.Fatalf("unimplemented media format %s acquired provider: %#v", kind, snapshot)
		}
	}
}
