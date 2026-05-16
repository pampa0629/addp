package metacatalog

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
)

func FileCatalogRootPaths(ctx context.Context, resource *commonModels.Engine, p plugin.EnginePlugin) ([]string, error) {
	catalogProvider, ok := p.(plugin.CatalogProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}

	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), rootCatalogPath(resource.ID), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if raw := commonJSON.String(node.Attributes, "storage", "path"); raw != "" {
			paths = append(paths, raw)
			continue
		}
		paths = append(paths, node.Path.StringPath())
	}
	return paths, nil
}

func NamespaceInfos(ctx context.Context, resource *commonModels.Engine, p plugin.EnginePlugin) ([]plugin.SchemaInfo, error) {
	catalogProvider, ok := p.(plugin.CatalogProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}

	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), rootCatalogPath(resource.ID), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}

	schemas := make([]plugin.SchemaInfo, 0, len(nodes))
	for _, node := range nodes {
		tableCount := 0
		if count, ok := int64Stat(node.Stats, "table_count"); ok {
			tableCount = int(count)
		}
		schemas = append(schemas, plugin.SchemaInfo{
			Name:       node.Name,
			TableCount: tableCount,
		})
	}
	return schemas, nil
}

func NamespaceDatabaseInfos(ctx context.Context, resource *commonModels.Engine, catalogProvider plugin.CatalogProvider) ([]plugin.DatabaseInfo, error) {
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), rootCatalogPath(resource.ID), plugin.ListOptions{})
	if err != nil {
		return nil, err
	}

	databases := make([]plugin.DatabaseInfo, 0, len(nodes))
	for _, node := range nodes {
		if !node.IsContainer {
			continue
		}
		sizeBytes, _ := int64Stat(node.Stats, "size_bytes")
		databases = append(databases, plugin.DatabaseInfo{
			Name:      node.Name,
			SizeBytes: sizeBytes,
		})
	}
	return databases, nil
}

func rootCatalogPath(engineID uint) plugin.CatalogPath {
	return plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
	}
}

func int64Stat(stats map[string]interface{}, key string) (int64, bool) {
	if stats == nil {
		return 0, false
	}
	raw, ok := stats[key]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case uint:
		return int64(v), true
	case float64:
		return int64(v), true
	default:
		return 0, false
	}
}
