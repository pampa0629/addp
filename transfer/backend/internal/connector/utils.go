package connector

import (
	"encoding/json"
	"fmt"

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
