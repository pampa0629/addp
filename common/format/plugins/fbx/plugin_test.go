package fbx

import (
	"bytes"
	"context"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestFBXDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatFBX {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatFBX)
	}
	if descriptor.DataType != datatype.Model3D {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.Model3D)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("Layouts = %#v, want single", descriptor.Layouts)
	}
}

func TestDescribeModel3DReadsBinaryFBXHeader(t *testing.T) {
	content := append([]byte{}, fbxBinaryMagic...)
	content = append(content, []byte{0x00, 0x00, 0x00, 0x00}...)
	result, err := NewPlugin().DescribeModel3D(context.Background(), bytes.NewReader(content), nil)
	if err != nil {
		t.Fatalf("DescribeModel3D() error = %v", err)
	}
	if result == nil || result.Model3D == nil {
		t.Fatalf("DescribeModel3D() = %#v, want model info", result)
	}
	if result.Model3D.ModelKind != datatype.Model3DKindMeshScene {
		t.Fatalf("ModelKind = %q, want mesh_scene", result.Model3D.ModelKind)
	}
	if result.FormatInfo["encoding"] != "binary" {
		t.Fatalf("format_info = %#v, want binary encoding", result.FormatInfo)
	}
}

func TestSniffFormatRecognizesFBX(t *testing.T) {
	if !NewPlugin().SniffFormat(append([]byte{}, fbxBinaryMagic...)) {
		t.Fatal("SniffFormat(binary FBX) = false, want true")
	}
	if !NewPlugin().SniffFormat([]byte("; FBX 7.4.0 project file")) {
		t.Fatal("SniffFormat(ascii FBX) = false, want true")
	}
}
