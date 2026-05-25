package metaattr

import (
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/meta/internal/models"
)

type DocumentCollectionAttributesInput struct {
	Fields     []datatype.FieldInfo
	Indexes    []IndexAttributesInput
	Stats      map[string]interface{}
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

func BuildDocumentCollectionAttributes(input DocumentCollectionAttributesInput) models.JSONMap {
	attrs := models.JSONMap{}
	applyDocumentCollectionMetadata(attrs, input)
	if len(input.Indexes) > 0 {
		UpsertNested(attrs, "capabilities", "indexing", map[string]interface{}{"indexes": IndexAttributes(input.Indexes)})
	}
	if len(input.Fields) > 0 {
		SetTableFields(attrs, input.Fields)
	}
	return attrs
}

func applyDocumentCollectionMetadata(attrs models.JSONMap, input DocumentCollectionAttributesInput) {
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
	if count := firstPresent(input.Stats, input.Attributes, "document_count", "total_documents"); count != nil {
		table["row_count"] = count
	}
	if v, ok := input.Stats["index_count"]; ok {
		statistics["index_count"] = v
	}
	if v, ok := input.Stats["avg_doc_size"]; ok {
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
