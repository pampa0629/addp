package datatype

const (
	GaussianSplatRepresentation3DGS = "3d_gaussian_splatting"
)

// GaussianSplatInfo is the common type info for 3D Gaussian splatting data items.
type GaussianSplatInfo struct {
	Representation           string    `json:"representation,omitempty"`
	SplatCount               *int64    `json:"splat_count,omitempty"`
	HasOpacity               *bool     `json:"has_opacity,omitempty"`
	HasScale                 *bool     `json:"has_scale,omitempty"`
	HasRotation              *bool     `json:"has_rotation,omitempty"`
	HasSphericalHarmonics    *bool     `json:"has_spherical_harmonics,omitempty"`
	SHDegree                 *int      `json:"sh_degree,omitempty"`
	Bounds3D                 *Bounds3D `json:"bounds_3d,omitempty"`
	SampledBounds3D          *Bounds3D `json:"sampled_bounds_3d,omitempty"`
	SampledBoundsMethod      string    `json:"sampled_bounds_method,omitempty"`
	SampledBoundsSampleCount *int64    `json:"sampled_bounds_sample_count,omitempty"`
	SizeBytes                *int64    `json:"size_bytes,omitempty"`
}

// Clone returns a deep copy of GaussianSplatInfo.
func (g *GaussianSplatInfo) Clone() *GaussianSplatInfo {
	if g == nil {
		return nil
	}
	cloned := *g
	cloned.SplatCount = cloneInt64Ptr(g.SplatCount)
	cloned.HasOpacity = cloneBoolPtr(g.HasOpacity)
	cloned.HasScale = cloneBoolPtr(g.HasScale)
	cloned.HasRotation = cloneBoolPtr(g.HasRotation)
	cloned.HasSphericalHarmonics = cloneBoolPtr(g.HasSphericalHarmonics)
	cloned.SHDegree = cloneIntPtr(g.SHDegree)
	cloned.Bounds3D = g.Bounds3D.Clone()
	cloned.SampledBounds3D = g.SampledBounds3D.Clone()
	cloned.SampledBoundsSampleCount = cloneInt64Ptr(g.SampledBoundsSampleCount)
	cloned.SizeBytes = cloneInt64Ptr(g.SizeBytes)
	return &cloned
}
