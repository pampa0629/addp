package datatype

import (
	commonJSON "github.com/addp/common/jsonmap"
	"strings"
)

func CADInfoFromPayload(payload map[string]interface{}) *CADInfo {
	if len(payload) == 0 {
		return nil
	}
	var info CADInfo
	if err := commonJSON.DecodeStruct(payload, &info); err != nil {
		return nil
	}
	info.DrawingKind = strings.TrimSpace(info.DrawingKind)
	info.Unit = strings.TrimSpace(info.Unit)
	normalizeNonNegativeInt64Ptr(&info.EntityCount)
	normalizeNonNegativeInt64Ptr(&info.LayerCount)
	normalizeNonNegativeInt64Ptr(&info.LayoutCount)
	normalizeNonNegativeInt64Ptr(&info.BlockDefinitionCount)
	normalizeNonNegativeInt64Ptr(&info.XRefCount)
	normalizeNonNegativeInt64Ptr(&info.SizeBytes)
	info.Bounds2D = NormalizeBounds2D(info.Bounds2D)
	info.Bounds3D = NormalizeBounds3D(info.Bounds3D)
	if emptyCADInfo(&info) {
		return nil
	}
	return &info
}

func CADInfoPayload(info *CADInfo) map[string]interface{} {
	return commonJSON.MapFromStruct(NormalizeCADInfo(info))
}

func NormalizeCADInfo(info *CADInfo) *CADInfo {
	if info == nil {
		return nil
	}
	return CADInfoFromPayload(commonJSON.MapFromStruct(info))
}

func NormalizeBounds2D(bounds *Bounds2D) *Bounds2D {
	if bounds == nil || (bounds.MinX == nil && bounds.MinY == nil && bounds.MaxX == nil && bounds.MaxY == nil) {
		return nil
	}
	return bounds.Clone()
}

func emptyCADInfo(info *CADInfo) bool {
	return info.DrawingKind == "" && info.Unit == "" && info.EntityCount == nil && info.LayerCount == nil &&
		info.LayoutCount == nil && info.BlockDefinitionCount == nil && info.XRefCount == nil &&
		info.HasModelSpace == nil && info.HasPaperSpace == nil && info.Bounds2D == nil &&
		info.Bounds3D == nil && info.SizeBytes == nil
}
