package postgresql

import (
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
func (m *TypeMapper) ToCommon(pgType string) format.FieldType {
	pgType = strings.ToLower(strings.TrimSpace(pgType))

	// 处理带精度的类型（如 varchar(255) -> varchar）
	if idx := strings.Index(pgType, "("); idx > 0 {
		pgType = pgType[:idx]
	}

	switch pgType {
	// 字符串类型
	case "varchar", "character varying", "char", "character", "text", "name":
		return format.FieldTypeString

	// 整数类型
	case "smallint", "int2":
		return format.FieldTypeInt
	case "integer", "int", "int4":
		return format.FieldTypeInt
	case "bigint", "int8":
		return format.FieldTypeBigInt

	// 浮点类型
	case "real", "float4":
		return format.FieldTypeFloat
	case "double precision", "float8":
		return format.FieldTypeFloat
	case "numeric", "decimal":
		return format.FieldTypeDecimal

	// 布尔类型
	case "boolean", "bool":
		return format.FieldTypeBool

	// 日期时间类型
	case "date":
		return format.FieldTypeDate
	case "time", "time without time zone":
		return format.FieldTypeTime
	case "timestamp", "timestamp without time zone", "timestamp with time zone", "timestamptz":
		return format.FieldTypeTimestamp

	// 二进制类型
	case "bytea":
		return format.FieldTypeBytes

	// 地理空间类型
	case "geometry":
		return format.FieldTypeGeometry
	case "point":
		return format.FieldTypePoint
	case "linestring":
		return format.FieldTypeLineString
	case "polygon":
		return format.FieldTypePolygon

	// 复杂类型
	case "json", "jsonb":
		return format.FieldTypeJSON
	case "uuid":
		return format.FieldTypeUUID

	// 数组类型（简化处理）
	default:
		if strings.HasSuffix(pgType, "[]") {
			return format.FieldTypeArray
		}
		return format.FieldTypeUnknown
	}
}

// FromCommon 将通用类型转换为PostgreSQL类型
func (m *TypeMapper) FromCommon(commonType format.FieldType) (string, int, int) {
	switch commonType {
	case format.FieldTypeString:
		return "TEXT", 0, 0
	case format.FieldTypeInt:
		return "INTEGER", 0, 0
	case format.FieldTypeBigInt:
		return "BIGINT", 0, 0
	case format.FieldTypeFloat:
		return "DOUBLE PRECISION", 0, 0
	case format.FieldTypeDecimal:
		return "NUMERIC", 0, 0
	case format.FieldTypeBool:
		return "BOOLEAN", 0, 0
	case format.FieldTypeDate:
		return "DATE", 0, 0
	case format.FieldTypeTime:
		return "TIME", 0, 0
	case format.FieldTypeTimestamp:
		return "TIMESTAMP", 0, 0
	case format.FieldTypeBytes:
		return "BYTEA", 0, 0
	case format.FieldTypeGeometry:
		return "GEOMETRY", 0, 0
	case format.FieldTypePoint:
		return "GEOMETRY(Point)", 0, 0
	case format.FieldTypeLineString:
		return "GEOMETRY(LineString)", 0, 0
	case format.FieldTypePolygon:
		return "GEOMETRY(Polygon)", 0, 0
	case format.FieldTypeJSON:
		return "JSONB", 0, 0
	case format.FieldTypeUUID:
		return "UUID", 0, 0
	case format.FieldTypeArray:
		return "TEXT[]", 0, 0 // 默认为文本数组
	default:
		return "TEXT", 0, 0
	}
}

// init 自动注册PostgreSQL类型映射器
func init() {
	format.RegisterTypeMapper(&TypeMapper{})
}
