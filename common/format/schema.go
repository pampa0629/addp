package format

import (
	"fmt"
)

// FieldType 标准化字段类型
type FieldType string

const (
	// 基础类型
	FieldTypeString    FieldType = "string"
	FieldTypeInt       FieldType = "int"       // 整数 (int32/int64)
	FieldTypeBigInt    FieldType = "bigint"    // 大整数 (int64)
	FieldTypeFloat     FieldType = "float"     // 浮点数 (float32/float64)
	FieldTypeDecimal   FieldType = "decimal"   // 高精度小数
	FieldTypeBool      FieldType = "bool"      // 布尔值
	FieldTypeDate      FieldType = "date"      // 日期 (YYYY-MM-DD)
	FieldTypeTime      FieldType = "time"      // 时间 (HH:MM:SS)
	FieldTypeTimestamp FieldType = "timestamp" // 时间戳 (YYYY-MM-DD HH:MM:SS)
	FieldTypeBytes     FieldType = "bytes"     // 二进制数据

	// 地理空间类型
	FieldTypeGeometry   FieldType = "geometry"   // 通用几何类型
	FieldTypePoint      FieldType = "point"      // 点
	FieldTypeLineString FieldType = "linestring" // 线
	FieldTypePolygon    FieldType = "polygon"    // 多边形
	FieldTypeMultiPoint FieldType = "multipoint" // 多点

	// 复杂类型
	FieldTypeJSON  FieldType = "json"  // JSON对象
	FieldTypeArray FieldType = "array" // 数组
	FieldTypeUUID  FieldType = "uuid"  // UUID

	// 未知类型
	FieldTypeUnknown FieldType = "unknown"
)

// Field 字段定义
type Field struct {
	Name      string    // 字段名
	Type      FieldType // 字段类型
	Nullable  bool      // 是否允许NULL
	Size      int       // 字符串长度或数值精度 (0表示不限制)
	Precision int       // 小数位数 (仅用于decimal/numeric类型)
	Comment   string    // 字段注释/描述

	// 扩展属性（可选）
	DefaultValue *string                // 默认值
	Extra        map[string]interface{} // 额外属性（如: unique, index, etc.）
}

// Schema 数据模式
type Schema struct {
	Fields        []Field  // 字段列表
	PrimaryKey    []string // 主键字段名
	GeometryField *string  // 地理空间数据的几何字段名
	GeometryType  *string  // 几何类型: Point, LineString, Polygon, etc.
	SpatialRefSys *string  // 空间参考系统: EPSG:4326, etc.

	// 元数据
	RecordCount *int64 // 记录数 (如果已知)
	Comment     string // Schema注释
}

// GetField 根据字段名查找字段定义
func (s *Schema) GetField(name string) *Field {
	for i := range s.Fields {
		if s.Fields[i].Name == name {
			return &s.Fields[i]
		}
	}
	return nil
}

// HasField 判断是否存在指定字段
func (s *Schema) HasField(name string) bool {
	return s.GetField(name) != nil
}

// IsPrimaryKey 判断字段是否为主键
func (s *Schema) IsPrimaryKey(fieldName string) bool {
	for _, pk := range s.PrimaryKey {
		if pk == fieldName {
			return true
		}
	}
	return false
}

// IsGeospatial 判断是否为地理空间数据
func (s *Schema) IsGeospatial() bool {
	return s.GeometryField != nil
}

// FieldNames 返回所有字段名
func (s *Schema) FieldNames() []string {
	names := make([]string, len(s.Fields))
	for i, field := range s.Fields {
		names[i] = field.Name
	}
	return names
}

// Validate 验证Schema的有效性
func (s *Schema) Validate() error {
	if len(s.Fields) == 0 {
		return fmt.Errorf("schema must have at least one field")
	}

	// 检查字段名唯一性
	seen := make(map[string]bool)
	for _, field := range s.Fields {
		if field.Name == "" {
			return fmt.Errorf("field name cannot be empty")
		}
		if seen[field.Name] {
			return fmt.Errorf("duplicate field name: %s", field.Name)
		}
		seen[field.Name] = true
	}

	// 检查主键字段存在
	for _, pk := range s.PrimaryKey {
		if !s.HasField(pk) {
			return fmt.Errorf("primary key field not found: %s", pk)
		}
	}

	// 检查几何字段存在
	if s.GeometryField != nil && !s.HasField(*s.GeometryField) {
		return fmt.Errorf("geometry field not found: %s", *s.GeometryField)
	}

	return nil
}

// TypeMapping 类型转换器（已弃用：请使用 TypeMapper 接口和注册表机制）
// 保留此类型仅用于向后兼容，新代码应使用 format.GetTypeMapper("postgresql") 等
type TypeMapping struct{}

// PostgreSQLToCommon 将PostgreSQL类型转换为通用类型
// 已弃用：使用 format.GetTypeMapper("postgresql").ToCommon(pgType)
func (m *TypeMapping) PostgreSQLToCommon(pgType string) FieldType {
	mapper := GetTypeMapper("postgresql")
	if mapper == nil {
		return FieldTypeUnknown
	}
	return mapper.ToCommon(pgType)
}

// MySQLToCommon 将MySQL类型转换为通用类型
// 已弃用：使用 format.GetTypeMapper("mysql").ToCommon(mysqlType)
func (m *TypeMapping) MySQLToCommon(mysqlType string) FieldType {
	mapper := GetTypeMapper("mysql")
	if mapper == nil {
		return FieldTypeUnknown
	}
	return mapper.ToCommon(mysqlType)
}

// ShapefileDBFToCommon 将Shapefile DBF类型转换为通用类型
// 已弃用：使用 format.GetTypeMapper("shapefile").ToCommon(string(dbfType))
func (m *TypeMapping) ShapefileDBFToCommon(dbfType byte) FieldType {
	mapper := GetTypeMapper("shapefile")
	if mapper == nil {
		return FieldTypeUnknown
	}
	return mapper.ToCommon(string(dbfType))
}

// CommonToPostgreSQL 将通用类型转换为PostgreSQL类型
// 已弃用：使用 format.GetTypeMapper("postgresql").FromCommon(commonType)
func (m *TypeMapping) CommonToPostgreSQL(commonType FieldType) string {
	mapper := GetTypeMapper("postgresql")
	if mapper == nil {
		return "TEXT"
	}
	nativeType, _, _ := mapper.FromCommon(commonType)
	return nativeType
}

// CommonToShapefileDBF 将通用类型转换为Shapefile DBF类型
// 已弃用：使用 format.GetTypeMapper("shapefile").FromCommon(commonType)
func (m *TypeMapping) CommonToShapefileDBF(commonType FieldType) (dbfType byte, size uint8, precision uint8) {
	mapper := GetTypeMapper("shapefile")
	if mapper == nil {
		return 'C', 254, 0
	}
	typeStr, sizeInt, precisionInt := mapper.FromCommon(commonType)
	if len(typeStr) > 0 {
		return typeStr[0], uint8(sizeInt), uint8(precisionInt)
	}
	return 'C', 254, 0
}

// IsNumeric 判断字段类型是否为数值类型
func IsNumeric(fieldType FieldType) bool {
	switch fieldType {
	case FieldTypeInt, FieldTypeBigInt, FieldTypeFloat, FieldTypeDecimal:
		return true
	default:
		return false
	}
}

// IsTemporalType 判断字段类型是否为时间类型
func IsTemporalType(fieldType FieldType) bool {
	switch fieldType {
	case FieldTypeDate, FieldTypeTime, FieldTypeTimestamp:
		return true
	default:
		return false
	}
}

// IsGeometryType 判断字段类型是否为几何类型
func IsGeometryType(fieldType FieldType) bool {
	switch fieldType {
	case FieldTypeGeometry, FieldTypePoint, FieldTypeLineString, FieldTypePolygon, FieldTypeMultiPoint:
		return true
	default:
		return false
	}
}
