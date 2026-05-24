package format

import "github.com/addp/common/datatype"

// TableInfo 是 format reader / writer / Transfer 使用的表操作 schema。
//
// 通用 table 类型事实源是 datatype.TableInfo；TableInfo 只在需要字段顺序、
// 写出 schema 或采样上下文边界使用。
type TableInfo struct {
	datatype.TableInfo
}

// Clone 返回 TableInfo 的深拷贝，供 reader / writer / transfer 操作 schema 复用。
func (t *TableInfo) Clone() *TableInfo {
	if t == nil {
		return nil
	}
	base := t.TableInfo.Clone()
	return &TableInfo{
		TableInfo: *base,
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
