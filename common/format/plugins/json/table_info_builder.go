package jsonformat

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/addp/common/format"
)

type tableInfoBuilder struct {
	geometryField string
	fieldTypes    map[string]format.FieldType
	geometryTypes map[string]struct{}
	propertySet   map[string]struct{}
	bounds        geometryBounds
	srid          int
}

func newTableInfoBuilder(geometryField string) *tableInfoBuilder {
	return &tableInfoBuilder{
		geometryField: geometryField,
		fieldTypes:    make(map[string]format.FieldType),
		geometryTypes: make(map[string]struct{}),
		propertySet:   make(map[string]struct{}),
	}
}

func (b *tableInfoBuilder) AddFeature(feature *Feature) {
	if feature == nil {
		return
	}
	if gt := feature.GeometryType(); gt != "" {
		b.geometryTypes[gt] = struct{}{}
		if feature.GeometryField != "" {
			b.geometryField = feature.GeometryField
		}
		if b.srid == 0 {
			b.srid = geometrySRID(feature.Geometry)
		}
		b.bounds.AddGeometry(feature.Geometry)
	}
	for key, val := range feature.Properties {
		b.propertySet[key] = struct{}{}
		fieldType := inferFieldType(val)
		b.fieldTypes[key] = mergeFieldType(b.fieldTypes[key], fieldType)
	}
}

func (b *tableInfoBuilder) GeometryType() string {
	if len(b.geometryTypes) == 0 {
		return ""
	}
	if len(b.geometryTypes) == 1 {
		for gt := range b.geometryTypes {
			return gt
		}
	}
	return "Geometry"
}

func (b *tableInfoBuilder) HasGeometry() bool {
	return len(b.geometryTypes) > 0
}

func (b *tableInfoBuilder) BoundingBox() ([4]float64, bool) {
	return b.bounds.BoundingBox()
}

func (b *tableInfoBuilder) SRID() int {
	return b.srid
}

func (b *tableInfoBuilder) Build() *format.TableInfo {
	fieldNames := make([]string, 0, len(b.propertySet))
	for name := range b.propertySet {
		if name == "" {
			continue
		}
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)

	fields := make([]format.FieldInfo, 0, len(fieldNames)+1)
	geometryField := b.geometryField
	if b.HasGeometry() {
		fields = append(fields, format.FieldInfo{
			Name:     geometryField,
			Type:     format.FieldTypeGeometry,
			Nullable: false,
		})
	}
	for _, name := range fieldNames {
		fieldType := b.fieldTypes[name]
		if fieldType == "" {
			fieldType = format.FieldTypeUnknown
		}
		fields = append(fields, format.FieldInfo{
			Name:     name,
			Type:     fieldType,
			Nullable: true,
		})
	}

	tableInfo := &format.TableInfo{
		Name:   "json_records",
		Fields: fields,
	}
	if b.HasGeometry() {
		tableInfo.SpatialInfo = &format.SpatialInfo{
			GeometryColumn: geometryField,
			GeometryType:   b.GeometryType(),
			SRID:           b.SRID(),
			Dimension:      2,
		}
	}
	return tableInfo
}

func inferFieldType(value interface{}) format.FieldType {
	switch v := value.(type) {
	case nil:
		return format.FieldTypeUnknown
	case bool:
		return format.FieldTypeBool
	case int, int8, int16, int32, int64:
		return format.FieldTypeInt
	case uint, uint8, uint16, uint32, uint64:
		return format.FieldTypeBigInt
	case float32:
		return format.FieldTypeFloat
	case float64:
		return format.FieldTypeDouble
	case json.Number:
		str := v.String()
		if strings.Contains(str, ".") {
			return format.FieldTypeDouble
		}
		return format.FieldTypeInt
	case string:
		if looksLikeDate(v) {
			return format.FieldTypeDate
		}
		if looksLikeTimestamp(v) {
			return format.FieldTypeTimestamp
		}
		return format.FieldTypeString
	case map[string]interface{}:
		return format.FieldTypeJSON
	case []interface{}:
		return format.FieldTypeArray
	default:
		return format.FieldTypeString
	}
}

func mergeFieldType(current, next format.FieldType) format.FieldType {
	if current == "" || current == format.FieldTypeUnknown {
		return next
	}
	if next == "" || next == format.FieldTypeUnknown {
		return current
	}
	if current == next {
		return current
	}
	if isNumericType(current) && isNumericType(next) {
		if current == format.FieldTypeDecimal || next == format.FieldTypeDecimal {
			return format.FieldTypeDecimal
		}
		if current == format.FieldTypeDouble || next == format.FieldTypeDouble {
			return format.FieldTypeDouble
		}
		if current == format.FieldTypeFloat || next == format.FieldTypeFloat {
			return format.FieldTypeFloat
		}
		if current == format.FieldTypeBigInt || next == format.FieldTypeBigInt {
			return format.FieldTypeBigInt
		}
		return format.FieldTypeInt
	}
	if isTemporalType(current) && isTemporalType(next) {
		return format.FieldTypeString
	}
	return format.FieldTypeString
}

func isNumericType(t format.FieldType) bool {
	switch t {
	case format.FieldTypeInt, format.FieldTypeBigInt, format.FieldTypeFloat, format.FieldTypeDouble, format.FieldTypeDecimal:
		return true
	default:
		return false
	}
}

func isTemporalType(t format.FieldType) bool {
	switch t {
	case format.FieldTypeDate, format.FieldTypeTime, format.FieldTypeTimestamp:
		return true
	default:
		return false
	}
}

func looksLikeDate(value string) bool {
	if len(value) != 10 {
		return false
	}
	if value[4] != '-' || value[7] != '-' {
		return false
	}
	return true
}

func looksLikeTimestamp(value string) bool {
	return strings.Contains(value, "T") || strings.Count(value, ":") >= 2
}
