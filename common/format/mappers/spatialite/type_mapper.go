package spatialite

import (
	"github.com/addp/common/datatype"
	"strings"

	"github.com/addp/common/format"
)

// TypeMapper SpatiaLite/SQLite 类型映射器
type TypeMapper struct{}

// Name 返回映射器名称
func (m *TypeMapper) Name() string {
	return "spatialite"
}

// ToCommon 将 SpatiaLite/SQLite 类型转换为通用类型
func (m *TypeMapper) ToCommon(sqliteType string) datatype.FieldType {
	sqliteType = strings.ToUpper(strings.TrimSpace(sqliteType))

	// 处理带精度的类型（如 VARCHAR(255) -> VARCHAR）
	if idx := strings.Index(sqliteType, "("); idx > 0 {
		sqliteType = sqliteType[:idx]
	}

	switch sqliteType {
	// 字符串类型
	case "TEXT", "VARCHAR", "CHAR", "CHARACTER", "CLOB":
		return datatype.FieldTypeString

	// 整数类型
	// SQLite 的 INTEGER 存储类可以存储 1-8 字节整数
	case "INTEGER", "INT", "TINYINT", "SMALLINT", "MEDIUMINT", "BIGINT":
		return datatype.FieldTypeInt
	case "INT2", "INT8":
		return datatype.FieldTypeInt

	// 浮点类型
	// SQLite 的 REAL 存储类使用 8 字节 IEEE 浮点数 (双精度)
	// FLOAT、DOUBLE、NUMERIC、DECIMAL 都是 REAL 的别名
	case "REAL", "DOUBLE", "FLOAT", "NUMERIC", "DECIMAL":
		return datatype.FieldTypeDouble // 8字节双精度

	// 布尔类型（SQLite 没有真正的 BOOLEAN，通常用 INTEGER 0/1）
	case "BOOLEAN", "BOOL":
		return datatype.FieldTypeBool

	// 日期时间类型（SQLite 没有原生日期时间类型，通常存为 TEXT 或 INTEGER）
	case "DATE":
		return datatype.FieldTypeDate
	case "TIME":
		return datatype.FieldTypeTime
	case "DATETIME", "TIMESTAMP":
		return datatype.FieldTypeTimestamp

	// 二进制类型
	case "BLOB", "BINARY":
		return datatype.FieldTypeBytes

	// 地理空间类型（SpatiaLite 扩展）
	case "GEOMETRY", "POINT", "LINESTRING", "POLYGON", "MULTIPOINT", "MULTILINESTRING", "MULTIPOLYGON", "GEOMETRYCOLLECTION":
		return datatype.FieldTypeGeometry

	// 默认为字符串（SQLite 的亲和类型机制）
	default:
		return datatype.FieldTypeString
	}
}

// FromCommon 将通用类型转换为 SpatiaLite/SQLite 类型
func (m *TypeMapper) FromCommon(commonType datatype.FieldType) (string, int, int) {
	switch commonType {
	case datatype.FieldTypeString:
		return "TEXT", 0, 0
	case datatype.FieldTypeInt:
		return "INTEGER", 0, 0
	case datatype.FieldTypeBigInt:
		return "INTEGER", 0, 0 // SQLite INTEGER 可以存储最多 8 字节
	case datatype.FieldTypeFloat:
		return "REAL", 0, 0 // SQLite REAL 是 8 字节双精度，但兼容单精度
	case datatype.FieldTypeDouble:
		return "REAL", 0, 0 // SQLite REAL 是 8 字节双精度
	case datatype.FieldTypeDecimal:
		return "REAL", 0, 0 // SQLite 没有真正的 DECIMAL，用 REAL
	case datatype.FieldTypeBool:
		return "INTEGER", 0, 0 // 布尔值用 INTEGER 0/1
	case datatype.FieldTypeDate:
		return "TEXT", 0, 0 // 日期存为 TEXT (ISO8601 格式)
	case datatype.FieldTypeTime:
		return "TEXT", 0, 0
	case datatype.FieldTypeTimestamp:
		return "TEXT", 0, 0 // 或用 INTEGER 存 Unix 时间戳
	case datatype.FieldTypeBytes:
		return "BLOB", 0, 0
	case datatype.FieldTypeGeometry:
		return "GEOMETRY", 0, 0
	case datatype.FieldTypeJSON:
		return "TEXT", 0, 0 // SQLite 3.38.0+ 支持 JSON 函数，但存为 TEXT
	case datatype.FieldTypeUUID:
		return "TEXT", 0, 0 // UUID 存为 TEXT
	case datatype.FieldTypeArray:
		return "TEXT", 0, 0 // 数组序列化为 JSON TEXT
	default:
		return "TEXT", 0, 0
	}
}

// init 自动注册 SpatiaLite 类型映射器
func init() {
	format.RegisterTypeMapper(&TypeMapper{})
}
