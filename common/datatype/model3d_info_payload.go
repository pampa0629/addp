package datatype

import (
	"strings"

	commonJSON "github.com/addp/common/jsonmap"
)

// Model3DInfoFromPayload restores common 3D model facts from a JSON payload.
func Model3DInfoFromPayload(payload map[string]interface{}) *Model3DInfo {
	if len(payload) == 0 {
		return nil
	}
	var info Model3DInfo
	if err := commonJSON.DecodeStruct(payload, &info); err != nil {
		return nil
	}
	info.ModelKind = strings.TrimSpace(info.ModelKind)
	info.Unit = strings.TrimSpace(info.Unit)
	info.UpAxis = strings.TrimSpace(info.UpAxis)
	normalizeNonNegativeInt64Ptr(&info.NodeCount)
	normalizeNonNegativeInt64Ptr(&info.MeshCount)
	normalizeNonNegativeInt64Ptr(&info.VertexCount)
	normalizeNonNegativeInt64Ptr(&info.TriangleCount)
	normalizeNonNegativeInt64Ptr(&info.MaterialCount)
	normalizeNonNegativeInt64Ptr(&info.TextureCount)
	normalizeNonNegativeInt64Ptr(&info.AnimationCount)
	normalizeNonNegativeInt64Ptr(&info.LODCount)
	normalizeNonNegativeInt64Ptr(&info.SizeBytes)
	info.Bounds3D = NormalizeBounds3D(info.Bounds3D)
	if emptyModel3DInfo(&info) {
		return nil
	}
	return &info
}

// Model3DInfoPayload converts common 3D model facts to a JSON payload.
func Model3DInfoPayload(info *Model3DInfo) map[string]interface{} {
	normalized := NormalizeModel3DInfo(info)
	return commonJSON.MapFromStruct(normalized)
}

// NormalizeModel3DInfo returns a normalized copy of 3D model facts.
func NormalizeModel3DInfo(info *Model3DInfo) *Model3DInfo {
	if info == nil {
		return nil
	}
	payload := commonJSON.MapFromStruct(info)
	return Model3DInfoFromPayload(payload)
}

// NormalizeBounds3D returns a normalized copy of a 3D bounds value.
func NormalizeBounds3D(bounds *Bounds3D) *Bounds3D {
	if bounds == nil {
		return nil
	}
	if bounds.MinX == nil && bounds.MinY == nil && bounds.MinZ == nil &&
		bounds.MaxX == nil && bounds.MaxY == nil && bounds.MaxZ == nil {
		return nil
	}
	return bounds.Clone()
}

func emptyModel3DInfo(info *Model3DInfo) bool {
	return info.ModelKind == "" &&
		info.NodeCount == nil &&
		info.MeshCount == nil &&
		info.VertexCount == nil &&
		info.TriangleCount == nil &&
		info.MaterialCount == nil &&
		info.TextureCount == nil &&
		info.AnimationCount == nil &&
		info.LODCount == nil &&
		info.Bounds3D == nil &&
		info.Unit == "" &&
		info.UpAxis == "" &&
		info.SizeBytes == nil
}

func normalizeNonNegativeInt64Ptr(value **int64) {
	if value == nil || *value == nil {
		return
	}
	if **value < 0 {
		*value = nil
	}
}
