package gltf

import (
	"bytes"
	"context"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestGLTFDescriptor(t *testing.T) {
	descriptor := NewPlugin().Descriptor()
	if descriptor.Format != format.FormatGLTF {
		t.Fatalf("Format = %q, want %q", descriptor.Format, format.FormatGLTF)
	}
	if descriptor.DataType != datatype.Model3D {
		t.Fatalf("DataType = %q, want %q", descriptor.DataType, datatype.Model3D)
	}
	if !format.HasLayout(descriptor.Layouts, format.LayoutMulti) {
		t.Fatalf("Layouts = %#v, want multi", descriptor.Layouts)
	}
}

func TestDescribeModel3DReadsGLTFManifest(t *testing.T) {
	content := []byte(`{
		"asset":{"version":"2.0","generator":"unit-test"},
		"scene":0,
		"scenes":[{"nodes":[0]}],
		"nodes":[{"mesh":0}],
		"meshes":[{"primitives":[{"attributes":{"POSITION":0},"indices":1}]}],
		"materials":[{}],
		"textures":[{},{}],
		"images":[{"uri":"textures/baseColor.png"},{"uri":"data:image/png;base64,AAAA"}],
		"buffers":[{"uri":"buffers/geometry.bin","byteLength":256}],
		"animations":[{}],
		"accessors":[
			{"count":8,"type":"VEC3","min":[0,1,2],"max":[3,4,5]},
			{"count":12,"type":"SCALAR"}
		],
		"extensionsUsed":["KHR_texture_basisu"]
	}`)
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
	if result.Model3D.VertexCount == nil || *result.Model3D.VertexCount != 8 {
		t.Fatalf("VertexCount = %v, want 8", result.Model3D.VertexCount)
	}
	if result.Model3D.TriangleCount == nil || *result.Model3D.TriangleCount != 4 {
		t.Fatalf("TriangleCount = %v, want 4", result.Model3D.TriangleCount)
	}
	if result.Model3D.Bounds3D == nil || result.Model3D.Bounds3D.MaxZ == nil || *result.Model3D.Bounds3D.MaxZ != 5 {
		t.Fatalf("Bounds3D = %#v, want max_z 5", result.Model3D.Bounds3D)
	}
	if result.FormatInfo["gltf_version"] != "2.0" || result.FormatInfo["generator"] != "unit-test" {
		t.Fatalf("format_info = %#v, want glTF asset facts", result.FormatInfo)
	}
	if result.FormatInfo["external_resource_count"] != 2 {
		t.Fatalf("external_resource_count = %#v, want 2 local resources", result.FormatInfo["external_resource_count"])
	}
}
