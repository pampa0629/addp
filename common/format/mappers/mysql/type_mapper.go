package mysql

import (
	"github.com/addp/common/datatype"
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
func (m *TypeMapper) ToCommon(mysqlType string) datatype.FieldType {
	mysqlType = strings.ToLower(strings.TrimSpace(mysqlType))

	// 处理带精度的类型
	if idx := strings.Index(mysqlType, "("); idx > 0 {
		mysqlType = mysqlType[:idx]
	}

	switch mysqlType {
	// 字符串类型
	case "varchar", "char", "text", "tinytext", "mediumtext", "longtext":
		return datatype.FieldTypeString

	// 整数类型
	case "tinyint", "smallint", "mediumint", "int", "integer":
		return datatype.FieldTypeInt
	case "bigint":
		return datatype.FieldTypeBigInt

	// 浮点类型
	case "float":
		return datatype.FieldTypeFloat // 4字节单精度
	case "double", "real":
		return datatype.FieldTypeDouble // 8字节双精度
	case "decimal", "numeric":
		return datatype.FieldTypeDecimal

	// 布尔类型 (MySQL没有真正的bool，用tinyint(1)表示)
	case "boolean", "bool":
		return datatype.FieldTypeBool

	// 日期时间类型
	case "date":
		return datatype.FieldTypeDate
	case "time":
		return datatype.FieldTypeTime
	case "datetime", "timestamp":
		return datatype.FieldTypeTimestamp

	// 二进制类型
	case "binary", "varbinary", "blob", "tinyblob", "mediumblob", "longblob":
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
	case "multipoint":
		return datatype.FieldTypeMultiPoint
	case "multilinestring", "multipolygon", "geometrycollection":
		return datatype.FieldTypeGeometry

	// 复杂类型
	case "json":
		return datatype.FieldTypeJSON

	default:
		return datatype.FieldTypeUnknown
	}
}

// FromCommon 将通用类型转换为MySQL类型
func (m *TypeMapper) FromCommon(commonType datatype.FieldType) (string, int, int) {
	switch commonType {
	case datatype.FieldTypeString:
		return "TEXT", 0, 0
	case datatype.FieldTypeInt:
		return "INT", 0, 0
	case datatype.FieldTypeBigInt:
		return "BIGINT", 0, 0
	case datatype.FieldTypeFloat:
		return "FLOAT", 0, 0 // 4字节单精度
	case datatype.FieldTypeDouble:
		return "DOUBLE", 0, 0 // 8字节双精度
	case datatype.FieldTypeDecimal:
		return "DECIMAL", 10, 2 // 默认 (10,2)
	case datatype.FieldTypeBool:
		return "TINYINT", 1, 0 // TINYINT(1)
	case datatype.FieldTypeDate:
		return "DATE", 0, 0
	case datatype.FieldTypeTime:
		return "TIME", 0, 0
	case datatype.FieldTypeTimestamp:
		return "DATETIME", 0, 0
	case datatype.FieldTypeBytes:
		return "BLOB", 0, 0
	case datatype.FieldTypeGeometry:
		return "GEOMETRY", 0, 0
	case datatype.FieldTypePoint:
		return "POINT", 0, 0
	case datatype.FieldTypeLineString:
		return "LINESTRING", 0, 0
	case datatype.FieldTypePolygon:
		return "POLYGON", 0, 0
	case datatype.FieldTypeMultiPoint:
		return "MULTIPOINT", 0, 0
	case datatype.FieldTypeJSON:
		return "JSON", 0, 0
	case datatype.FieldTypeUUID:
		return "VARCHAR", 36, 0 // UUID 存储为 VARCHAR(36)
	default:
		return "TEXT", 0, 0
	}
}

// init 自动注册MySQL类型映射器
func init() {
	format.RegisterTypeMapper(&TypeMapper{})
}
