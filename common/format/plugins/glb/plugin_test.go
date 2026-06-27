package glb

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestGLBDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatGLB {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatGLB)
	}
	if descriptor.DataType != datatype.Model3D {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.Model3D)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutSingle) {
		t.Fatalf("Layouts = %#v, want single", descriptor.Layouts)
	}
}

func TestDescribeModel3DReadsGLBJSONChunk(t *testing.T) {
	jsonChunk := []byte(`{
		"asset":{"version":"2.0","generator":"unit-test"},
		"nodes":[{}],
		"meshes":[{"primitives":[{"attributes":{"POSITION":0},"indices":1}]}],
		"materials":[{}],
		"textures":[{}],
		"animations":[{}],
		"accessors":[
			{"count":4,"type":"VEC3","min":[1,2,3],"max":[4,5,6]},
			{"count":6,"type":"SCALAR"}
		],
		"extensionsUsed":["KHR_materials_unlit"]
	}`)
	result, err := NewPlugin().DescribeModel3D(context.Background(), bytes.NewReader(buildGLB(jsonChunk)), nil)
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
	if result.Model3D.Bounds3D == nil || result.Model3D.Bounds3D.MinX == nil || *result.Model3D.Bounds3D.MinX != 1 {
		t.Fatalf("Bounds3D = %#v, want min_x 1", result.Model3D.Bounds3D)
	}
	if result.FormatInfo["gltf_version"] != "2.0" {
		t.Fatalf("format_info.gltf_version = %#v, want 2.0", result.FormatInfo["gltf_version"])
	}
}

func buildGLB(jsonChunk []byte) []byte {
	for len(jsonChunk)%4 != 0 {
		jsonChunk = append(jsonChunk, ' ')
	}
	totalLen := uint32(12 + 8 + len(jsonChunk))
	buf := bytes.NewBuffer(make([]byte, 0, totalLen))
	buf.WriteString(glbMagic)
	_ = binary.Write(buf, binary.LittleEndian, uint32(2))
	_ = binary.Write(buf, binary.LittleEndian, totalLen)
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(jsonChunk)))
	_ = binary.Write(buf, binary.LittleEndian, uint32(glbJSONChunkType))
	buf.Write(jsonChunk)
	return buf.Bytes()
}
