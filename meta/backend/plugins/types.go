package plugins

import (
	"github.com/addp/common/format"
	"github.com/addp/meta/plugins/scanners"
)

// 重新导出 common/format 包中的类型，方便 meta 模块内部使用
// 这些类型别名保持向后兼容，避免大范围修改现有代码
type (
	// Scanner 相关类型
	Scanner              = format.Scanner
	SchemaInfo           = format.SchemaInfo
	TableInfo            = format.TableInfo
	FieldInfo            = format.FieldInfo
	ObjectNode           = format.ObjectNode
	ObjectMetadata       = format.ObjectMetadata
	ObjectStorageScanner = format.ObjectStorageScanner

	// Extractor 相关类型
	MetadataExtractor = format.FileMetadataExtractor
	ExtractInput      = format.ExtractInput
	Metadata          = format.ExtractedMetadata
	BasicMetadata     = format.BasicMetadata
	SchemaMetadata    = format.SchemaMetadata
	ColumnInfo        = format.ColumnMetadata
)

// NewScanner 创建扫描器的便捷函数
// 重新导出 scanners.NewScanner，方便调用
func NewScanner(dbType, connStr string) (Scanner, error) {
	return scanners.NewScanner(dbType, connStr)
}

// GetExtractor 获取元数据提取器的便捷函数
// 重新导出 format.GetExtractor
func GetExtractor(contentType string) MetadataExtractor {
	return format.GetExtractor(contentType)
}
