package datatype

const CADDrawingKind2D = "2d"

type Bounds2D struct {
	MinX *float64 `json:"min_x,omitempty"`
	MinY *float64 `json:"min_y,omitempty"`
	MaxX *float64 `json:"max_x,omitempty"`
	MaxY *float64 `json:"max_y,omitempty"`
}

type CADInfo struct {
	DrawingKind          string    `json:"drawing_kind,omitempty"`
	Unit                 string    `json:"unit,omitempty"`
	EntityCount          *int64    `json:"entity_count,omitempty"`
	LayerCount           *int64    `json:"layer_count,omitempty"`
	LayoutCount          *int64    `json:"layout_count,omitempty"`
	BlockDefinitionCount *int64    `json:"block_definition_count,omitempty"`
	XRefCount            *int64    `json:"xref_count,omitempty"`
	HasModelSpace        *bool     `json:"has_model_space,omitempty"`
	HasPaperSpace        *bool     `json:"has_paper_space,omitempty"`
	Bounds2D             *Bounds2D `json:"bounds_2d,omitempty"`
	Bounds3D             *Bounds3D `json:"bounds_3d,omitempty"`
	SizeBytes            *int64    `json:"size_bytes,omitempty"`
}

func (c *CADInfo) Clone() *CADInfo {
	if c == nil {
		return nil
	}
	cloned := *c
	cloned.EntityCount = cloneInt64Ptr(c.EntityCount)
	cloned.LayerCount = cloneInt64Ptr(c.LayerCount)
	cloned.LayoutCount = cloneInt64Ptr(c.LayoutCount)
	cloned.BlockDefinitionCount = cloneInt64Ptr(c.BlockDefinitionCount)
	cloned.XRefCount = cloneInt64Ptr(c.XRefCount)
	cloned.HasModelSpace = cloneBoolPtr(c.HasModelSpace)
	cloned.HasPaperSpace = cloneBoolPtr(c.HasPaperSpace)
	cloned.Bounds2D = c.Bounds2D.Clone()
	cloned.Bounds3D = c.Bounds3D.Clone()
	cloned.SizeBytes = cloneInt64Ptr(c.SizeBytes)
	return &cloned
}

func (b *Bounds2D) Clone() *Bounds2D {
	if b == nil {
		return nil
	}
	cloned := *b
	cloned.MinX = cloneFloat64Ptr(b.MinX)
	cloned.MinY = cloneFloat64Ptr(b.MinY)
	cloned.MaxX = cloneFloat64Ptr(b.MaxX)
	cloned.MaxY = cloneFloat64Ptr(b.MaxY)
	return &cloned
}
