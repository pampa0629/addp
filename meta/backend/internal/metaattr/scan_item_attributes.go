package metaattr

import (
	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/models"
)

func BuildTableAttributes(schemaName string, fields []map[string]interface{}, tableMetadata map[string]interface{}, tableType, tableComment string) models.JSONMap {
	attrs := models.JSONMap{
		"storage": map[string]interface{}{
			"schema_name": schemaName,
		},
		"type_info": map[string]interface{}{
			"table": map[string]interface{}{
				"fields":         fields,
				"table_metadata": tableMetadata,
				"table_type":     tableType,
				"table_comment":  tableComment,
			},
		},
	}
	return attrs
}

func BuildBasicTableAttributes(schemaName, tableType, tableComment string) models.JSONMap {
	return models.JSONMap{
		"storage": map[string]interface{}{
			"schema_name": schemaName,
		},
		"type_info": map[string]interface{}{
			"table": map[string]interface{}{
				"table_type":    tableType,
				"table_comment": tableComment,
			},
		},
	}
}

func SpatialCapabilityFromMetadata(spatialMeta *models.SpatialMetadata) map[string]interface{} {
	if spatialMeta == nil || spatialMeta.GeometryColumn == "" {
		return map[string]interface{}{}
	}

	geometryColumn := map[string]interface{}{
		"name":          spatialMeta.GeometryColumn,
		"geometry_type": "geometry",
	}
	if spatialMeta.SRID > 0 {
		geometryColumn["srid"] = spatialMeta.SRID
	}

	values := map[string]interface{}{
		"geometry_columns":        []map[string]interface{}{geometryColumn},
		"primary_geometry_column": spatialMeta.GeometryColumn,
		"has_spatial_index":       spatialMeta.HasSpatialIndex,
	}
	if len(spatialMeta.Extent) == 4 {
		values["extent"] = spatialMeta.Extent
		values["extent_srid"] = spatialMeta.ExtentSRID
	}
	return values
}

func BuildDocumentCollectionAttributes(itemMetadata *plugin.ItemMetadata) models.JSONMap {
	attrs := models.JSONMap{}
	if itemMetadata == nil {
		return attrs
	}

	for key, value := range itemMetadata.Attributes {
		attrs[key] = value
	}
	if len(itemMetadata.Indexes) > 0 {
		UpsertNested(attrs, "type_info", "table", map[string]interface{}{"indexes": IndexAttributes(itemMetadata.Indexes)})
	}
	if len(itemMetadata.Fields) > 0 {
		UpsertNested(attrs, "type_info", "table", map[string]interface{}{"fields": DocumentFieldAttributes(itemMetadata.Fields)})
	}
	return attrs
}

func IndexAttributes(indexes []plugin.IndexInfo) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(indexes))
	for _, idx := range indexes {
		result = append(result, map[string]interface{}{
			"name":       idx.Name,
			"fields":     idx.Fields,
			"is_unique":  idx.IsUnique,
			"index_type": idx.IndexType,
		})
	}
	return result
}

func DocumentFieldAttributes(fields []plugin.FieldInfo) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(fields))
	for _, field := range fields {
		fieldAttr := map[string]interface{}{
			"name":           field.Name,
			"type":           field.Type,
			"original_type":  field.NativeType,
			"nullable":       field.Nullable,
			"is_primary_key": field.PrimaryKey,
		}

		if field.Attributes != nil {
			if occurrenceRate, ok := field.Attributes["occurrence_rate"]; ok {
				fieldAttr["occurrence_rate"] = occurrenceRate
			}
		}
		result = append(result, fieldAttr)
	}
	return result
}

func ApplyNoSQLDataItemAttributes(attrs models.JSONMap, itemType string) {
	SetItem(attrs, "organization", string(dataitem.OrganizationSingle))
	switch itemType {
	case "collection":
		SetItem(attrs, "data_type", string(dataitem.DataTypeTable))
	case "label", "relationship":
		SetItem(attrs, "data_type", string(dataitem.DataTypeGraph))
	default:
		SetItem(attrs, "data_type", string(dataitem.DataTypeUnknown))
	}
}
