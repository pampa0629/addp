package plugins

import (
	"github.com/addp/common/format"
)

// 重新导出 common/format 包中的类型，方便 meta 模块内部使用
// 这些类型别名保持向后兼容，避免大范围修改现有代码
type (
	// 已废弃：Scanner 相关类型（重构后不再使用）
	// 保留类型别名仅用于兼容性，实际使用 plugin.RelationalDBPlugin 和 plugin.ObjectStoragePlugin
	Scanner              = format.Scanner
	SchemaInfo           = format.SchemaInfo
	TableInfo            = format.TableInfo
	FieldInfo            = format.FieldInfo
	ObjectNode           = format.ObjectNode
	ObjectMetadata       = format.ObjectMetadata
	ObjectStorageScanner = format.ObjectStorageScanner

	// Extractor 相关类型（仍在使用）
	MetadataExtractor = format.FileMetadataExtractor
	ExtractInput      = format.ExtractInput
	Metadata          = format.ExtractedMetadata
	BasicMetadata     = format.BasicMetadata
	SchemaMetadata    = format.SchemaMetadata
	ColumnInfo        = format.ColumnMetadata
)

// GetExtractor 获取元数据提取器的便捷函数
// 重新导出 format.GetExtractor
func GetExtractor(contentType string) MetadataExtractor {
	return format.GetExtractor(contentType)
}
