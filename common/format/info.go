package format

import (
	"time"
)

// TableInfo 表信息（统一的表元数据结构）
type TableInfo struct {
	// 基础信息
	Name      string     // 表名 / 文件名
	RowCount  *int64     // 记录数（可能未知）
	SizeBytes *int64     // 大小（字节）
	CreatedAt *time.Time // 创建时间
	UpdatedAt *time.Time // 更新时间

	// 字段定义
	Fields     []FieldInfo // 字段列表
	PrimaryKey []string    // 主键字段名列表

	// 扩展信息（灵活扩展机制）
	Extensions []ExtensionInfo
}

// FieldInfo 字段信息
type FieldInfo struct {
	Name         string    // 字段名
	Type         FieldType // 统一的字段类型
	OriginalType string    // 原始类型（如 PostgreSQL 的 "int4", CSV 推断的 "integer"）
	Nullable     bool      // 是否允许 NULL
	IsPrimaryKey bool      // 是否主键
	Comment      string    // 字段注释
	Size         int       // 字符串长度或数值精度（0表示不限制）
	Precision    int       // 小数位数（仅用于 decimal/numeric 类型）

	// 用于文档数据库的动态 Schema
	OccurrenceRate float64 // 字段出现率（0.0-1.0），仅用于文档数据库采样推断
}

// GetExtension 获取指定类型的扩展信息
func (t *TableInfo) GetExtension(extensionType string) ExtensionInfo {
	for _, ext := range t.Extensions {
		if ext.ExtensionType() == extensionType {
			return ext
		}
	}
	return nil
}

// HasExtension 检查是否存在指定类型的扩展
func (t *TableInfo) HasExtension(extensionType string) bool {
	return t.GetExtension(extensionType) != nil
}

// GetSpatialInfo 获取空间扩展信息（便捷方法）
func (t *TableInfo) GetSpatialInfo() *SpatialInfo {
	ext := t.GetExtension("spatial")
	if ext == nil {
		return nil
	}
	if spatial, ok := ext.(*SpatialInfo); ok {
		return spatial
	}
	return nil
}

// GetCSVInfo 获取CSV扩展信息（便捷方法）
func (t *TableInfo) GetCSVInfo() *CSVInfo {
	ext := t.GetExtension("csv")
	if ext == nil {
		return nil
	}
	if csv, ok := ext.(*CSVInfo); ok {
		return csv
	}
	return nil
}

// GetDocCollectionInfo 获取文档集合扩展信息（便捷方法）
func (t *TableInfo) GetDocCollectionInfo() *DocCollectionInfo {
	ext := t.GetExtension("document_collection")
	if ext == nil {
		return nil
	}
	if doc, ok := ext.(*DocCollectionInfo); ok {
		return doc
	}
	return nil
}

// IsSpatial 判断是否为空间数据
func (t *TableInfo) IsSpatial() bool {
	return t.HasExtension("spatial")
}

// FieldNames 返回所有字段名
func (t *TableInfo) FieldNames() []string {
	names := make([]string, len(t.Fields))
	for i, field := range t.Fields {
		names[i] = field.Name
	}
	return names
}

// GetField 根据字段名查找字段定义
func (t *TableInfo) GetField(name string) *FieldInfo {
	for i := range t.Fields {
		if t.Fields[i].Name == name {
			return &t.Fields[i]
		}
	}
	return nil
}

// HasField 判断是否存在指定字段
func (t *TableInfo) HasField(name string) bool {
	return t.GetField(name) != nil
}
