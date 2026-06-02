package metaattr

import (
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/meta/internal/models"
)

type DynamicSchemaAttributesInput struct {
	Fields     []datatype.FieldInfo
	Indexes    []IndexAttributesInput
	Attributes map[string]interface{}
}

type IndexAttributesInput struct {
	Name      string
	Fields    []string
	IsUnique  bool
	IndexType string
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

func BuildDynamicSchemaAttributes(input DynamicSchemaAttributesInput) models.JSONMap {
	attrs := models.JSONMap{}
	applyDynamicSchemaMetadata(attrs, input)
	if len(input.Indexes) > 0 {
		UpsertNested(attrs, "capabilities", "indexing", map[string]interface{}{"indexes": IndexAttributes(input.Indexes)})
	}
	if len(input.Fields) > 0 {
		SetTableFields(attrs, input.Fields)
	}
	return attrs
}

func applyDynamicSchemaMetadata(attrs models.JSONMap, input DynamicSchemaAttributesInput) {
	if attrs == nil {
		return
	}

	table := map[string]interface{}{}
	statistics := map[string]interface{}{}
	if v, ok := input.Attributes["is_sampled"]; ok {
		statistics["is_sampled"] = v
	}
	if v, ok := input.Attributes["schema_type"]; ok {
		statistics["schema_type"] = v
	}
	if v, ok := input.Attributes["sample_size"]; ok {
		statistics["sample_size"] = v
	}
	if count := firstPresent(input.Attributes, "total_documents"); count != nil {
		table["row_count"] = count
	}
	if v, ok := input.Attributes["index_count"]; ok {
		statistics["index_count"] = v
	}
	if v, ok := input.Attributes["avg_record_size"]; ok {
		statistics["avg_record_size"] = v
	}

	UpsertNested(attrs, "type_info", "table", table)
	UpsertNested(attrs, "capabilities", "statistics", statistics)
}

func ApplyDynamicSchemaStatistics(attrs models.JSONMap, documentCount, sizeBytes int64) {
	if attrs == nil {
		return
	}
	UpsertNested(attrs, "type_info", "table", map[string]interface{}{"row_count": documentCount})
	SetStorage(attrs, "total_size", sizeBytes)
}

func ApplyTableItemAttributes(attrs models.JSONMap, tableInfo *datatype.TableInfo) {
	if attrs == nil {
		return
	}
	SetItem(attrs, "layout", "single")
	SetItem(attrs, "data_type", string(datatype.DataTypeTable))
	if tableInfo == nil {
		return
	}
	UpsertNested(attrs, "type_info", "table", datatype.TableInfoPayload(tableInfo))
	if tableInfo.RowCount != nil {
		rowCount := *tableInfo.RowCount
		UpsertNested(attrs, "type_info", "table", map[string]interface{}{"row_count": rowCount})
		UpsertNested(attrs, "capabilities", "statistics", map[string]interface{}{"row_count": rowCount})
	}
}

func ApplyGraphItemAttributes(attrs models.JSONMap, graphInfo *datatype.GraphInfo) {
	if attrs == nil {
		return
	}
	SetItem(attrs, "layout", "single")
	SetItem(attrs, "data_type", string(datatype.DataTypeGraph))
	if graphInfo == nil {
		return
	}
	UpsertNested(attrs, "type_info", "graph", datatype.GraphInfoPayload(graphInfo))
}

func IndexAttributes(indexes []IndexAttributesInput) []map[string]interface{} {
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

func firstPresent(values map[string]interface{}, keys ...string) interface{} {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func ApplyBranchLeafItemAttributes(attrs models.JSONMap, itemType string) {
	SetItem(attrs, "layout", "single")
	switch itemType {
	case "collection":
		SetItem(attrs, "data_type", string(datatype.DataTypeTable))
	case "graph":
		SetItem(attrs, "data_type", string(datatype.DataTypeGraph))
	default:
		SetItem(attrs, "data_type", string(datatype.DataTypeUnknown))
	}
}
