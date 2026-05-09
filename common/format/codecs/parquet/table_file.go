package parquet

import (
	"path/filepath"
	"strings"
)

// IsTableFileExt 判断文件扩展名（含点，如 ".parquet"）是否为表格文件格式。
func IsTableFileExt(ext string) bool {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".parquet", ".orc", ".avro":
		return true
	default:
		return false
	}
}

// IsTableFileType 判断文件类型字符串（如 "parquet"）是否为表格文件格式。
func IsTableFileType(fileType string) bool {
	switch strings.ToLower(strings.TrimSpace(fileType)) {
	case "parquet", "orc", "avro":
		return true
	default:
		return false
	}
}

// LogicalTableName 去掉表格文件扩展名，返回逻辑表名。
func LogicalTableName(fileName string) string {
	ext := filepath.Ext(fileName)
	if ext == "" {
		return fileName
	}
	base := strings.TrimSuffix(fileName, ext)
	if strings.TrimSpace(base) == "" {
		return fileName
	}
	return base
}
