package mysql

import (
	"strings"

	"github.com/addp/common/format"
)

// TypeMapper MySQL类型映射器
type TypeMapper struct{}

// Name 返回映射器名称
func (m *TypeMapper) Name() string {
	return "mysql"
}

// ToCommon 将MySQL类型转换为通用类型
func (m *TypeMapper) ToCommon(mysqlType string) format.FieldType {
	mysqlType = strings.ToLower(strings.TrimSpace(mysqlType))

	// 处理带精度的类型
	if idx := strings.Index(mysqlType, "("); idx > 0 {
		mysqlType = mysqlType[:idx]
	}

	switch mysqlType {
	// 字符串类型
	case "varchar", "char", "text", "tinytext", "mediumtext", "longtext":
		return format.FieldTypeString

	// 整数类型
	case "tinyint", "smallint", "mediumint", "int", "integer":
		return format.FieldTypeInt
	case "bigint":
		return format.FieldTypeBigInt

	// 浮点类型
	case "float":
		return format.FieldTypeFloat // 4字节单精度
	case "double", "real":
		return format.FieldTypeDouble // 8字节双精度
	case "decimal", "numeric":
		return format.FieldTypeDecimal

	// 布尔类型 (MySQL没有真正的bool，用tinyint(1)表示)
	case "boolean", "bool":
		return format.FieldTypeBool

	// 日期时间类型
	case "date":
		return format.FieldTypeDate
	case "time":
		return format.FieldTypeTime
	case "datetime", "timestamp":
		return format.FieldTypeTimestamp

	// 二进制类型
	case "binary", "varbinary", "blob", "tinyblob", "mediumblob", "longblob":
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
	case "multipoint":
		return format.FieldTypeMultiPoint
	case "multilinestring", "multipolygon", "geometrycollection":
		return format.FieldTypeGeometry

	// 复杂类型
	case "json":
		return format.FieldTypeJSON

	default:
		return format.FieldTypeUnknown
	}
}

// FromCommon 将通用类型转换为MySQL类型
func (m *TypeMapper) FromCommon(commonType format.FieldType) (string, int, int) {
	switch commonType {
	case format.FieldTypeString:
		return "TEXT", 0, 0
	case format.FieldTypeInt:
		return "INT", 0, 0
	case format.FieldTypeBigInt:
		return "BIGINT", 0, 0
	case format.FieldTypeFloat:
		return "FLOAT", 0, 0 // 4字节单精度
	case format.FieldTypeDouble:
		return "DOUBLE", 0, 0 // 8字节双精度
	case format.FieldTypeDecimal:
		return "DECIMAL", 10, 2 // 默认 (10,2)
	case format.FieldTypeBool:
		return "TINYINT", 1, 0 // TINYINT(1)
	case format.FieldTypeDate:
		return "DATE", 0, 0
	case format.FieldTypeTime:
		return "TIME", 0, 0
	case format.FieldTypeTimestamp:
		return "DATETIME", 0, 0
	case format.FieldTypeBytes:
		return "BLOB", 0, 0
	case format.FieldTypeGeometry:
		return "GEOMETRY", 0, 0
	case format.FieldTypePoint:
		return "POINT", 0, 0
	case format.FieldTypeLineString:
		return "LINESTRING", 0, 0
	case format.FieldTypePolygon:
		return "POLYGON", 0, 0
	case format.FieldTypeMultiPoint:
		return "MULTIPOINT", 0, 0
	case format.FieldTypeJSON:
		return "JSON", 0, 0
	case format.FieldTypeUUID:
		return "VARCHAR", 36, 0 // UUID 存储为 VARCHAR(36)
	default:
		return "TEXT", 0, 0
	}
}

// init 自动注册MySQL类型映射器
func init() {
	format.RegisterTypeMapper(&TypeMapper{})
}
