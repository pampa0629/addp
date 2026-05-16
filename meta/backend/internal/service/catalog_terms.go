package service

import "github.com/addp/common/engine/plugin"

func catalogItemTermForPlugin(p plugin.EnginePlugin, fallback string) string {
	if modelProvider, ok := p.(plugin.CatalogModelProvider); ok {
		if term := plugin.CatalogItemTerm(modelProvider.CatalogModel()); term != "" {
			return term
		}
	}
	return fallback
}

func namespaceLevelForPlugin(p plugin.EnginePlugin) (plugin.CatalogLevelSpec, bool) {
	model := catalogModelForPlugin(p)
	if model == nil {
		return plugin.CatalogLevelSpec{}, false
	}
	return plugin.CatalogNamespaceLevel(*model)
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
