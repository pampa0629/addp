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

func TestDescribeModel3DRejectsHeaderWithoutCompleteStructure(t *testing.T) {
	content := append([]byte{}, fbxBinaryMagic...)
	content = append(content, []byte{0x00, 0x00, 0x00, 0x00}...)
	result, err := NewPlugin().DescribeModel3D(context.Background(), bytes.NewReader(content), nil)
	if err == nil || result != nil {
		t.Fatalf("header invented a mesh: %#v, %v", result, err)
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
