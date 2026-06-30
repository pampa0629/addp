package obj

import (
	"bytes"
	"context"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestOBJDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatOBJ {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatOBJ)
	}
	if descriptor.DataType != datatype.Model3D {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.Model3D)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("Layouts = %#v, want single", descriptor.Layouts)
	}
}

func TestDescribeModel3DReadsOBJMeshSummary(t *testing.T) {
	content := []byte(`
mtllib material.mtl
o cube
v 0 0 0
v 1 0 0
v 1 1 0
v 0 1 0
usemtl white
f 1 2 3 4
`)
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
	if result.Model3D.VertexCount == nil || *result.Model3D.VertexCount != 4 {
		t.Fatalf("VertexCount = %v, want 4", result.Model3D.VertexCount)
	}
	if result.Model3D.TriangleCount == nil || *result.Model3D.TriangleCount != 2 {
		t.Fatalf("TriangleCount = %v, want 2", result.Model3D.TriangleCount)
	}
	if result.Model3D.Bounds3D == nil || result.Model3D.Bounds3D.MaxY == nil || *result.Model3D.Bounds3D.MaxY != 1 {
		t.Fatalf("Bounds3D = %#v, want max_y 1", result.Model3D.Bounds3D)
	}
	if result.FormatInfo["material_library_count"] != int64(1) || result.FormatInfo["uses_material"] != true {
		t.Fatalf("format_info = %#v, want material facts", result.FormatInfo)
	}
}

func TestDescribeModel3DReadsDeclaredOBJHeaderFacts(t *testing.T) {
	content := []byte(`# P3BJet 5.0.0.9 Wavefront OBJ format
# BoundingBox(620863.8114089966 4895390.731185913 1045.8348376727308 621015.5117721558 4895536.927829742 1154.097400030809)
# Vertices: 4127063
# Faces : 8237314

mtllib ROI_001.mtl
v 12.326071 -21.655987 1085.369995
f 1 2 3
`)
	result, err := NewPlugin().DescribeModel3D(context.Background(), bytes.NewReader(content), nil)
	if err != nil {
		t.Fatalf("DescribeModel3D() error = %v", err)
	}
	if result.Model3D.VertexCount == nil || *result.Model3D.VertexCount != 4127063 {
		t.Fatalf("VertexCount = %v, want declared count", result.Model3D.VertexCount)
	}
	if result.Model3D.TriangleCount == nil || *result.Model3D.TriangleCount != 8237314 {
		t.Fatalf("TriangleCount = %v, want declared face count as triangle summary", result.Model3D.TriangleCount)
	}
	if result.Model3D.Bounds3D == nil || result.Model3D.Bounds3D.MinX == nil || *result.Model3D.Bounds3D.MinX != 620863.8114089966 {
		t.Fatalf("Bounds3D = %#v, want declared bounding box", result.Model3D.Bounds3D)
	}
	if result.FormatInfo["declared_vertex_count"] != int64(4127063) || result.FormatInfo["declared_face_count"] != int64(8237314) {
		t.Fatalf("format_info = %#v, want declared counts", result.FormatInfo)
	}
	if result.FormatInfo["scan_complete"] != true {
		t.Fatalf("scan_complete = %#v, want true", result.FormatInfo["scan_complete"])
	}
}
