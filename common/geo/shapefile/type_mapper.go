package shapefile

import (
	"github.com/addp/common/format"
)

// TypeMapper Shapefile DBF类型映射器
type TypeMapper struct{}

// Name 返回映射器名称
func (m *TypeMapper) Name() string {
	return "shapefile"
}

// ToCommon 将Shapefile DBF类型转换为通用类型
// nativeType 为单字符字符串（如 "C", "N", "F", "L", "D", "M"）
func (m *TypeMapper) ToCommon(nativeType string) format.FieldType {
	if len(nativeType) == 0 {
		return format.FieldTypeUnknown
	}

	// DBF 类型是单字符
	switch nativeType[0] {
	case 'C': // Character
		return format.FieldTypeString
	case 'N': // Numeric
		return format.FieldTypeFloat // DBF的N类型可以是整数或浮点数
	case 'F': // Float
		return format.FieldTypeFloat
	case 'L': // Logical
		return format.FieldTypeBool
	case 'D': // Date
		return format.FieldTypeDate
	case 'M': // Memo
		return format.FieldTypeString
	default:
		return format.FieldTypeUnknown
	}
}

// FromCommon 将通用类型转换为Shapefile DBF类型
// 返回: (DBF类型字符, 字段长度, 小数位数)
func (m *TypeMapper) FromCommon(commonType format.FieldType) (string, int, int) {
	switch commonType {
	case format.FieldTypeString:
		return "C", 254, 0 // Character, 最大254字节
	case format.FieldTypeInt, format.FieldTypeBigInt:
		return "N", 18, 0 // Numeric, 18位整数
	case format.FieldTypeFloat, format.FieldTypeDecimal:
		return "F", 20, 8 // Float, 20位总长度，8位小数
	case format.FieldTypeBool:
		return "L", 1, 0 // Logical
	case format.FieldTypeDate:
		return "D", 8, 0 // Date (YYYYMMDD)
	default:
		return "C", 254, 0 // 默认为Character
	}
}

// ToCommonFromByte 从字节类型转换（兼容旧代码）
func (m *TypeMapper) ToCommonFromByte(dbfType byte) format.FieldType {
	return m.ToCommon(string(dbfType))
}

// FromCommonToDBF 将通用类型转换为DBF字节类型（兼容旧代码）
func (m *TypeMapper) FromCommonToDBF(commonType format.FieldType) (dbfType byte, size uint8, precision uint8) {
	typeStr, sizeInt, precisionInt := m.FromCommon(commonType)
	return typeStr[0], uint8(sizeInt), uint8(precisionInt)
}

// init 自动注册Shapefile类型映射器
func init() {
	format.RegisterTypeMapper(&TypeMapper{})
}
