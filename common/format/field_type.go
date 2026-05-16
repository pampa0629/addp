package format

// FieldType 标准化字段类型。
type FieldType string

const (
	FieldTypeString    FieldType = "string"
	FieldTypeInt       FieldType = "int"
	FieldTypeBigInt    FieldType = "bigint"
	FieldTypeFloat     FieldType = "float"
	FieldTypeDouble    FieldType = "double"
	FieldTypeDecimal   FieldType = "decimal"
	FieldTypeBool      FieldType = "bool"
	FieldTypeDate      FieldType = "date"
	FieldTypeTime      FieldType = "time"
	FieldTypeTimestamp FieldType = "timestamp"
	FieldTypeBytes     FieldType = "bytes"

	FieldTypeGeometry   FieldType = "geometry"
	FieldTypePoint      FieldType = "point"
	FieldTypeLineString FieldType = "linestring"
	FieldTypePolygon    FieldType = "polygon"
	FieldTypeMultiPoint FieldType = "multipoint"

	FieldTypeJSON  FieldType = "json"
	FieldTypeArray FieldType = "array"
	FieldTypeUUID  FieldType = "uuid"
	FieldTypeMixed FieldType = "mixed"

	FieldTypeUnknown FieldType = "unknown"
)

func IsNumeric(fieldType FieldType) bool {
	switch fieldType {
	case FieldTypeInt, FieldTypeBigInt, FieldTypeFloat, FieldTypeDouble, FieldTypeDecimal:
		return true
	default:
		return false
	}
}

func IsTemporalType(fieldType FieldType) bool {
	switch fieldType {
	case FieldTypeDate, FieldTypeTime, FieldTypeTimestamp:
		return true
	default:
		return false
	}
}

func IsGeometryType(fieldType FieldType) bool {
	switch fieldType {
	case FieldTypeGeometry, FieldTypePoint, FieldTypeLineString, FieldTypePolygon, FieldTypeMultiPoint:
		return true
	default:
		return false
	}
}
