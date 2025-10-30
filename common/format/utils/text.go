// Package utils 提供跨格式的通用工具函数
package utils

import (
	"fmt"
	"strconv"
	"strings"
)

// TruncateRunes 截断文本到指定的字符数（按 rune 计算，支持多字节字符）
// 这是一个 UTF-8 安全的截断函数，不会在多字节字符中间截断
func TruncateRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// FormatFileSize 将字节数转换为人类可读格式
// 例如：1024 -> "1.0 KB", 1048576 -> "1.0 MB"
func FormatFileSize(size int64) string {
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
