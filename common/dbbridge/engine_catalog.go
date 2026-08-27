package dbbridge

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

// ListEngineCatalogChildren 列出指定 catalog 路径下的实时子节点。
func ListEngineCatalogChildren(ctx context.Context, engine *models.Engine, parent plugin.EngineCatalogPath, opts plugin.ListOptions) ([]plugin.EngineCatalogEntry, error) {
	pluginEngine := toPluginEngine(engine)

	p, err := plugin.Get(pluginEngine.EngineType)
	if err != nil {
		return nil, err
	}
	modelProvider, ok := p.(plugin.EngineCatalogModelProvider)
	if !ok {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorUnsupported,
			fmt.Errorf("plugin %s does not implement EngineCatalogModelProvider", pluginEngine.EngineType))
	}
	if len(parent.Segments) == 0 {
		return []plugin.EngineCatalogEntry{
			plugin.EngineCatalogRootEntry(modelProvider.EngineCatalogModel(), engine.ID, engine.Name),
		}, nil
	}
	if parent.Version == "" {
		parent.Version = plugin.EngineCatalogPathVersion
	}
	if parent.EngineID == 0 {
		parent.EngineID = pluginEngine.ID
	}
	catalogProvider, ok := p.(plugin.EngineCatalogProvider)
	if !ok {
		return nil, plugin.WrapEngineCatalogError(plugin.EngineCatalogErrorUnsupported,
			fmt.Errorf("plugin %s does not implement EngineCatalogProvider", pluginEngine.EngineType))
	}
	return catalogProvider.ListChildren(ctx, pluginEngine.ConnectionInfo, parent, opts)
}

// DescribeEngineCatalogFacts 描述 catalog leaf 的 engine-native facts。
func DescribeEngineCatalogFacts(ctx context.Context, engine *models.Engine, path plugin.EngineCatalogPath, opts plugin.EngineCatalogFactsOptions) (*plugin.EngineCatalogFacts, error) {
	return plugin.DescribeEngineCatalogFacts(ctx, toPluginEngine(engine), path, opts)
}

// CountEngineCatalogItemRows 获取 tabular catalog leaf 的行数。
func CountEngineCatalogItemRows(ctx context.Context, engine *models.Engine, path plugin.EngineCatalogPath) (int64, error) {
	return plugin.CountEngineCatalogItemRows(ctx, toPluginEngine(engine), path)
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
