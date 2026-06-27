package datatype

const (
	Model3DKindGeneric             = "generic"
	Model3DKindMeshScene           = "mesh_scene"
	Model3DKindPhotogrammetryScene = "photogrammetry_scene"
	Model3DKindBIMModel            = "bim_model"
	Model3DKindTiledScene          = "tiled_scene"
)

// Bounds3D describes an axis-aligned 3D bounding box in the item's native
// coordinate space.
type Bounds3D struct {
	MinX *float64 `json:"min_x,omitempty"`
	MinY *float64 `json:"min_y,omitempty"`
	MinZ *float64 `json:"min_z,omitempty"`
	MaxX *float64 `json:"max_x,omitempty"`
	MaxY *float64 `json:"max_y,omitempty"`
	MaxZ *float64 `json:"max_z,omitempty"`
}

// Model3DInfo is the common type info for 3D model data items.
type Model3DInfo struct {
	ModelKind      string    `json:"model_kind,omitempty"`
	NodeCount      *int64    `json:"node_count,omitempty"`
	MeshCount      *int64    `json:"mesh_count,omitempty"`
	VertexCount    *int64    `json:"vertex_count,omitempty"`
	TriangleCount  *int64    `json:"triangle_count,omitempty"`
	MaterialCount  *int64    `json:"material_count,omitempty"`
	TextureCount   *int64    `json:"texture_count,omitempty"`
	AnimationCount *int64    `json:"animation_count,omitempty"`
	LODCount       *int64    `json:"lod_count,omitempty"`
	Bounds3D       *Bounds3D `json:"bounds_3d,omitempty"`
	Unit           string    `json:"unit,omitempty"`
	UpAxis         string    `json:"up_axis,omitempty"`
	SizeBytes      *int64    `json:"size_bytes,omitempty"`
}

// Clone returns a deep copy of Model3DInfo.
func (m *Model3DInfo) Clone() *Model3DInfo {
	if m == nil {
		return nil
	}
	cloned := *m
	cloned.NodeCount = cloneInt64Ptr(m.NodeCount)
	cloned.MeshCount = cloneInt64Ptr(m.MeshCount)
	cloned.VertexCount = cloneInt64Ptr(m.VertexCount)
	cloned.TriangleCount = cloneInt64Ptr(m.TriangleCount)
	cloned.MaterialCount = cloneInt64Ptr(m.MaterialCount)
	cloned.TextureCount = cloneInt64Ptr(m.TextureCount)
	cloned.AnimationCount = cloneInt64Ptr(m.AnimationCount)
	cloned.LODCount = cloneInt64Ptr(m.LODCount)
	cloned.SizeBytes = cloneInt64Ptr(m.SizeBytes)
	cloned.Bounds3D = m.Bounds3D.Clone()
	return &cloned
}

// Clone returns a deep copy of Bounds3D.
func (b *Bounds3D) Clone() *Bounds3D {
	if b == nil {
		return nil
	}
	cloned := *b
	cloned.MinX = cloneFloat64Ptr(b.MinX)
	cloned.MinY = cloneFloat64Ptr(b.MinY)
	cloned.MinZ = cloneFloat64Ptr(b.MinZ)
	cloned.MaxX = cloneFloat64Ptr(b.MaxX)
	cloned.MaxY = cloneFloat64Ptr(b.MaxY)
	cloned.MaxZ = cloneFloat64Ptr(b.MaxZ)
	return &cloned
}

func cloneInt64Ptr(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneFloat64Ptr(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
