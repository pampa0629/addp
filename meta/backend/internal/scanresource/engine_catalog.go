package scanresource

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
)

func RootBranchEntries(ctx context.Context, resource *commonModels.Engine, p plugin.EnginePlugin) ([]plugin.EngineCatalogEntry, error) {
	catalogProvider, ok := p.(plugin.EngineCatalogProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement EngineCatalogProvider", resource.EngineType)
	}

	rootPath, err := rootEngineCatalogPath(resource.ID, p)
	if err != nil {
		return nil, err
	}
	nodes, err := catalogProvider.ListChildren(ctx, plugin.ConnectionInfo(resource.ConnectionInfo), rootPath, plugin.ListOptions{})
	if err != nil {
		return nil, err
	}

	branches := make([]plugin.EngineCatalogEntry, 0, len(nodes))
	for _, node := range nodes {
		if node.Role != plugin.EngineCatalogRoleBranch {
			continue
		}
		branches = append(branches, node)
	}
	return branches, nil
}

func rootEngineCatalogPath(engineID uint, p plugin.EnginePlugin) (plugin.EngineCatalogPath, error) {
	modelProvider, ok := p.(plugin.EngineCatalogModelProvider)
	if !ok {
		return plugin.EngineCatalogPath{}, fmt.Errorf("engine %s does not implement EngineCatalogModelProvider", p.Type())
	}
	return plugin.EngineCatalogRootPath(modelProvider.EngineCatalogModel(), engineID), nil
}
