package service

import (
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
)

func catalogLeafTermForPlugin(p plugin.EnginePlugin, fallback string) string {
	if modelProvider, ok := p.(plugin.CatalogModelProvider); ok {
		if term := plugin.CatalogLeafTerm(modelProvider.CatalogModel()); term != "" {
			return term
		}
	}
	return fallback
}

func firstBusinessBranchLevelForPlugin(p plugin.EnginePlugin) (plugin.CatalogLevelSpec, bool) {
	model := catalogModelForPlugin(p)
	if model == nil {
		return plugin.CatalogLevelSpec{}, false
	}
	return plugin.CatalogFirstBusinessBranch(*model)
}

func firstBusinessBranchTermForPlugin(p plugin.EnginePlugin) string {
	if level, ok := firstBusinessBranchLevelForPlugin(p); ok && level.Term != "" {
		return level.Term
	}
	return plugin.CatalogTermDatabase
}

// namespaceTermForPlugin is only for tabular database/schema scan paths.
func namespaceTermForPlugin(p plugin.EnginePlugin) string {
	return firstBusinessBranchTermForPlugin(p)
}

func catalogRootTermForPlugin(p plugin.EnginePlugin) string {
	if model := catalogModelForPlugin(p); model != nil && model.RootTerm != "" {
		return model.RootTerm
	}
	return plugin.CatalogTermServer
}

func ensureCatalogRootNode(repo *metaRepo.ScanRepository, tenantID uint, resource *commonModels.Engine, p plugin.EnginePlugin) (*models.MetaNode, error) {
	return ensureCatalogRootNodeWithNativeName(repo, tenantID, resource, p, "")
}

func ensureCatalogRootNodeWithNativeName(repo *metaRepo.ScanRepository, tenantID uint, resource *commonModels.Engine, p plugin.EnginePlugin, nativeName string) (*models.MetaNode, error) {
	rootTerm := catalogRootTermForPlugin(p)
	fullName := ""
	attrs := models.JSONMap{
		"schema_version": 1,
		"catalog": map[string]interface{}{
			"root_term":           rootTerm,
			"display_name_source": "engine.name",
		},
	}
	if nativeName != "" && nativeName != resource.Name {
		attrs["catalog"].(map[string]interface{})["native_name"] = nativeName
	}
	return repo.UpsertNode(tenantID, resource.ID, nil, rootTerm, resource.Name, &fullName, attrs)
}

func catalogModelForPlugin(p plugin.EnginePlugin) *plugin.CatalogModelSpec {
	if modelProvider, ok := p.(plugin.CatalogModelProvider); ok {
		model := modelProvider.CatalogModel()
		return &model
	}
	capabilities := p.Capabilities()
	if capabilities.Storage == nil || capabilities.Storage.CatalogModel == nil {
		return nil
	}
	return capabilities.Storage.CatalogModel
}
