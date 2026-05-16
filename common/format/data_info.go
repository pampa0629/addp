package format

import (
	"fmt"
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

	// 可选补充事实。FormatInfo 写入 attributes.format_info.<format>，
	// SpatialInfo 写入 capabilities.spatial，ContentIndex 写入 content_index.table。
	FormatInfo   map[string]interface{}
	SpatialInfo  *SpatialInfo
	ContentIndex *ContentIndexInfo
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

// SpatialInfo 表示 table/media 等 data type 上的空间横切事实。
type SpatialInfo struct {
	GeometryColumn  string
	GeometryType    string
	SRID            int
	BoundingBox     *[4]float64
	HasSpatialIndex bool
	IndexName       string
	Dimension       int
}

func (s *SpatialInfo) IsSRIDWGS84() bool {
	return s.SRID == 4326
}

func (s *SpatialInfo) IsSRIDWebMercator() bool {
	return s.SRID == 3857
}

func (s *SpatialInfo) GetBoundingBoxString() string {
	if s.BoundingBox == nil {
		return ""
	}
	bbox := *s.BoundingBox
	return fmt.Sprintf("[%.6f, %.6f, %.6f, %.6f]", bbox[0], bbox[1], bbox[2], bbox[3])
}

const (
	ContentIndexKindSparseRow = "sparse_row_index"
	ContentIndexDataTypeTable = "table"
	ContentIndexUnitRow       = "row"
	ContentIndexOffsetByte    = "byte"
)

// ContentIndex 描述面向内容读取的通用访问索引。
//
// 索引本身不是 TableInfo 的核心语义，也不是格式私有元数据；
// 上层通常将其写入 attributes.content_index.<data_type>。
type ContentIndex struct {
	Kind        string                 `json:"kind"`
	DataType    string                 `json:"data_type,omitempty"`
	Format      string                 `json:"format,omitempty"`
	Unit        string                 `json:"unit,omitempty"`
	OffsetUnit  string                 `json:"offset_unit,omitempty"`
	Step        int64                  `json:"step,omitempty"`
	RowCount    int64                  `json:"row_count,omitempty"`
	HeaderBytes int64                  `json:"header_bytes,omitempty"`
	Source      map[string]interface{} `json:"source,omitempty"`
	Anchors     []ContentIndexAnchor   `json:"anchors,omitempty"`
}

type ContentIndexAnchor struct {
	Row        int64 `json:"row"`
	ByteOffset int64 `json:"byte_offset"`
}

// ContentIndexInfo 允许 info provider 在返回 TableInfo 时夹带通用访问索引。
// Meta 层消费后应将其写入 attributes.content_index，而不是 format_info。
type ContentIndexInfo struct {
	Table *ContentIndex
}

// GetSpatialInfo 获取空间扩展信息（便捷方法）
func (t *TableInfo) GetSpatialInfo() *SpatialInfo {
	if t == nil {
		return nil
	}
	return t.SpatialInfo
}

// GetContentIndexInfo 获取内容索引信息（便捷方法）
func (t *TableInfo) GetContentIndexInfo() *ContentIndexInfo {
	if t == nil {
		return nil
	}
	return t.ContentIndex
}

// IsSpatial 判断是否为空间数据
func (t *TableInfo) IsSpatial() bool {
	return t.GetSpatialInfo() != nil
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
