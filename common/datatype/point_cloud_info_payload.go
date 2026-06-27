package datatype

import (
	"strings"

	commonJSON "github.com/addp/common/jsonmap"
)

// PointCloudInfoFromPayload restores common point cloud facts from a JSON payload.
func PointCloudInfoFromPayload(payload map[string]interface{}) *PointCloudInfo {
	if len(payload) == 0 {
		return nil
	}
	var info PointCloudInfo
	if err := commonJSON.DecodeStruct(payload, &info); err != nil {
		return nil
	}
	info.PointCloudKind = strings.TrimSpace(info.PointCloudKind)
	info.PointFormat = strings.TrimSpace(info.PointFormat)
	normalizeNonNegativeInt64Ptr(&info.PointCount)
	normalizeNonNegativeIntPtr(&info.DimensionCount)
	info.Dimensions = normalizeStringList(info.Dimensions)
	info.Bounds3D = NormalizeBounds3D(info.Bounds3D)
	info.Scale = normalizeFloat64List(info.Scale)
	info.Offset = normalizeFloat64List(info.Offset)
	normalizeNonNegativeInt64Ptr(&info.SizeBytes)
	if emptyPointCloudInfo(&info) {
		return nil
	}
	return &info
}

// PointCloudInfoPayload converts common point cloud facts to a JSON payload.
func PointCloudInfoPayload(info *PointCloudInfo) map[string]interface{} {
	normalized := NormalizePointCloudInfo(info)
	return commonJSON.MapFromStruct(normalized)
}

// NormalizePointCloudInfo returns a normalized copy of point cloud facts.
func NormalizePointCloudInfo(info *PointCloudInfo) *PointCloudInfo {
	if info == nil {
		return nil
	}
	payload := commonJSON.MapFromStruct(info)
	return PointCloudInfoFromPayload(payload)
}

func emptyPointCloudInfo(info *PointCloudInfo) bool {
	return info.PointCloudKind == "" &&
		info.PointCount == nil &&
		info.PointFormat == "" &&
		info.DimensionCount == nil &&
		len(info.Dimensions) == 0 &&
		info.Bounds3D == nil &&
		len(info.Scale) == 0 &&
		len(info.Offset) == 0 &&
		info.HasColor == nil &&
		info.HasIntensity == nil &&
		info.HasClassification == nil &&
		info.SizeBytes == nil
}

func normalizeNonNegativeIntPtr(value **int) {
	if value == nil || *value == nil {
		return
	}
	if **value < 0 {
		*value = nil
	}
}

func normalizeStringList(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		output = append(output, value)
	}
	return output
}

func normalizeFloat64List(input []float64) []float64 {
	if len(input) == 0 {
		return nil
	}
	output := make([]float64, 0, len(input))
	for _, value := range input {
		output = append(output, value)
	}
	return output
}
