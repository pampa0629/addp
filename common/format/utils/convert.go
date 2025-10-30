// Package utils 提供跨格式的通用工具函数
package utils

import (
	"fmt"
	"sort"
)

// ToString 将任意类型转换为字符串
// 支持常见类型的智能转换，包括数值类型、布尔值等
func ToString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case []byte:
		return string(v)
	case float64:
		// 去除尾随的 0 和小数点
		s := fmt.Sprintf("%f", v)
		s = trimTrailingZeros(s)
		return s
	case float32:
		s := fmt.Sprintf("%f", float64(v))
		s = trimTrailingZeros(s)
		return s
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case int32:
		return fmt.Sprintf("%d", v)
	case uint:
		return fmt.Sprintf("%d", v)
	case uint64:
		return fmt.Sprintf("%d", v)
	case uint32:
		return fmt.Sprintf("%d", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// trimTrailingZeros 去除浮点数字符串的尾随零和小数点
func trimTrailingZeros(s string) string {
	if len(s) == 0 {
		return s
	}
	// 先去除尾随的 0
	s = trimRight(s, "0")
	// 再去除尾随的小数点
	s = trimRight(s, ".")
	return s
}

// trimRight 去除字符串右侧的指定字符
func trimRight(s, cutset string) string {
	for len(s) > 0 && containsRune(cutset, rune(s[len(s)-1])) {
		s = s[:len(s)-1]
	}
	return s
}

// containsRune 检查字符串是否包含指定的 rune
func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

// ToStringSlice 将任意类型转换为字符串切片
func ToStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			result = append(result, ToString(item))
		}
		return result
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case nil:
		return nil
	default:
		return []string{ToString(v)}
	}
}

// CoerceSliceOfMap 将任意类型强制转换为 []map[string]interface{}
// 主要用于处理 JSON 反序列化后的数据结构
func CoerceSliceOfMap(value interface{}) []map[string]interface{} {
	switch v := value.(type) {
	case []map[string]interface{}:
		return v
	case []interface{}:
		result := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				result = append(result, m)
			}
		}
		return result
	default:
		return nil
	}
}

// MapKeys 提取 map 的所有 key 并排序
func MapKeys(m map[string]interface{}) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// AsInt 将任意类型转换为 int
func AsInt(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case uint:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	default:
		return 0
	}
}

// TrimStringSlice 去除字符串切片中每个元素两端的空格
func TrimStringSlice(values []string) []string {
	result := make([]string, len(values))
	for i, v := range values {
		result[i] = trimSpace(v)
	}
	return result
}

// trimSpace 去除字符串两端的空格
func trimSpace(s string) string {
	start := 0
	end := len(s)

	// 找到第一个非空格字符
	for start < end && isSpace(s[start]) {
		start++
	}

	// 找到最后一个非空格字符
	for end > start && isSpace(s[end-1]) {
		end--
	}

	return s[start:end]
}

// isSpace 检查字符是否为空格
func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
