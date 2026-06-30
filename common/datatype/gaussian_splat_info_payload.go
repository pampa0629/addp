package datatype

import (
	"strings"

	commonJSON "github.com/addp/common/jsonmap"
)

// GaussianSplatInfoFromPayload restores common Gaussian splatting facts from a JSON payload.
func GaussianSplatInfoFromPayload(payload map[string]interface{}) *GaussianSplatInfo {
	if len(payload) == 0 {
		return nil
	}
	var info GaussianSplatInfo
	if err := commonJSON.DecodeStruct(payload, &info); err != nil {
		return nil
	}
	info.Representation = strings.TrimSpace(info.Representation)
	info.SampledBoundsMethod = strings.TrimSpace(info.SampledBoundsMethod)
	normalizeNonNegativeInt64Ptr(&info.SplatCount)
	normalizeNonNegativeIntPtr(&info.SHDegree)
	info.Bounds3D = NormalizeBounds3D(info.Bounds3D)
	info.SampledBounds3D = NormalizeBounds3D(info.SampledBounds3D)
	normalizeNonNegativeInt64Ptr(&info.SampledBoundsSampleCount)
	normalizeNonNegativeInt64Ptr(&info.SizeBytes)
	if emptyGaussianSplatInfo(&info) {
		return nil
	}
	return &info
}

// GaussianSplatInfoPayload converts common Gaussian splatting facts to a JSON payload.
func GaussianSplatInfoPayload(info *GaussianSplatInfo) map[string]interface{} {
	normalized := NormalizeGaussianSplatInfo(info)
	return commonJSON.MapFromStruct(normalized)
}

// NormalizeGaussianSplatInfo returns a normalized copy of Gaussian splatting facts.
func NormalizeGaussianSplatInfo(info *GaussianSplatInfo) *GaussianSplatInfo {
	if info == nil {
		return nil
	}
	payload := commonJSON.MapFromStruct(info)
	return GaussianSplatInfoFromPayload(payload)
}

func emptyGaussianSplatInfo(info *GaussianSplatInfo) bool {
	return info.Representation == "" &&
		info.SplatCount == nil &&
		info.HasOpacity == nil &&
		info.HasScale == nil &&
		info.HasRotation == nil &&
		info.HasSphericalHarmonics == nil &&
		info.SHDegree == nil &&
		info.Bounds3D == nil &&
		info.SampledBounds3D == nil &&
		info.SampledBoundsMethod == "" &&
		info.SampledBoundsSampleCount == nil &&
		info.SizeBytes == nil
}
