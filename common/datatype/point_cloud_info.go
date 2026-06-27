package datatype

const (
	PointCloudKindGeneric         = "generic"
	PointCloudKindRawPointCloud   = "raw_point_cloud"
	PointCloudKindTiledPointCloud = "tiled_point_cloud"
	PointCloudKindScanCollection  = "scan_collection"
)

// PointCloudInfo is the common type info for point cloud data items.
type PointCloudInfo struct {
	PointCloudKind    string    `json:"point_cloud_kind,omitempty"`
	PointCount        *int64    `json:"point_count,omitempty"`
	PointFormat       string    `json:"point_format,omitempty"`
	DimensionCount    *int      `json:"dimension_count,omitempty"`
	Dimensions        []string  `json:"dimensions,omitempty"`
	Bounds3D          *Bounds3D `json:"bounds_3d,omitempty"`
	Scale             []float64 `json:"scale,omitempty"`
	Offset            []float64 `json:"offset,omitempty"`
	HasColor          *bool     `json:"has_color,omitempty"`
	HasIntensity      *bool     `json:"has_intensity,omitempty"`
	HasClassification *bool     `json:"has_classification,omitempty"`
	SizeBytes         *int64    `json:"size_bytes,omitempty"`
}

// Clone returns a deep copy of PointCloudInfo.
func (p *PointCloudInfo) Clone() *PointCloudInfo {
	if p == nil {
		return nil
	}
	cloned := *p
	cloned.PointCount = cloneInt64Ptr(p.PointCount)
	cloned.DimensionCount = cloneIntPtr(p.DimensionCount)
	cloned.Dimensions = append([]string(nil), p.Dimensions...)
	cloned.Bounds3D = p.Bounds3D.Clone()
	cloned.Scale = append([]float64(nil), p.Scale...)
	cloned.Offset = append([]float64(nil), p.Offset...)
	cloned.HasColor = cloneBoolPtr(p.HasColor)
	cloned.HasIntensity = cloneBoolPtr(p.HasIntensity)
	cloned.HasClassification = cloneBoolPtr(p.HasClassification)
	cloned.SizeBytes = cloneInt64Ptr(p.SizeBytes)
	return &cloned
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
