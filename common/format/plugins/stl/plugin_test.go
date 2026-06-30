package stl

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestSTLDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatSTL {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatSTL)
	}
	if descriptor.DataType != datatype.Model3D {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.Model3D)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("Layouts = %#v, want single", descriptor.Layouts)
	}
}

func TestDescribeModel3DReadsASCIISTL(t *testing.T) {
	content := []byte(`solid sample
facet normal 0 0 1
 outer loop
  vertex 0 0 0
  vertex 1 0 0
  vertex 0 1 0
 endloop
endfacet
endsolid sample`)
	result, err := NewPlugin().DescribeModel3D(context.Background(), bytes.NewReader(content), nil)
	if err != nil {
		t.Fatalf("DescribeModel3D() error = %v", err)
	}
	assertSTLSummary(t, result, "ascii", 3, 1)
}

func TestDescribeModel3DReadsBinarySTL(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(make([]byte, 80))
	if err := binary.Write(&buf, binary.LittleEndian, uint32(1)); err != nil {
		t.Fatalf("write triangle count: %v", err)
	}
	writeVec3(t, &buf, 0, 0, 1)
	writeVec3(t, &buf, 0, 0, 0)
	writeVec3(t, &buf, 2, 0, 0)
	writeVec3(t, &buf, 0, 3, 0)
	if err := binary.Write(&buf, binary.LittleEndian, uint16(0)); err != nil {
		t.Fatalf("write attr bytes: %v", err)
	}

	result, err := NewPlugin().DescribeModel3D(context.Background(), bytes.NewReader(buf.Bytes()), nil)
	if err != nil {
		t.Fatalf("DescribeModel3D() error = %v", err)
	}
	assertSTLSummary(t, result, "binary", 3, 1)
	if result.Model3D.Bounds3D == nil || result.Model3D.Bounds3D.MaxY == nil || *result.Model3D.Bounds3D.MaxY != 3 {
		t.Fatalf("Bounds3D = %#v, want max_y 3", result.Model3D.Bounds3D)
	}
}

func assertSTLSummary(t *testing.T, result *format.Model3DDescribeResult, encoding string, vertices int64, triangles int64) {
	t.Helper()
	if result == nil || result.Model3D == nil {
		t.Fatalf("DescribeModel3D() = %#v, want model info", result)
	}
	if result.Model3D.ModelKind != datatype.Model3DKindMeshScene {
		t.Fatalf("ModelKind = %q, want mesh_scene", result.Model3D.ModelKind)
	}
	if result.Model3D.VertexCount == nil || *result.Model3D.VertexCount != vertices {
		t.Fatalf("VertexCount = %v, want %d", result.Model3D.VertexCount, vertices)
	}
	if result.Model3D.TriangleCount == nil || *result.Model3D.TriangleCount != triangles {
		t.Fatalf("TriangleCount = %v, want %d", result.Model3D.TriangleCount, triangles)
	}
	if result.FormatInfo["encoding"] != encoding {
		t.Fatalf("format_info = %#v, want encoding %s", result.FormatInfo, encoding)
	}
	if result.FormatInfo["scan_complete"] != true {
		t.Fatalf("scan_complete = %#v, want true", result.FormatInfo["scan_complete"])
	}
}

func writeVec3(t *testing.T, buf *bytes.Buffer, x, y, z float32) {
	t.Helper()
	for _, value := range []float32{x, y, z} {
		if err := binary.Write(buf, binary.LittleEndian, math.Float32bits(value)); err != nil {
			t.Fatalf("write vec3: %v", err)
		}
	}
}
