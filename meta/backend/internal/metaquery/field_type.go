package metaquery

import (
	"strings"

	"github.com/addp/common/format"
	_ "github.com/addp/common/format/mappers/mysql"
	_ "github.com/addp/common/format/mappers/postgresql"
	_ "github.com/addp/common/format/mappers/spatialite"
)

func IsSpatialDataType(dataType string) bool {
	spatialTypes := []string{
		"geometry", "geography",
		"point", "linestring", "polygon",
		"multipoint", "multilinestring", "multipolygon",
		"geometrycollection",
	}
	lowerType := strings.ToLower(dataType)
	for _, t := range spatialTypes {
		if lowerType == t || strings.HasPrefix(lowerType, t+"(") {
			return true
		}
	}
	return false
}

// StandardizeFieldType 标准化字段类型。
func StandardizeFieldType(dataType, columnType string) string {
	if dataType == "" && columnType == "" {
		return string(format.FieldTypeUnknown)
	}

	typeToMap := dataType
	if typeToMap == "" {
		typeToMap = columnType
	}
	typeToMap = strings.ToLower(strings.TrimSpace(typeToMap))

	var standardType format.FieldType
	if isGeometryType(typeToMap) {
		if mapper := format.GetTypeMapper("postgresql"); mapper != nil {
			standardType = mapper.ToCommon(typeToMap)
		}
	} else {
		if mapper := format.GetTypeMapper("postgresql"); mapper != nil {
			standardType = mapper.ToCommon(typeToMap)
			if standardType != format.FieldTypeUnknown {
				return string(standardType)
			}
		}
		if mapper := format.GetTypeMapper("mysql"); mapper != nil {
			standardType = mapper.ToCommon(typeToMap)
			if standardType != format.FieldTypeUnknown {
				return string(standardType)
			}
		}
		if mapper := format.GetTypeMapper("spatialite"); mapper != nil {
			standardType = mapper.ToCommon(typeToMap)
			if standardType != format.FieldTypeUnknown {
				return string(standardType)
			}
		}
	}

	if standardType == "" || standardType == format.FieldTypeUnknown {
		return string(format.FieldTypeString)
	}
	return string(standardType)
}

func isGeometryType(typeName string) bool {
	typeLower := strings.ToLower(typeName)
	geometryKeywords := []string{
		"geometry", "point", "linestring", "polygon",
		"multipoint", "multilinestring", "multipolygon",
		"geometrycollection", "geom", "geog",
	}
	for _, keyword := range geometryKeywords {
		if strings.Contains(typeLower, keyword) {
			return true
		}
	}
	return false
}
