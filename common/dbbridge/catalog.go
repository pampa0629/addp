package dbbridge

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

// ListCatalogChildren 列出指定 catalog 路径下的实时子节点。
func ListCatalogChildren(ctx context.Context, engine *models.Engine, parent plugin.CatalogPath, opts plugin.ListOptions) ([]plugin.CatalogEntry, error) {
	pluginEngine := toPluginEngine(engine)

	p, err := plugin.Get(pluginEngine.EngineType)
	if err != nil {
		return nil, err
	}
	modelProvider, ok := p.(plugin.CatalogModelProvider)
	if !ok {
		return nil, plugin.WrapCatalogError(plugin.CatalogErrorUnsupported,
			fmt.Errorf("plugin %s does not implement CatalogModelProvider", pluginEngine.EngineType))
	}
	if len(parent.Segments) == 0 {
		return []plugin.CatalogEntry{
			plugin.CatalogRootEntry(modelProvider.CatalogModel(), engine.ID, engine.Name),
		}, nil
	}
	if parent.Version == "" {
		parent.Version = plugin.CatalogPathVersion
	}
	if parent.EngineID == 0 {
		parent.EngineID = pluginEngine.ID
	}
	catalogProvider, ok := p.(plugin.CatalogProvider)
	if !ok {
		return nil, plugin.WrapCatalogError(plugin.CatalogErrorUnsupported,
			fmt.Errorf("plugin %s does not implement CatalogProvider", pluginEngine.EngineType))
	}
	return catalogProvider.ListChildren(ctx, pluginEngine.ConnectionInfo, parent, opts)
}

// DescribeCatalogFacts 描述 catalog leaf 的 engine-native facts。
func DescribeCatalogFacts(ctx context.Context, engine *models.Engine, path plugin.CatalogPath, opts plugin.CatalogFactsOptions) (*plugin.CatalogFacts, error) {
	return plugin.DescribeCatalogFacts(ctx, toPluginEngine(engine), path, opts)
}

// CountCatalogItemRows 获取 tabular catalog leaf 的行数。
func CountCatalogItemRows(ctx context.Context, engine *models.Engine, path plugin.CatalogPath) (int64, error) {
	return plugin.CountCatalogItemRows(ctx, toPluginEngine(engine), path)
}

// ============ 统一查询执行 ============

// SupportsDirectQuery 检查引擎是否实现了非 SQL 原生查询运行时（MongoDB/Neo4j 等）
func SupportsDirectQuery(engineType string) bool {
	p, err := plugin.Get(engineType)
	if err != nil {
		return false
	}
	if _, ok := p.(plugin.SQLQueryRuntimeProvider); ok {
		return false
	}
	if qp, ok := p.(plugin.QueryRuntimeProvider); ok {
		if _, isSQLRuntime := qp.(plugin.SQLQueryRuntimeProvider); !isSQLRuntime {
			return true
		}
	}
	if _, ok := p.(plugin.GraphQueryProvider); ok {
		return true
	}
	return false
}
