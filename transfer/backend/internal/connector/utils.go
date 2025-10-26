package connector

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/addp/transfer/pkg/pipeline"
)

// mapToStruct 将 map 转换为结构体
func mapToStruct(m map[string]interface{}, v interface{}) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// getStringConfig 从配置中获取字符串值
func getStringConfig(config pipeline.ConnectorConfig, key string, defaultValue string) string {
	if val, ok := config.Config[key]; ok {
		if strVal, ok := val.(string); ok {
			return strVal
		}
	}
	return defaultValue
}

// getIntConfig 从配置中获取整数值
func getIntConfig(config pipeline.ConnectorConfig, key string, defaultValue int) int {
	if val, ok := config.Config[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		case string:
			var intVal int
			if _, err := fmt.Sscanf(v, "%d", &intVal); err == nil {
				return intVal
			}
		}
	}
	return defaultValue
}

// getBoolConfig 从配置中获取布尔值
func getBoolConfig(config pipeline.ConnectorConfig, key string, defaultValue bool) bool {
	if val, ok := config.Config[key]; ok {
		if boolVal, ok := val.(bool); ok {
			return boolVal
		}
	}
	return defaultValue
}

// getStringSliceConfig 从配置中获取字符串数组
func getStringSliceConfig(config pipeline.ConnectorConfig, key string) []string {
	if val, ok := config.Config[key]; ok {
		return toStringSlice(val)
	}
	return nil
}

func toStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return filterEmptyStrings(v)
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			switch val := item.(type) {
			case string:
				if trimmed := strings.TrimSpace(val); trimmed != "" {
					result = append(result, trimmed)
				}
			case fmt.Stringer:
				result = append(result, strings.TrimSpace(val.String()))
			default:
				str := strings.TrimSpace(fmt.Sprintf("%v", val))
				if str != "" {
					result = append(result, str)
				}
			}
		}
		return result
	case string:
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return []string{trimmed}
		}
		return nil
	default:
		return nil
	}
}

func filterEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, val := range values {
		if trimmed := strings.TrimSpace(val); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
