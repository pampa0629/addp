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
	return dbfNativeTypeToCommon(nativeType)
}

// FromCommon 将通用类型转换为Shapefile DBF类型
// 返回: (DBF类型字符, 字段长度, 小数位数)
func (m *TypeMapper) FromCommon(commonType format.FieldType) (string, int, int) {
	return commonTypeToDBFNative(commonType)
}

func dbfNativeTypeToCommon(nativeType string) format.FieldType {
	if len(nativeType) == 0 {
		return format.FieldTypeUnknown
	}

	switch nativeType[0] {
	case 'C':
		return format.FieldTypeString
	case 'N':
		return format.FieldTypeFloat
	case 'F':
		return format.FieldTypeFloat
	case 'L':
		return format.FieldTypeBool
	case 'D':
		return format.FieldTypeDate
	case 'M':
		return format.FieldTypeString
	default:
		return format.FieldTypeUnknown
	}
}

func commonTypeToDBFNative(commonType format.FieldType) (string, int, int) {
	switch commonType {
	case format.FieldTypeString:
		return "C", 254, 0 // Character, 最大254字节
	case format.FieldTypeInt, format.FieldTypeBigInt:
		return "N", 18, 0 // Numeric, 18位整数
	case format.FieldTypeFloat:
		return "F", 13, 6 // Float, 单精度，13位总长度，6位小数
	case format.FieldTypeDouble:
		return "F", 20, 8 // Float, 双精度，20位总长度，8位小数
	case format.FieldTypeDecimal:
		return "N", 20, 8 // Numeric, 高精度小数
	case format.FieldTypeBool:
		return "L", 1, 0 // Logical
	case format.FieldTypeDate:
		return "D", 8, 0 // Date (YYYYMMDD)
	default:
		return "C", 254, 0 // 默认为Character
	}
}

// init 自动注册Shapefile类型映射器
func init() {
	format.RegisterTypeMapper(&TypeMapper{})
}
