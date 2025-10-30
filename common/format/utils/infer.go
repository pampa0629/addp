// Package utils 提供跨格式的通用工具函数
package utils

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// InferColumnType 根据示例值推断列类型
// 返回类型字符串："string", "integer", "number", "boolean", "date"
func InferColumnType(values []string) string {
	if len(values) == 0 {
		return "string"
	}

	var intCount, floatCount, boolCount, dateCount, nullCount int

	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || strings.EqualFold(value, "null") || strings.EqualFold(value, "na") {
			nullCount++
			continue
		}
		if IsBool(value) {
			boolCount++
			continue
		}
		if IsInteger(value) {
			intCount++
			continue
		}
		if IsFloat(value) {
			floatCount++
			continue
		}
		if IsDate(value) {
			dateCount++
			continue
		}
	}

	nonNull := len(values) - nullCount
	if nonNull <= 0 {
		return "string"
	}

	if boolCount == nonNull {
		return "boolean"
	}
	if dateCount >= nonNull/2 && dateCount > 0 {
		return "date"
	}
	if floatCount+intCount == nonNull {
		if floatCount > 0 {
			return "number"
		}
		return "integer"
	}

	return "string"
}

// IsNumeric 检查字符串是否为数字（整数或浮点数）
func IsNumeric(value string) bool {
	if value == "" {
		return false
	}
	return IsInteger(value) || IsFloat(value)
}

// IsInteger 检查字符串是否为整数
func IsInteger(value string) bool {
	_, err := strconv.ParseInt(value, 10, 64)
	return err == nil
}

var floatPattern = regexp.MustCompile(`^[+-]?(\d+\.\d+|\.\d+|\d+\.)$`)

// IsFloat 检查字符串是否为浮点数
func IsFloat(value string) bool {
	if floatPattern.MatchString(value) {
		return true
	}
	_, err := strconv.ParseFloat(value, 64)
	return err == nil
}

// IsBool 检查字符串是否为布尔值
func IsBool(value string) bool {
	switch strings.ToLower(value) {
	case "true", "false", "yes", "no", "y", "n", "1", "0":
		return true
	default:
		return false
	}
}

var dateLayouts = []string{
	time.RFC3339,
	"2006-01-02",
	"2006/01/02",
	"2006-1-2",
	"02-01-2006",
	"02/01/2006",
	"2006-01-02 15:04:05",
	"2006/01/02 15:04:05",
	"01/02/2006",
	"1/2/2006",
	"02-Jan-2006",
	"02-Jan-06",
}

// IsDate 检查字符串是否为日期
func IsDate(value string) bool {
	if value == "" {
		return false
	}
	for _, layout := range dateLayouts {
		if _, err := time.Parse(layout, value); err == nil {
			return true
		}
	}
	return false
}

// MaxInt 返回两个整数中的较大值
func MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
