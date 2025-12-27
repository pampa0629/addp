package plugin

import "strings"

// StandardType 标准数据类型
type StandardType string

const (
	StdTypeInteger  StandardType = "integer"
	StdTypeNumber   StandardType = "number"
	StdTypeString   StandardType = "string"
	StdTypeBoolean  StandardType = "boolean"
	StdTypeDatetime StandardType = "datetime"
	StdTypeJSON     StandardType = "json"
	StdTypeBinary   StandardType = "binary"
	StdTypeArray    StandardType = "array"
)

// TypeMapping 类型映射配置
type TypeMapping struct {
	// DBTypes 数据库原生类型（大小写不敏感）
	DBTypes []string
	// StdType 映射到的标准类型
	StdType StandardType
}

// CommonTypeMappings 通用类型映射表（适用于大多数SQL数据库）
var CommonTypeMappings = []TypeMapping{
	// 整数类型
	{DBTypes: []string{"tinyint", "smallint", "mediumint", "int", "integer", "int2", "int4", "int8", "bigint", "largeint"}, StdType: StdTypeInteger},

	// 浮点数类型
	{DBTypes: []string{"float", "double", "real", "decimal", "numeric", "decimalv3", "float4", "float8", "double precision", "money"}, StdType: StdTypeNumber},

	// 字符串类型
	{DBTypes: []string{"char", "varchar", "text", "string", "tinytext", "mediumtext", "longtext", "character", "character varying"}, StdType: StdTypeString},

	// 布尔类型
	{DBTypes: []string{"bool", "boolean", "bit"}, StdType: StdTypeBoolean},

	// 日期时间类型
	{DBTypes: []string{"date", "time", "datetime", "timestamp", "year", "datev2", "datetimev2", "timestamp with time zone", "timestamp without time zone", "time with time zone", "time without time zone"}, StdType: StdTypeDatetime},

	// JSON类型
	{DBTypes: []string{"json", "jsonb"}, StdType: StdTypeJSON},

	// 二进制类型
	{DBTypes: []string{"binary", "varbinary", "blob", "tinyblob", "mediumblob", "longblob", "bytea", "hll", "bitmap"}, StdType: StdTypeBinary},

	// 数组类型
	{DBTypes: []string{"array", "ARRAY"}, StdType: StdTypeArray},
}

// MapToStandardType 将数据库原生类型映射为标准类型
// 使用通用映射表，大小写不敏感
func MapToStandardType(dbType string) string {
	dbType = strings.ToLower(strings.TrimSpace(dbType))

	for _, mapping := range CommonTypeMappings {
		for _, t := range mapping.DBTypes {
			if strings.ToLower(t) == dbType {
				return string(mapping.StdType)
			}
		}
	}

	// 特殊处理：复合类型（map, struct, enum, set）映射为json
	if strings.Contains(dbType, "map") || strings.Contains(dbType, "struct") ||
		strings.Contains(dbType, "enum") || strings.Contains(dbType, "set") {
		return string(StdTypeJSON)
	}

	// UUID 映射为字符串
	if strings.Contains(dbType, "uuid") {
		return string(StdTypeString)
	}

	// 默认返回字符串类型
	return string(StdTypeString)
}

// MapToStandardTypeWithCustom 使用自定义映射表和通用映射表
// customMappings 优先级高于通用映射表
func MapToStandardTypeWithCustom(dbType string, customMappings []TypeMapping) string {
	dbType = strings.ToLower(strings.TrimSpace(dbType))

	// 先检查自定义映射
	for _, mapping := range customMappings {
		for _, t := range mapping.DBTypes {
			if strings.ToLower(t) == dbType {
				return string(mapping.StdType)
			}
		}
	}

	// 回退到通用映射
	return MapToStandardType(dbType)
}
