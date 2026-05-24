package metaattr

import (
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/models"
)

func BuildTableAttributes(schemaName string, fields []map[string]interface{}, tableMetadata map[string]interface{}, tableKind, tableComment string) models.JSONMap {
	table := map[string]interface{}{
		"fields":         fields,
		"table_metadata": tableMetadata,
	}
	if tableKind != "" {
		table["kind"] = tableKind
	}
	if tableComment != "" {
		table["comment"] = tableComment
	}
	attrs := models.JSONMap{
		"storage": map[string]interface{}{
			"schema_name": schemaName,
		},
		"type_info": map[string]interface{}{
			"table": table,
		},
	}
	return attrs
}

func BuildBasicTableAttributes(schemaName, tableKind, tableComment string) models.JSONMap {
	table := map[string]interface{}{}
	if tableKind != "" {
		table["kind"] = tableKind
	}
	if tableComment != "" {
		table["comment"] = tableComment
	}
	return models.JSONMap{
		"storage": map[string]interface{}{
			"schema_name": schemaName,
		},
		"type_info": map[string]interface{}{
			"table": table,
		},
	}
}

func UpsertTableNative(attrs models.JSONMap, native map[string]interface{}) {
	if attrs == nil || len(native) == 0 {
		return
	}
	UpsertNested(attrs, "type_info", "table", map[string]interface{}{"native": cloneInterfaceMap(native)})
}

func cloneInterfaceMap(values map[string]interface{}) map[string]interface{} {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func SpatialCapabilityFromMetadata(spatialMeta *models.SpatialMetadata) map[string]interface{} {
	if spatialMeta == nil || spatialMeta.GeometryColumn == "" {
		return map[string]interface{}{}
	}

	geometryColumn := map[string]interface{}{
		"name":          spatialMeta.GeometryColumn,
		"geometry_type": GeometryTypeFromSpatialMetadata(spatialMeta),
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

func GeometryTypeFromSpatialMetadata(spatialMeta *models.SpatialMetadata) string {
	if spatialMeta == nil || len(spatialMeta.GeometryTypes) == 0 {
		return "Geometry"
	}
	return NormalizeGeometryType(spatialMeta.GeometryTypes[0])
}

func NormalizeGeometryType(value string) string {
	switch normalizeGeometryToken(value) {
	case "point":
		return "Point"
	case "linestring":
		return "LineString"
	case "polygon":
		return "Polygon"
	case "multipoint":
		return "MultiPoint"
	case "multilinestring":
		return "MultiLineString"
	case "multipolygon":
		return "MultiPolygon"
	default:
		return "Geometry"
	}
}

func normalizeGeometryToken(value string) string {
	token := strings.ToLower(strings.TrimSpace(value))
	token = strings.TrimPrefix(token, "st_")
	token = strings.TrimPrefix(token, "st")
	token = strings.ReplaceAll(token, "_", "")
	token = strings.ReplaceAll(token, "-", "")
	token = strings.ReplaceAll(token, " ", "")
	return token
}

func BuildDocumentCollectionAttributes(itemMetadata *plugin.ItemMetadata) models.JSONMap {
	attrs := models.JSONMap{}
	if itemMetadata == nil {
		return attrs
	}

	applyDocumentCollectionMetadata(attrs, itemMetadata)
	if len(itemMetadata.Indexes) > 0 {
		UpsertNested(attrs, "type_info", "table", map[string]interface{}{"indexes": IndexAttributes(itemMetadata.Indexes)})
	}
	if len(itemMetadata.Fields) > 0 {
		UpsertNested(attrs, "type_info", "table", map[string]interface{}{"fields": DocumentFieldAttributes(itemMetadata.Fields)})
	}
	return attrs
}

func applyDocumentCollectionMetadata(attrs models.JSONMap, itemMetadata *plugin.ItemMetadata) {
	if itemMetadata == nil || attrs == nil {
		return
	}

	table := map[string]interface{}{}
	statistics := map[string]interface{}{}
	if v, ok := itemMetadata.Attributes["is_sampled"]; ok {
		table["is_sampled"] = v
	}
	if v, ok := itemMetadata.Attributes["schema_type"]; ok {
		table["schema_type"] = v
	}
	if v, ok := itemMetadata.Attributes["sample_size"]; ok {
		statistics["sample_size"] = v
	}
	if count := firstPresent(itemMetadata.Stats, itemMetadata.Attributes, "document_count", "total_documents"); count != nil {
		table["row_count"] = count
	}
	if v, ok := itemMetadata.Stats["index_count"]; ok {
		statistics["index_count"] = v
	}
	if v, ok := itemMetadata.Stats["avg_doc_size"]; ok {
		statistics["avg_doc_size"] = v
	}

	UpsertNested(attrs, "type_info", "table", table)
	UpsertNested(attrs, "capabilities", "statistics", statistics)
}

func ApplyDocumentCollectionStatistics(attrs models.JSONMap, documentCount, sizeBytes int64) {
	if attrs == nil {
		return
	}
	UpsertNested(attrs, "type_info", "table", map[string]interface{}{"row_count": documentCount})
	SetStorage(attrs, "total_size", sizeBytes)
}

func ApplyGraphItemAttributes(attrs models.JSONMap, itemType string, count int64, sourceAttributes map[string]interface{}) {
	if attrs == nil {
		return
	}

	graphAttrs := map[string]interface{}{}
	switch itemType {
	case "label":
		graphAttrs["label"] = true
		graphAttrs["node_count"] = count
	case "relationship":
		graphAttrs["relationship"] = true
		graphAttrs["edge_count"] = count
		if values := stringSliceAttribute(sourceAttributes["from_labels"]); len(values) > 0 {
			graphAttrs["from_labels"] = values
		}
		if values := stringSliceAttribute(sourceAttributes["to_labels"]); len(values) > 0 {
			graphAttrs["to_labels"] = values
		}
	default:
		graphAttrs["item_count"] = count
	}
	UpsertNested(attrs, "type_info", "graph", graphAttrs)
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

func stringSliceAttribute(raw interface{}) []string {
	switch values := raw.(type) {
	case []string:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if value != "" {
				result = append(result, value)
			}
		}
		return result
	case []interface{}:
		result := make([]string, 0, len(values))
		for _, value := range values {
			if str, ok := value.(string); ok && str != "" {
				result = append(result, str)
			}
		}
		return result
	default:
		return nil
	}
}

func firstPresent(primary, secondary map[string]interface{}, keys ...string) interface{} {
	for _, values := range []map[string]interface{}{primary, secondary} {
		for _, key := range keys {
			if value, ok := values[key]; ok {
				return value
			}
		}
	}
	return nil
}

func DocumentFieldAttributes(fields []datatype.FieldInfo) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(fields))
	for _, field := range fields {
		fieldAttr := map[string]interface{}{
			"name":           field.Name,
			"type":           string(field.Type),
			"nullable":       field.Nullable,
			"is_primary_key": field.PrimaryKey,
		}
		if field.NativeType != "" {
			fieldAttr["native_type"] = field.NativeType
		}

		result = append(result, fieldAttr)
	}
	return result
}

func ApplyNamespaceItemAttributes(attrs models.JSONMap, itemType string) {
	SetItem(attrs, "layout", string(dataitem.LayoutSingle))
	switch itemType {
	case "collection":
		SetItem(attrs, "data_type", string(dataitem.DataTypeTable))
	case "label", "relationship":
		SetItem(attrs, "data_type", string(dataitem.DataTypeGraph))
	default:
		SetItem(attrs, "data_type", string(dataitem.DataTypeUnknown))
	}
}
