package metacatalog

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
)

func RootBranchEntries(ctx context.Context, resource *commonModels.Engine, p plugin.EnginePlugin) ([]plugin.CatalogEntry, error) {
	catalogProvider, ok := p.(plugin.CatalogProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}

	rootPath, err := rootCatalogPath(resource.ID, p)
	if err != nil {
		return nil, err
	}
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), rootPath, plugin.ListOptions{})
	if err != nil {
		return nil, err
	}

	branches := make([]plugin.CatalogEntry, 0, len(nodes))
	for _, node := range nodes {
		if node.Role != plugin.CatalogRoleBranch {
			continue
		}
		branches = append(branches, node)
	}
	return branches, nil
}

func rootCatalogPath(engineID uint, p plugin.EnginePlugin) (plugin.CatalogPath, error) {
	modelProvider, ok := p.(plugin.CatalogModelProvider)
	if !ok {
		return plugin.CatalogPath{}, fmt.Errorf("engine %s does not implement CatalogModelProvider", p.Type())
	}
	return plugin.CatalogRootPath(modelProvider.CatalogModel(), engineID), nil
}
