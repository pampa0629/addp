package datatype

import "testing"

func TestModel3DInfoPayloadNormalizesFacts(t *testing.T) {
	meshCount := int64(3)
	vertexCount := int64(-1)
	minX := 1.5
	info := Model3DInfoFromPayload(map[string]interface{}{
		"model_kind":   " mesh_scene ",
		"mesh_count":   meshCount,
		"vertex_count": vertexCount,
		"bounds_3d": map[string]interface{}{
			"min_x": minX,
		},
		"unit":    " meter ",
		"up_axis": " z ",
	})
	if info == nil {
		t.Fatal("Model3DInfoFromPayload() = nil")
	}
	if info.ModelKind != Model3DKindMeshScene {
		t.Fatalf("ModelKind = %q, want %q", info.ModelKind, Model3DKindMeshScene)
	}
	if info.MeshCount == nil || *info.MeshCount != meshCount {
		t.Fatalf("MeshCount = %v, want %d", info.MeshCount, meshCount)
	}
	if info.VertexCount != nil {
		t.Fatalf("VertexCount = %v, want nil for negative input", *info.VertexCount)
	}
	if info.Bounds3D == nil || info.Bounds3D.MinX == nil || *info.Bounds3D.MinX != minX {
		t.Fatalf("Bounds3D.MinX = %#v, want %f", info.Bounds3D, minX)
	}
	if info.Unit != "meter" || info.UpAxis != "z" {
		t.Fatalf("unit/up_axis = %q/%q, want meter/z", info.Unit, info.UpAxis)
	}
}

func TestPointCloudInfoPayloadNormalizesFacts(t *testing.T) {
	pointCount := int64(42)
	dimensionCount := -2
	hasColor := true
	info := PointCloudInfoFromPayload(map[string]interface{}{
		"point_cloud_kind": " raw_point_cloud ",
		"point_count":      pointCount,
		"point_format":     " las_1.4_point_format_7 ",
		"dimension_count":  dimensionCount,
		"dimensions":       []interface{}{" x ", "", "intensity"},
		"scale":            []interface{}{0.01, 0.01, 0.01},
		"has_color":        hasColor,
	})
	if info == nil {
		t.Fatal("PointCloudInfoFromPayload() = nil")
	}
	if info.PointCloudKind != PointCloudKindRawPointCloud {
		t.Fatalf("PointCloudKind = %q, want %q", info.PointCloudKind, PointCloudKindRawPointCloud)
	}
	if info.PointCount == nil || *info.PointCount != pointCount {
		t.Fatalf("PointCount = %v, want %d", info.PointCount, pointCount)
	}
	if info.DimensionCount != nil {
		t.Fatalf("DimensionCount = %v, want nil for negative input", *info.DimensionCount)
	}
	if len(info.Dimensions) != 2 || info.Dimensions[0] != "x" || info.Dimensions[1] != "intensity" {
		t.Fatalf("Dimensions = %#v, want normalized x/intensity", info.Dimensions)
	}
	if len(info.Scale) != 3 || info.Scale[0] != 0.01 {
		t.Fatalf("Scale = %#v, want three scale values", info.Scale)
	}
	if info.HasColor == nil || !*info.HasColor {
		t.Fatalf("HasColor = %v, want true", info.HasColor)
	}
}

func TestGaussianSplatInfoPayloadNormalizesFacts(t *testing.T) {
	splatCount := int64(128)
	shDegree := -1
	hasScale := true
	sampledBoundsSampleCount := int64(256)
	info := GaussianSplatInfoFromPayload(map[string]interface{}{
		"representation":              " 3d_gaussian_splatting ",
		"splat_count":                 splatCount,
		"has_scale":                   hasScale,
		"sh_degree":                   shDegree,
		"sampled_bounds_3d":           map[string]interface{}{"min_x": 1.0, "max_x": 2.0},
		"sampled_bounds_method":       " sampled_binary_vertices ",
		"sampled_bounds_sample_count": sampledBoundsSampleCount,
	})
	if info == nil {
		t.Fatal("GaussianSplatInfoFromPayload() = nil")
	}
	if info.Representation != GaussianSplatRepresentation3DGS {
		t.Fatalf("Representation = %q, want %q", info.Representation, GaussianSplatRepresentation3DGS)
	}
	if info.SplatCount == nil || *info.SplatCount != splatCount {
		t.Fatalf("SplatCount = %v, want %d", info.SplatCount, splatCount)
	}
	if info.SHDegree != nil {
		t.Fatalf("SHDegree = %v, want nil for negative input", *info.SHDegree)
	}
	if info.HasScale == nil || !*info.HasScale {
		t.Fatalf("HasScale = %v, want true", info.HasScale)
	}
	if info.SampledBounds3D == nil || info.SampledBounds3D.MinX == nil || *info.SampledBounds3D.MinX != 1 {
		t.Fatalf("SampledBounds3D = %#v, want min_x", info.SampledBounds3D)
	}
	if info.SampledBoundsMethod != "sampled_binary_vertices" {
		t.Fatalf("SampledBoundsMethod = %q, want sampled_binary_vertices", info.SampledBoundsMethod)
	}
	if info.SampledBoundsSampleCount == nil || *info.SampledBoundsSampleCount != sampledBoundsSampleCount {
		t.Fatalf("SampledBoundsSampleCount = %v, want %d", info.SampledBoundsSampleCount, sampledBoundsSampleCount)
	}
}
