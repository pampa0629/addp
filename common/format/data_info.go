package format

import "github.com/addp/common/datatype"

// TableInfo 是 format reader / writer / Transfer 使用的表操作 schema。
//
// 通用 table 类型事实源是 datatype.TableInfo；TableInfo 只在需要字段顺序、
// 写出 schema、采样上下文或格式操作补充信息的边界使用。
type TableInfo struct {
	datatype.TableInfo

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
	base := t.TableInfo.Clone()
	return &TableInfo{
		TableInfo:    *base,
		FormatInfo:   cloneInterfaceMap(t.FormatInfo),
		SpatialInfo:  t.SpatialInfo.Clone(),
		ContentIndex: t.ContentIndex.Clone(),
	}
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
