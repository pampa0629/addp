package duckdb

import (
	"context"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	commonModels "github.com/addp/common/models"
)

// BuildObjectTableMap 构建对象存储表映射：engineName -> (schema.table 或 table) -> physicalPath
func BuildObjectTableMap(ctx context.Context, tenantID uint, engines []commonModels.Engine, metaClient *commonClient.MetaClient) map[string]map[string]string {
	result := make(map[string]map[string]string)
	if metaClient == nil {
		return result
	}
	metaClient = metaClient.WithTenantID(tenantID)
	for _, engine := range engines {
		if !IsObjectTableEngine(engine.EngineType) {
			continue
		}
		items, err := metaClient.ListEngineItems(ctx, engine.ID, "")
		if err != nil {
			continue
		}
		tables := make(map[string]string)
		for _, item := range items {
			descriptor, ok := ObjectTableDescriptorFromMetaItem(item)
			if !ok || descriptor.PhysicalPath == "" {
				continue
			}
			tables[item.Name] = descriptor.PhysicalPath
			tables[SanitizeName(item.Name)] = descriptor.PhysicalPath
			if item.FullName != "" && item.FullName != item.Name {
				tables[item.FullName] = descriptor.PhysicalPath
			}
		}
		if len(tables) > 0 {
			result[engine.Name] = tables
			if sn := SanitizeName(engine.Name); sn != engine.Name {
				result[sn] = tables
			}
		}
	}
	return result
}

func IsObjectTableItem(item commonModels.MetaItem) bool {
	_, ok := ObjectTableDescriptorFromMetaItem(item)
	return ok
}

// ObjectTableDescriptorFromMetaItem returns the table descriptor for file/object catalog leaves DuckDB can read directly.
func ObjectTableDescriptorFromMetaItem(item commonModels.MetaItem) (dataitem.ItemDescriptor, bool) {
	switch item.ItemType {
	case "object", "file":
	default:
		return dataitem.ItemDescriptor{}, false
	}
	descriptor := dataitem.DescriptorFromAttributes(item.Attributes)
	if descriptor.DataType != datatype.Table {
		return dataitem.ItemDescriptor{}, false
	}
	if descriptor.Format != "parquet" {
		return dataitem.ItemDescriptor{}, false
	}
	return descriptor, true
}
