package jsonformat

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/addp/common/datatype"
)

type tableInfoBuilder struct {
	geometryField string
	fieldTypes    map[string]datatype.FieldType
	geometryTypes map[string]struct{}
	propertySet   map[string]struct{}
	bounds        geometryBounds
	srid          int
}

func newTableInfoBuilder(geometryField string) *tableInfoBuilder {
	return &tableInfoBuilder{
		geometryField: geometryField,
		fieldTypes:    make(map[string]datatype.FieldType),
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

type tableInfoBuildResult struct {
	Table   *datatype.TableInfo
	Spatial *datatype.SpatialInfo
}

func (b *tableInfoBuilder) Build() tableInfoBuildResult {
	fieldNames := make([]string, 0, len(b.propertySet))
	for name := range b.propertySet {
		if name == "" {
			continue
		}
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)

	fields := make([]datatype.FieldInfo, 0, len(fieldNames)+1)
	geometryField := b.geometryField
	if b.HasGeometry() {
		fields = append(fields, datatype.FieldInfo{
			Name:     geometryField,
			Type:     datatype.FieldTypeGeometry,
			Nullable: false,
		})
	}
	for _, name := range fieldNames {
		fieldType := b.fieldTypes[name]
		if fieldType == "" {
			fieldType = datatype.FieldTypeUnknown
		}
		fields = append(fields, datatype.FieldInfo{
			Name:     name,
			Type:     fieldType,
			Nullable: true,
		})
	}

	result := tableInfoBuildResult{
		Table: &datatype.TableInfo{
			Name:   "json_records",
			Fields: fields,
		},
	}
	if b.HasGeometry() {
		result.Spatial = datatype.NewSingleGeometrySpatialInfo(geometryField, b.GeometryType(), b.SRID(), 2)
	}
	return result
}

func (b *tableInfoBuilder) BuildSchema() *datatype.TableInfo {
	result := b.Build()
	if result.Table == nil {
		return &datatype.TableInfo{}
	}
	return result.Table.Clone()
}

func (b *tableInfoBuilder) BuildTableInfo() *datatype.TableInfo {
	return b.Build().Table
}

func inferFieldType(value interface{}) datatype.FieldType {
	switch v := value.(type) {
	case nil:
		return datatype.FieldTypeUnknown
	case bool:
		return datatype.FieldTypeBool
	case int, int8, int16, int32, int64:
		return datatype.FieldTypeInt
	case uint, uint8, uint16, uint32, uint64:
		return datatype.FieldTypeBigInt
	case float32:
		return datatype.FieldTypeFloat
	case float64:
		return datatype.FieldTypeDouble
	case json.Number:
		str := v.String()
		if strings.Contains(str, ".") {
			return datatype.FieldTypeDouble
		}
		return datatype.FieldTypeInt
	case string:
		if looksLikeDate(v) {
			return datatype.FieldTypeDate
		}
		if looksLikeTimestamp(v) {
			return datatype.FieldTypeTimestamp
		}
		return datatype.FieldTypeString
	case map[string]interface{}:
		return datatype.FieldTypeJSON
	case []interface{}:
		return datatype.FieldTypeArray
	default:
		return datatype.FieldTypeString
	}
}

func mergeFieldType(current, next datatype.FieldType) datatype.FieldType {
	if current == "" || current == datatype.FieldTypeUnknown {
		return next
	}
	if next == "" || next == datatype.FieldTypeUnknown {
		return current
	}
	if current == next {
		return current
	}
	if isNumericType(current) && isNumericType(next) {
		if current == datatype.FieldTypeDecimal || next == datatype.FieldTypeDecimal {
			return datatype.FieldTypeDecimal
		}
		if current == datatype.FieldTypeDouble || next == datatype.FieldTypeDouble {
			return datatype.FieldTypeDouble
		}
		if current == datatype.FieldTypeFloat || next == datatype.FieldTypeFloat {
			return datatype.FieldTypeFloat
		}
		if current == datatype.FieldTypeBigInt || next == datatype.FieldTypeBigInt {
			return datatype.FieldTypeBigInt
		}
		return datatype.FieldTypeInt
	}
	if isTemporalType(current) && isTemporalType(next) {
		return datatype.FieldTypeString
	}
	return datatype.FieldTypeString
}

func isNumericType(t datatype.FieldType) bool {
	switch t {
	case datatype.FieldTypeInt, datatype.FieldTypeBigInt, datatype.FieldTypeFloat, datatype.FieldTypeDouble, datatype.FieldTypeDecimal:
		return true
	default:
		return false
	}
}

func isTemporalType(t datatype.FieldType) bool {
	switch t {
	case datatype.FieldTypeDate, datatype.FieldTypeTime, datatype.FieldTypeTimestamp:
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
