package s3m

import (
	"bytes"
	"context"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestS3MDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatS3M || descriptor.DataType != datatype.Model3D {
		t.Fatalf("descriptor = %#v, want model_3d/s3m", descriptor)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutWhole) {
		t.Fatalf("layouts = %#v, want whole", descriptor.Layouts)
	}
	if len(descriptor.Identification.RelativePaths) != 1 || descriptor.Identification.RelativePaths[0] != "config/"+ManifestFileName {
		t.Fatalf("relative paths = %#v, want canonical config/scene.scp", descriptor.Identification.RelativePaths)
	}
}

func TestDescribeLegacyXMLSCP(t *testing.T) {
	input := `<?xml version="1.0"?><SuperMapCache xmlns:sml="http://www.supermap.com/SuperMapCache/vectortile"><sml:Version>1.0</sml:Version><sml:FileType>OSGBFile</sml:FileType><sml:Position><sml:X>118.5</sml:X><sml:Y>44.2</sml:Y><sml:Z>0</sml:Z></sml:Position><sml:OSGFiles><sml:Files><sml:FileName>Tile_1/Tile_1.osgb</sml:FileName></sml:Files></sml:OSGFiles></SuperMapCache>`
	result, err := NewPlugin().DescribeModel3D(context.Background(), bytes.NewBufferString(input), nil)
	if err != nil {
		t.Fatalf("DescribeModel3D() error = %v", err)
	}
	if result.Model3D.ModelKind != datatype.Model3DKindTiledScene {
		t.Fatalf("model kind = %q", result.Model3D.ModelKind)
	}
	if result.FormatInfo["manifest_encoding"] != "xml" || result.FormatInfo["tile_extension"] != ".s3m" || result.FormatInfo["root_tile_count"] != int64(1) {
		t.Fatalf("format info = %#v", result.FormatInfo)
	}
}

func TestDescribeJSONSCP(t *testing.T) {
	input := `{"asset":"SuperMap","version":3.0,"extensions":{"s3m:FileType":"OSGBCacheFile"},"position":{"x":118.5,"y":44.2,"z":0},"tiles":[{"url":"Tile_1.s3mb"}]}`
	result, err := NewPlugin().DescribeModel3D(context.Background(), bytes.NewBufferString(input), nil)
	if err != nil {
		t.Fatalf("DescribeModel3D() error = %v", err)
	}
	if result.FormatInfo["manifest_encoding"] != "json" || result.FormatInfo["tile_extension"] != ".s3mb" {
		t.Fatalf("format info = %#v", result.FormatInfo)
	}
}
