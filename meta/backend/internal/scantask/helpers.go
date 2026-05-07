package scantask

import (
	"strings"

	"github.com/addp/meta/internal/models"
)

func NormalizeStorageType(resourceType string) string {
	normalized := strings.ToLower(strings.TrimSpace(resourceType))
	if normalized == "" {
		return "unknown"
	}
	return strings.ReplaceAll(normalized, " ", "_")
}

// JSONMapStringSlice 从 JSONMap 中提取字符串数组。
func JSONMapStringSlice(m models.JSONMap, key string) []string {
	if m == nil {
		return nil
	}
	raw, ok := m[key]
	if !ok {
		return nil
	}
	return StringSliceFromInterface(raw)
}

// JSONMapString 从 JSONMap 中提取字符串，缺失时返回默认值。
func JSONMapString(m models.JSONMap, key, defaultVal string) string {
	if m == nil {
		return defaultVal
	}
	v, ok := m[key]
	if !ok {
		return defaultVal
	}
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return defaultVal
}

// StringSliceFromInterface 从 interface{} 中提取字符串数组。
func StringSliceFromInterface(raw interface{}) []string {
	if raw == nil {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		if values, ok := raw.([]string); ok {
			result := make([]string, len(values))
			copy(result, values)
			return result
		}
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

func CloneJSONMap(m models.JSONMap) models.JSONMap {
	if m == nil {
		return nil
	}
	clone := models.JSONMap{}
	for k, v := range m {
		clone[k] = v
	}
	return clone
}
