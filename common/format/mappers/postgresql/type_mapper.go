package postgresql

import (
	"github.com/addp/common/datatype"
	"strings"

	"github.com/addp/common/format"
)

// TypeMapper PostgreSQL类型映射器
type TypeMapper struct{}

// Name 返回映射器名称
func (m *TypeMapper) Name() string {
	return "postgresql"
}

// ToCommon 将PostgreSQL类型转换为通用类型
func (m *TypeMapper) ToCommon(pgType string) datatype.FieldType {
	pgType = strings.ToLower(strings.TrimSpace(pgType))

	// 处理带精度的类型（如 varchar(255) -> varchar）
	if idx := strings.Index(pgType, "("); idx > 0 {
		pgType = pgType[:idx]
	}

	switch pgType {
	// 字符串类型
	case "varchar", "character varying", "char", "character", "text", "name":
		return datatype.FieldTypeString

	// 整数类型
	case "smallint", "int2":
		return datatype.FieldTypeInt
	case "integer", "int", "int4":
		return datatype.FieldTypeInt
	case "bigint", "int8":
		return datatype.FieldTypeBigInt

	// 浮点类型
	case "real", "float4":
		return datatype.FieldTypeFloat // 4字节单精度
	case "double precision", "float8":
		return datatype.FieldTypeDouble // 8字节双精度
	case "numeric", "decimal":
		return datatype.FieldTypeDecimal

	// 布尔类型
	case "boolean", "bool":
		return datatype.FieldTypeBool

	// 日期时间类型
	case "date":
		return datatype.FieldTypeDate
	case "time", "time without time zone":
		return datatype.FieldTypeTime
	case "timestamp", "timestamp without time zone", "timestamp with time zone", "timestamptz":
		return datatype.FieldTypeTimestamp

	// 二进制类型
	case "bytea":
		return datatype.FieldTypeBytes

	// 地理空间类型
	case "geometry":
		return datatype.FieldTypeGeometry
	case "point":
		return datatype.FieldTypePoint
	case "linestring":
		return datatype.FieldTypeLineString
	case "polygon":
		return datatype.FieldTypePolygon

	// 复杂类型
	case "json", "jsonb":
		return datatype.FieldTypeJSON
	case "uuid":
		return datatype.FieldTypeUUID

	// 数组类型（简化处理）
	default:
		if strings.HasSuffix(pgType, "[]") {
			return datatype.FieldTypeArray
		}
		return datatype.FieldTypeUnknown
	}
}

// FromCommon 将通用类型转换为PostgreSQL类型
func (m *TypeMapper) FromCommon(commonType datatype.FieldType) (string, int, int) {
	switch commonType {
	case datatype.FieldTypeString:
		return "TEXT", 0, 0
	case datatype.FieldTypeInt:
		return "INTEGER", 0, 0
	case datatype.FieldTypeBigInt:
		return "BIGINT", 0, 0
	case datatype.FieldTypeFloat:
		return "REAL", 0, 0 // 4字节单精度
	case datatype.FieldTypeDouble:
		return "DOUBLE PRECISION", 0, 0 // 8字节双精度
	case datatype.FieldTypeDecimal:
		return "NUMERIC", 0, 0
	case datatype.FieldTypeBool:
		return "BOOLEAN", 0, 0
	case datatype.FieldTypeDate:
		return "DATE", 0, 0
	case datatype.FieldTypeTime:
		return "TIME", 0, 0
	case datatype.FieldTypeTimestamp:
		return "TIMESTAMP", 0, 0
	case datatype.FieldTypeBytes:
		return "BYTEA", 0, 0
	case datatype.FieldTypeGeometry:
		return "GEOMETRY", 0, 0
	case datatype.FieldTypePoint:
		return "GEOMETRY(Point)", 0, 0
	case datatype.FieldTypeLineString:
		return "GEOMETRY(LineString)", 0, 0
	case datatype.FieldTypePolygon:
		return "GEOMETRY(Polygon)", 0, 0
	case datatype.FieldTypeJSON:
		return "JSONB", 0, 0
	case datatype.FieldTypeUUID:
		return "UUID", 0, 0
	case datatype.FieldTypeArray:
		return "TEXT[]", 0, 0 // 默认为文本数组
	default:
		return "TEXT", 0, 0
	}
}

// init 自动注册PostgreSQL类型映射器
func init() {
	format.RegisterTypeMapper(&TypeMapper{})
}
