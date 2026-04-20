package plugin

import (
	"fmt"
	"os"
	"strings"
)

// NormalizeHost 规范化主机地址
// 在 Docker 环境中，将 localhost 转换为容器可访问的地址
func NormalizeHost(host string) string {
	if host == "localhost" || host == "127.0.0.1" || host == "host.docker.internal" {
		if alias := os.Getenv("RESOURCE_LOCALHOST_ALIAS"); alias != "" {
			return alias
		}
		return "127.0.0.1"
	}
	return host
}

// GetString 从 ConnectionInfo 中获取字符串值
// 支持 string、int、float64 类型的自动转换
func GetString(connInfo ConnectionInfo, key string) string {
	if v, ok := connInfo[key]; ok {
		switch val := v.(type) {
		case string:
			return val
		case float64:
			// 对于端口字段，必须转为整数格式（避免 "9030.0"）
			if key == "port" {
				return fmt.Sprintf("%d", int(val))
			}
			return fmt.Sprintf("%.0f", val)
		case int:
			return fmt.Sprintf("%d", val)
		default:
			return fmt.Sprintf("%v", val)
		}
	}
	return ""
}

// GetInt 从 ConnectionInfo 中获取整数值
func GetInt(connInfo ConnectionInfo, key string) int {
	if v, ok := connInfo[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case float64:
			return int(val)
		case string:
			// 尝试解析字符串
			var result int
			fmt.Sscanf(val, "%d", &result)
			return result
		}
	}
	return 0
}

// GetBool 从 ConnectionInfo 中获取布尔值
func GetBool(connInfo ConnectionInfo, key string) bool {
	if v, ok := connInfo[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// ValidateRequiredFields 验证必填字段是否存在
func ValidateRequiredFields(connInfo ConnectionInfo, requiredFields []string) error {
	var missing []string

	for _, field := range requiredFields {
		value := GetString(connInfo, field)
		if value == "" {
			missing = append(missing, field)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required fields: %s", strings.Join(missing, ", "))
	}

	return nil
}

// Contains 检查字符串切片中是否包含指定字符串
func Contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// MaskSensitiveValue 脱敏敏感值
// 如果值长度 <= 4，返回 "****"
// 否则返回 "前4字符****后4字符"
func MaskSensitiveValue(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return "****"
	}
	return value[:4] + "****" + value[len(value)-4:]
}
