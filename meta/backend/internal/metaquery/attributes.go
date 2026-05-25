package metaquery

import (
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
)

func FieldsFromMetaItem(item models.MetaItem) ([]datatype.FieldInfo, error) {
	return datatype.FieldInfosFromAttributes(commonJSON.Value(item.Attributes, "type_info.table", "fields")), nil
}

func SpatialMetadataFromItem(item models.MetaItem) (*models.SpatialMetadataResponse, error) {
	spatialMeta := &models.SpatialMetadataResponse{
		Fields: []datatype.FieldInfo{},
	}

	if spatialData, ok := spatialMetadataAttribute(item.Attributes); ok {
		if geomCol, ok := spatialData["primary_geometry_column"].(string); ok {
			spatialMeta.GeometryColumn = geomCol
		}
		applyGeometryColumns(spatialMeta, spatialData["geometry_columns"])
		if spatialMeta.SRID == 0 {
			spatialMeta.SRID = int(toInt(spatialData["srid"]))
		}

		if extentSRID, ok := spatialData["extent_srid"].(float64); ok {
			spatialMeta.ExtentSRID = int(extentSRID)
		}
		if extent := float64Slice(spatialData["extent"]); len(extent) == 4 {
			spatialMeta.Extent = extent
		}
	}

	if pkArray, ok := sliceAttributeFromSection(item.Attributes, "type_info.table", "primary_key"); ok && len(pkArray) > 0 {
		if pkStr := toString(pkArray[0]); pkStr != "" {
			spatialMeta.PrimaryKey = pkStr
		}
	}

	spatialMeta.Fields = datatype.FieldInfosFromAttributes(commonJSON.Value(item.Attributes, "type_info.table", "fields"))

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
	column, ok := rawMap(rawColumn)
	if !ok {
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

func rawMap(raw interface{}) (map[string]interface{}, bool) {
	switch typed := raw.(type) {
	case map[string]interface{}:
		return typed, true
	case models.JSONMap:
		return map[string]interface{}(typed), true
	default:
		return nil, false
	}
}

func float64Slice(raw interface{}) []float64 {
	switch values := raw.(type) {
	case []float64:
		return values
	case []interface{}:
		result := make([]float64, 0, len(values))
		for _, value := range values {
			switch v := value.(type) {
			case float64:
				result = append(result, v)
			case int:
				result = append(result, float64(v))
			case int64:
				result = append(result, float64(v))
			default:
				return nil
			}
		}
		return result
	default:
		return nil
	}
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
