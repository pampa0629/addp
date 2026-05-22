package format

import (
	"time"

	"github.com/addp/common/datatype"
)

// TableInfo 是 format reader / writer / Transfer 使用的表操作 schema。
//
// 通用 table 类型事实源是 datatype.TableInfo；TableInfo 只在需要字段顺序、
// 写出 schema、采样上下文或格式操作补充信息的边界使用。
type TableInfo struct {
	// 基础信息
	Name      string     // 表名 / 文件名
	RowCount  *int64     // 记录数（可能未知）
	SizeBytes *int64     // 大小（字节）
	CreatedAt *time.Time // 创建时间
	UpdatedAt *time.Time // 更新时间

	// 字段定义
	Fields     []datatype.FieldInfo // 字段列表
	PrimaryKey []string             // 主键字段名列表

	// 可选补充事实。FormatInfo 写入 attributes.format_info.<format>，
	// SpatialInfo 写入 capabilities.spatial，ContentIndex 写入 content_index.table。
	FormatInfo   map[string]interface{}
	SpatialInfo  *datatype.SpatialInfo
	ContentIndex *datatype.ContentIndex
}

// GetSpatialInfo 获取空间扩展信息（便捷方法）
func (t *TableInfo) GetSpatialInfo() *datatype.SpatialInfo {
	if t == nil {
		return nil
	}
	return t.SpatialInfo
}

// IsSpatial 判断是否为空间数据
func (t *TableInfo) IsSpatial() bool {
	spatialInfo := t.GetSpatialInfo()
	return spatialInfo != nil && spatialInfo.IsSpatial()
}

// Clone 返回 TableInfo 的深拷贝，供 reader / writer / transfer 操作 schema 复用。
func (t *TableInfo) Clone() *TableInfo {
	if t == nil {
		return nil
	}
	cloned := *t
	cloned.Fields = append([]datatype.FieldInfo(nil), t.Fields...)
	cloned.PrimaryKey = append([]string(nil), t.PrimaryKey...)
	cloned.FormatInfo = cloneInterfaceMap(t.FormatInfo)
	cloned.SpatialInfo = t.SpatialInfo.Clone()
	cloned.ContentIndex = t.ContentIndex.Clone()
	if t.RowCount != nil {
		rowCount := *t.RowCount
		cloned.RowCount = &rowCount
	}
	if t.SizeBytes != nil {
		sizeBytes := *t.SizeBytes
		cloned.SizeBytes = &sizeBytes
	}
	if t.CreatedAt != nil {
		createdAt := *t.CreatedAt
		cloned.CreatedAt = &createdAt
	}
	if t.UpdatedAt != nil {
		updatedAt := *t.UpdatedAt
		cloned.UpdatedAt = &updatedAt
	}
	return &cloned
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
func (t *TableInfo) GetField(name string) *datatype.FieldInfo {
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
