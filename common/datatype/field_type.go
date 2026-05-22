package datatype

import "strings"

// FieldType is ADDP's common semantic type for fields and properties.
type FieldType string

const (
	FieldTypeUnknown FieldType = "unknown"
	FieldTypeString  FieldType = "string"
	FieldTypeBool    FieldType = "bool"
	FieldTypeBytes   FieldType = "bytes"
	FieldTypeMixed   FieldType = "mixed"

	FieldTypeInt     FieldType = "int"
	FieldTypeBigInt  FieldType = "bigint"
	FieldTypeFloat   FieldType = "float"
	FieldTypeDouble  FieldType = "double"
	FieldTypeDecimal FieldType = "decimal"

	FieldTypeDate      FieldType = "date"
	FieldTypeTime      FieldType = "time"
	FieldTypeTimestamp FieldType = "timestamp"

	FieldTypeJSON  FieldType = "json"
	FieldTypeArray FieldType = "array"

	FieldTypeUUID FieldType = "uuid"

	FieldTypeGeometry   FieldType = "geometry"
	FieldTypePoint      FieldType = "point"
	FieldTypeLineString FieldType = "linestring"
	FieldTypePolygon    FieldType = "polygon"
	FieldTypeMultiPoint FieldType = "multipoint"
)

var knownFieldTypes = map[FieldType]struct{}{
	FieldTypeUnknown:    {},
	FieldTypeString:     {},
	FieldTypeBool:       {},
	FieldTypeBytes:      {},
	FieldTypeMixed:      {},
	FieldTypeInt:        {},
	FieldTypeBigInt:     {},
	FieldTypeFloat:      {},
	FieldTypeDouble:     {},
	FieldTypeDecimal:    {},
	FieldTypeDate:       {},
	FieldTypeTime:       {},
	FieldTypeTimestamp:  {},
	FieldTypeJSON:       {},
	FieldTypeArray:      {},
	FieldTypeUUID:       {},
	FieldTypeGeometry:   {},
	FieldTypePoint:      {},
	FieldTypeLineString: {},
	FieldTypePolygon:    {},
	FieldTypeMultiPoint: {},
}

// ParseFieldType normalizes a string into a known ADDP field type.
func ParseFieldType(value string) FieldType {
	fieldType := FieldType(strings.ToLower(strings.TrimSpace(value)))
	if IsKnownFieldType(fieldType) {
		return fieldType
	}
	return FieldTypeUnknown
}

// IsKnownFieldType reports whether fieldType is one of the standard ADDP field types.
func IsKnownFieldType(fieldType FieldType) bool {
	_, ok := knownFieldTypes[fieldType]
	return ok
}

// IsNumericFieldType reports whether fieldType belongs to the numeric family.
func IsNumericFieldType(fieldType FieldType) bool {
	switch fieldType {
	case FieldTypeInt, FieldTypeBigInt, FieldTypeFloat, FieldTypeDouble, FieldTypeDecimal:
		return true
	default:
		return false
	}
}

// IsTemporalFieldType reports whether fieldType belongs to the temporal family.
func IsTemporalFieldType(fieldType FieldType) bool {
	switch fieldType {
	case FieldTypeDate, FieldTypeTime, FieldTypeTimestamp:
		return true
	default:
		return false
	}
}

// IsSpatialFieldType reports whether fieldType belongs to the spatial family.
func IsSpatialFieldType(fieldType FieldType) bool {
	switch fieldType {
	case FieldTypeGeometry, FieldTypePoint, FieldTypeLineString, FieldTypePolygon, FieldTypeMultiPoint:
		return true
	default:
		return false
	}
}

// IsSemiStructuredFieldType reports whether fieldType belongs to the semi-structured family.
func IsSemiStructuredFieldType(fieldType FieldType) bool {
	switch fieldType {
	case FieldTypeJSON, FieldTypeArray:
		return true
	default:
		return false
	}
}
