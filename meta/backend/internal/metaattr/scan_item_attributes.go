package metaattr

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
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
	if standard := datatype.StandardGeometryType(value); standard != "" {
		return standard
	}
	return string(datatype.GeometryTypeGeometry)
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
	if count := firstPresent(input.Attributes, "estimated_documents"); count != nil {
		table["estimated_row_count"] = count
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

func ApplyDynamicSchemaStatistics(attrs models.JSONMap, rowCount, estimatedRowCount *int64, sizeBytes int64) {
	if attrs == nil {
		return
	}
	table := map[string]interface{}{}
	if rowCount != nil {
		table["row_count"] = *rowCount
	}
	if estimatedRowCount != nil {
		table["estimated_row_count"] = *estimatedRowCount
	}
	UpsertNested(attrs, "type_info", "table", table)
	SetStorage(attrs, "total_size", sizeBytes)
}

func ApplyTableItemAttributes(attrs models.JSONMap, tableInfo *datatype.TableInfo) {
	if attrs == nil {
		return
	}
	SetItem(attrs, "layout", "single")
	SetItem(attrs, "data_type", string(datatype.Table))
	if tableInfo == nil {
		return
	}
	UpsertNested(attrs, "type_info", "table", datatype.TableInfoPayload(tableInfo))
}

func ApplyGraphItemAttributes(attrs models.JSONMap, graphInfo *datatype.GraphInfo) {
	if attrs == nil {
		return
	}
	SetItem(attrs, "layout", "single")
	SetItem(attrs, "data_type", string(datatype.Graph))
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

func ApplyCatalogFactsCapabilities(attrs models.JSONMap, facts *plugin.CatalogFacts) {
	if attrs == nil || facts == nil {
		return
	}
	indexes := make([]map[string]interface{}, 0, len(facts.Indexes))
	for _, index := range facts.Indexes {
		indexes = append(indexes, map[string]interface{}{
			"name":       index.Name,
			"fields":     append([]string(nil), index.Fields...),
			"is_unique":  index.IsUnique,
			"index_type": index.IndexType,
		})
	}
	indexing := map[string]interface{}{}
	if len(indexes) > 0 {
		indexing["indexes"] = indexes
	}
	ReplaceCapabilityNamespace(attrs, "indexing", indexing)

	constraints := make([]map[string]interface{}, 0, len(facts.Constraints))
	for _, constraint := range facts.Constraints {
		item := map[string]interface{}{
			"name":            constraint.Name,
			"constraint_type": constraint.ConstraintType,
			"fields":          append([]string(nil), constraint.Fields...),
		}
		if constraint.ReferencedNamespace != "" {
			item["referenced_namespace"] = constraint.ReferencedNamespace
		}
		if constraint.ReferencedTable != "" {
			item["referenced_table"] = constraint.ReferencedTable
		}
		if len(constraint.ReferencedFields) > 0 {
			item["referenced_fields"] = append([]string(nil), constraint.ReferencedFields...)
		}
		constraints = append(constraints, item)
	}
	constraintAttrs := map[string]interface{}{}
	if len(constraints) > 0 {
		constraintAttrs["constraints"] = constraints
	}
	ReplaceCapabilityNamespace(attrs, "constraints", constraintAttrs)

	partitioning := map[string]interface{}{}
	if facts.Partitioning != nil {
		partitioning["strategy"] = facts.Partitioning.Strategy
		if len(facts.Partitioning.KeyFields) > 0 {
			partitioning["key_fields"] = append([]string(nil), facts.Partitioning.KeyFields...)
		}
		partitioning["subpartition_strategy"] = facts.Partitioning.SubpartitionStrategy
		if len(facts.Partitioning.SubpartitionKeyFields) > 0 {
			partitioning["subpartition_key_fields"] = append([]string(nil), facts.Partitioning.SubpartitionKeyFields...)
		}
		partitioning["partition_count"] = facts.Partitioning.PartitionCount
	}
	ReplaceCapabilityNamespace(attrs, "partitioning", partitioning)
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
		SetItem(attrs, "data_type", string(datatype.Table))
	case "graph":
		SetItem(attrs, "data_type", string(datatype.Graph))
	default:
		SetItem(attrs, "data_type", string(datatype.Unknown))
	}
}
