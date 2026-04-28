package parquet

import (
	"path/filepath"
	"strings"
)

// IsLakeTableExt 判断文件扩展名（含点，如 ".parquet"）是否为湖表格式
func IsLakeTableExt(ext string) bool {
	switch ext {
	case ".parquet", ".orc", ".avro":
		return true
	}
	return false
}

// IsLakeTableFileType 判断文件类型字符串（如 "parquet"）是否为湖表格式
func IsLakeTableFileType(fileType string) bool {
	switch strings.ToLower(strings.TrimSpace(fileType)) {
	case "parquet", "orc", "avro":
		return true
	}
	return false
}

// LogicalLakeTableName 去掉湖表文件扩展名，返回逻辑表名
func LogicalLakeTableName(fileName string) string {
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
