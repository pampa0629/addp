package scantask

import (
	"strings"

	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
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

func JSONMapBool(m models.JSONMap, key string, defaultVal bool) bool {
	if m == nil {
		return defaultVal
	}
	v, ok := m[key]
	if !ok {
		return defaultVal
	}
	return BoolFromInterfaceWithDefault(v, defaultVal)
}

func BoolFromInterface(raw interface{}) bool {
	return BoolFromInterfaceWithDefault(raw, false)
}

func BoolFromInterfaceWithDefault(raw interface{}, defaultVal bool) bool {
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "y":
			return true
		case "false", "0", "no", "n":
			return false
		}
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	}
	return defaultVal
}

// StringSliceFromInterface 从 interface{} 中提取字符串数组。
func StringSliceFromInterface(raw interface{}) []string {
	return scanflow.StringSliceFromInterface(raw)
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
