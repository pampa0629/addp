// Package extractors 提取器共享辅助函数
package extractors

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// truncateRunes 截断文本到指定的字符数（按 rune 计算，支持多字节字符）
func truncateRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// inferColumnType 根据示例值推断列类型
func inferColumnType(values []string) string {
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
		if isBool(value) {
			boolCount++
			continue
		}
		if isInteger(value) {
			intCount++
			continue
		}
		if isFloat(value) {
			floatCount++
			continue
		}
		if isDate(value) {
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

// isNumeric 检查字符串是否为数字
func isNumeric(value string) bool {
	if value == "" {
		return false
	}
	return isInteger(value) || isFloat(value)
}

// isInteger 检查字符串是否为整数
func isInteger(value string) bool {
	_, err := strconv.ParseInt(value, 10, 64)
	return err == nil
}

var floatPattern = regexp.MustCompile(`^[+-]?(\d+\.\d+|\.\d+|\d+\.)$`)

// isFloat 检查字符串是否为浮点数
func isFloat(value string) bool {
	if floatPattern.MatchString(value) {
		return true
	}
	_, err := strconv.ParseFloat(value, 64)
	return err == nil
}

// isBool 检查字符串是否为布尔值
func isBool(value string) bool {
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

// isDate 检查字符串是否为日期
func isDate(value string) bool {
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

// maxInt 返回两个整数中的较大值
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// formatFileSize 将字节数转换为人类可读格式
func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return strconv.FormatInt(size, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	value := float64(size) / float64(div)
	suffix := "KMGTPE"
	if exp >= len(suffix) {
		exp = len(suffix) - 1
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f %cB", value, suffix[exp]), "0"), ".")
}
