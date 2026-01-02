package utils

import (
	"encoding/json"
	"strings"
)

// 敏感字段列表
var sensitiveFields = []string{
	"password", "passwd", "pwd",
	"token", "access_token", "refresh_token",
	"secret", "api_key", "private_key", "apikey",
	"credit_card", "ssn",
	"authorization",
}

// MaskSensitiveData 对敏感数据进行脱敏处理
// 输入：JSON 字符串
// 输出：脱敏后的 JSON 字符串
func MaskSensitiveData(jsonStr string) string {
	if jsonStr == "" {
		return ""
	}

	// 解析 JSON
	var data interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		// 如果解析失败，直接返回原字符串（可能不是 JSON）
		return jsonStr
	}

	// 递归脱敏
	masked := maskValue(data)

	// 转回 JSON 字符串
	result, err := json.Marshal(masked)
	if err != nil {
		return jsonStr
	}

	return string(result)
}

// maskValue 递归处理各种类型的值
func maskValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return maskMap(v)
	case []interface{}:
		return maskArray(v)
	default:
		return value
	}
}

// maskMap 处理 map 类型，对敏感字段进行脱敏
func maskMap(data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for key, value := range data {
		keyLower := strings.ToLower(key)

		// 检查是否为敏感字段
		isSensitive := false
		for _, sensitiveField := range sensitiveFields {
			if strings.Contains(keyLower, sensitiveField) {
				isSensitive = true
				break
			}
		}

		if isSensitive {
			// 完全脱敏
			result[key] = "***REDACTED***"
		} else {
			// 递归处理嵌套对象
			result[key] = maskValue(value)
		}
	}
	return result
}

// maskArray 处理数组类型
func maskArray(data []interface{}) []interface{} {
	result := make([]interface{}, len(data))
	for i, item := range data {
		result[i] = maskValue(item)
	}
	return result
}

// MaskString 对字符串进行部分脱敏
// 保留前 n 位和后 m 位，中间用 * 替代
func MaskString(str string, keepPrefix, keepSuffix int) string {
	length := len(str)
	if length <= keepPrefix+keepSuffix {
		return strings.Repeat("*", length)
	}

	prefix := str[:keepPrefix]
	suffix := str[length-keepSuffix:]
	masked := strings.Repeat("*", length-keepPrefix-keepSuffix)

	return prefix + masked + suffix
}
