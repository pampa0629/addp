package scanruntime

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/metaattr"
)

func dynamicSchemaAttributesInput(catalogFacts *plugin.CatalogFacts) metaattr.DynamicSchemaAttributesInput {
	if catalogFacts == nil {
		return metaattr.DynamicSchemaAttributesInput{}
	}
	indexes := make([]metaattr.IndexAttributesInput, 0, len(catalogFacts.Indexes))
	for _, index := range catalogFacts.Indexes {
		indexes = append(indexes, metaattr.IndexAttributesInput{
			Name:      index.Name,
			Fields:    append([]string(nil), index.Fields...),
			IsUnique:  index.IsUnique,
			IndexType: index.IndexType,
		})
	}
	return metaattr.DynamicSchemaAttributesInput{
		Fields:     dynamicSchemaFields(catalogFacts),
		Indexes:    indexes,
		Attributes: dynamicSchemaAttributes(catalogFacts),
	}
}

func dynamicSchemaAttributes(catalogFacts *plugin.CatalogFacts) map[string]interface{} {
	tableInfo := plugin.CatalogFactsTableInfo(catalogFacts)
	if tableInfo == nil {
		return nil
	}
	attrs := map[string]interface{}{
		"is_sampled": true,
	}
	if tableInfo.Native != nil {
		if v, ok := tableInfo.Native["schema_type"]; ok {
			attrs["schema_type"] = v
		}
		if v, ok := tableInfo.Native["sample_size"]; ok {
			attrs["sample_size"] = v
		}
		if v, ok := tableInfo.Native["index_count"]; ok {
			attrs["index_count"] = v
		}
		if v, ok := tableInfo.Native["avg_record_size"]; ok {
			attrs["avg_record_size"] = v
		}
		if v, ok := tableInfo.Native["database"]; ok {
			attrs["database"] = v
		}
		if v, ok := tableInfo.Native["collection"]; ok {
			attrs["collection"] = v
		}
	}
	if tableInfo.RowCount != nil {
		attrs["total_documents"] = *tableInfo.RowCount
	}
	return attrs
}

func dynamicSchemaFields(catalogFacts *plugin.CatalogFacts) []datatype.FieldInfo {
	tableInfo := plugin.CatalogFactsTableInfo(catalogFacts)
	if tableInfo == nil || len(tableInfo.Fields) == 0 {
		return nil
	}
	return append([]datatype.FieldInfo(nil), tableInfo.Fields...)
}
