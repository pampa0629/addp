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

	spatialColumns := spatialColumnsFromItem(item)
	fieldInfos := make([]commonModels.FieldInfo, 0, len(fieldsList))
	for _, fieldData := range fieldsList {
		fieldMap, ok := fieldData.(map[string]interface{})
		if !ok {
			continue
		}

		fieldName := toString(fieldMap["name"])
		dataType := firstString(fieldMap["data_type"], fieldMap["type"], fieldMap["original_type"])
		isSpatial := toBool(fieldMap["is_spatial"]) || isGeometryType(dataType)
		geometryType := toString(fieldMap["geometry_type"])
		srid := int(toInt(fieldMap["srid"]))
		if column, ok := spatialColumns[fieldName]; ok {
			isSpatial = true
			if column.GeometryType != "" {
				geometryType = column.GeometryType
			}
			if column.SRID > 0 {
				srid = column.SRID
			}
		}
		fieldInfos = append(fieldInfos, commonModels.FieldInfo{
			Name:         fieldName,
			DataType:     dataType,
			IsPrimaryKey: toBool(fieldMap["is_primary_key"]),
			IsNullable:   toBool(fieldMap["is_nullable"]),
			Comment:      toString(fieldMap["comment"]),
			IsSpatial:    isSpatial,
			GeometryType: geometryType,
			SRID:         srid,
		})
	}

	return fieldInfos, nil
}

type spatialColumnInfo struct {
	GeometryType string
	SRID         int
}

func spatialColumnsFromItem(item models.MetaItem) map[string]spatialColumnInfo {
	spatialData, ok := spatialMetadataAttribute(item.Attributes)
	if !ok {
		return nil
	}
	columns := map[string]spatialColumnInfo{}
	addSpatialColumn := func(raw interface{}) {
		column, ok := rawMap(raw)
		if !ok {
			return
		}
		name := toString(column["name"])
		if name == "" {
			return
		}
		columns[name] = spatialColumnInfo{
			GeometryType: toString(column["geometry_type"]),
			SRID:         int(toInt(column["srid"])),
		}
	}
	switch rawColumns := spatialData["geometry_columns"].(type) {
	case []interface{}:
		for _, rawColumn := range rawColumns {
			addSpatialColumn(rawColumn)
		}
	case []map[string]interface{}:
		for _, rawColumn := range rawColumns {
			addSpatialColumn(rawColumn)
		}
	}
	if len(columns) == 0 {
		return nil
	}
	return columns
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

		if extentSRID, ok := spatialData["extent_srid"].(float64); ok {
			spatialMeta.ExtentSRID = int(extentSRID)
		}
		if extent := float64Slice(spatialData["extent"]); len(extent) == 4 {
			spatialMeta.Extent = extent
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

func firstString(values ...interface{}) string {
	for _, value := range values {
		if text := toString(value); strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
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
