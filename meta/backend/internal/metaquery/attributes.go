package metaquery

import (
	"fmt"
	"strings"

	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
)

func FieldsFromMetaItem(item models.MetaItem) ([]commonModels.FieldInfo, error) {
	fieldsList, ok := sliceAttributeFromSection(item.Attributes, "type_info.table", "fields")
	if !ok {
		return []commonModels.FieldInfo{}, nil
	}

	fieldInfos := make([]commonModels.FieldInfo, 0, len(fieldsList))
	for _, fieldData := range fieldsList {
		fieldMap, ok := fieldData.(map[string]interface{})
		if !ok {
			continue
		}

		dataType := toString(fieldMap["data_type"])
		fieldInfos = append(fieldInfos, commonModels.FieldInfo{
			Name:         toString(fieldMap["name"]),
			DataType:     dataType,
			IsPrimaryKey: toBool(fieldMap["is_primary_key"]),
			IsNullable:   toBool(fieldMap["is_nullable"]),
			Comment:      toString(fieldMap["comment"]),
			IsSpatial:    toBool(fieldMap["is_spatial"]),
			GeometryType: toString(fieldMap["geometry_type"]),
			SRID:         int(toInt(fieldMap["srid"])),
		})
	}

	return fieldInfos, nil
}

func SpatialMetadataFromItem(item models.MetaItem) (*models.SpatialMetadataResponse, error) {
	spatialMeta := &models.SpatialMetadataResponse{
		Fields: []models.FieldInfo{},
	}

	if spatialData, ok := spatialMetadataAttribute(item.Attributes); ok {
		if geomCol, ok := spatialData["primary_geometry_column"].(string); ok {
			spatialMeta.GeometryColumn = geomCol
		}
		applyGeometryColumns(spatialMeta, spatialData["geometry_columns"])

		if srid, ok := spatialData["srid"].(float64); ok {
			spatialMeta.SRID = int(srid)
		}
		if extentSRID, ok := spatialData["extent_srid"].(float64); ok {
			spatialMeta.ExtentSRID = int(extentSRID)
		}
		if extent, ok := spatialData["extent"].([]interface{}); ok {
			spatialMeta.Extent = make([]float64, len(extent))
			for i, v := range extent {
				if f, ok := v.(float64); ok {
					spatialMeta.Extent[i] = f
				}
			}
		}
		if geomTypes, ok := spatialData["geometry_types"].([]interface{}); ok {
			spatialMeta.GeometryTypes = make([]string, 0, len(geomTypes))
			for _, v := range geomTypes {
				if s, ok := v.(string); ok {
					spatialMeta.GeometryTypes = append(spatialMeta.GeometryTypes, s)
				}
			}
		}
	}

	if tableMeta, ok := mapAttributeFromSection(item.Attributes, "type_info.table", "table_metadata"); ok {
		if pk, ok := tableMeta["primary_key"].(string); ok {
			spatialMeta.PrimaryKey = pk
		} else if pkArray, ok := tableMeta["primary_key"].([]interface{}); ok && len(pkArray) > 0 {
			if pkStr, ok := pkArray[0].(string); ok {
				spatialMeta.PrimaryKey = pkStr
			}
		}
	}

	if fields, ok := sliceAttributeFromSection(item.Attributes, "type_info.table", "fields"); ok {
		for _, f := range fields {
			if fieldMap, ok := f.(map[string]interface{}); ok {
				spatialMeta.Fields = append(spatialMeta.Fields, models.FieldInfo{
					Name:         toString(fieldMap["name"]),
					DataType:     toString(fieldMap["data_type"]),
					IsPrimaryKey: toBool(fieldMap["is_primary_key"]),
				})
			}
		}
	}

	if item.RowCount != nil {
		spatialMeta.RowCount = *item.RowCount
	}

	return spatialMeta, nil
}

func applyGeometryColumns(spatialMeta *models.SpatialMetadataResponse, rawColumns interface{}) {
	switch columns := rawColumns.(type) {
	case []interface{}:
		for _, rawColumn := range columns {
			if applyGeometryColumn(spatialMeta, rawColumn) {
				return
			}
		}
	case []map[string]interface{}:
		for _, column := range columns {
			if applyGeometryColumn(spatialMeta, column) {
				return
			}
		}
	}
}

func applyGeometryColumn(spatialMeta *models.SpatialMetadataResponse, rawColumn interface{}) bool {
	var column map[string]interface{}
	switch typed := rawColumn.(type) {
	case map[string]interface{}:
		column = typed
	case models.JSONMap:
		column = map[string]interface{}(typed)
	default:
		return false
	}

	if spatialMeta.GeometryColumn != "" && toString(column["name"]) != spatialMeta.GeometryColumn {
		return false
	}
	spatialMeta.GeometryColumn = toString(column["name"])
	spatialMeta.SRID = int(toInt(column["srid"]))
	if geomType := toString(column["geometry_type"]); geomType != "" {
		spatialMeta.GeometryTypes = []string{geomType}
	}
	return true
}

func attributeFromSection(attrs models.JSONMap, section, key string) (interface{}, bool) {
	if attrs == nil {
		return nil, false
	}
	if value := commonJSON.Value(attrs, section, key); value != nil {
		return value, true
	}
	return nil, false
}

func mapAttributeFromSection(attrs models.JSONMap, section, key string) (map[string]interface{}, bool) {
	value, ok := attributeFromSection(attrs, section, key)
	if !ok {
		return nil, false
	}
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed, true
	case models.JSONMap:
		return map[string]interface{}(typed), true
	default:
		return nil, false
	}
}

func sliceAttributeFromSection(attrs models.JSONMap, section, key string) ([]interface{}, bool) {
	value, ok := attributeFromSection(attrs, section, key)
	if !ok {
		return nil, false
	}
	switch typed := value.(type) {
	case []interface{}:
		return typed, true
	case []map[string]interface{}:
		result := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			result = append(result, item)
		}
		return result, true
	default:
		return nil, false
	}
}

func spatialMetadataAttribute(attrs models.JSONMap) (map[string]interface{}, bool) {
	value := commonJSON.ValueFromSections(attrs, "spatial", "capabilities")
	if value == nil {
		return nil, false
	}
	switch spatial := value.(type) {
	case map[string]interface{}:
		return spatial, true
	case models.JSONMap:
		return map[string]interface{}(spatial), true
	default:
		return nil, false
	}
}

func toString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func toBool(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		lower := strings.ToLower(v)
		return lower == "true" || lower == "1" || lower == "yes"
	case float64:
		return v != 0
	case int:
		return v != 0
	case int64:
		return v != 0
	default:
		return false
	}
}

func toInt(value interface{}) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case string:
		var result int64
		fmt.Sscanf(v, "%d", &result)
		return result
	default:
		return 0
	}
}
