package scanflow

import "github.com/addp/common/engine/plugin"

func EngineCatalogLeafTermForPlugin(p plugin.EnginePlugin, fallback string) string {
	if modelProvider, ok := p.(plugin.EngineCatalogModelProvider); ok {
		if term := plugin.EngineCatalogLeafTerm(modelProvider.EngineCatalogModel()); term != "" {
			return term
		}
	}
	return fallback
}

func FirstBusinessBranchLevelForPlugin(p plugin.EnginePlugin) (plugin.EngineCatalogLevelSpec, bool) {
	model := EngineCatalogModelForPlugin(p)
	if model == nil {
		return plugin.EngineCatalogLevelSpec{}, false
	}
	return plugin.EngineCatalogFirstBusinessBranch(*model)
}

func FirstBusinessBranchTermForPlugin(p plugin.EnginePlugin) string {
	if level, ok := FirstBusinessBranchLevelForPlugin(p); ok && level.Term != "" {
		return level.Term
	}
	return plugin.EngineCatalogTermDatabase
}

func NamespaceTermForPlugin(p plugin.EnginePlugin) string {
	return FirstBusinessBranchTermForPlugin(p)
}

func EngineCatalogRootTermForPlugin(p plugin.EnginePlugin) string {
	if model := EngineCatalogModelForPlugin(p); model != nil && model.RootTerm != "" {
		return model.RootTerm
	}
	return plugin.EngineCatalogTermServer
}

func EngineCatalogModelForPlugin(p plugin.EnginePlugin) *plugin.EngineCatalogModelSpec {
	if modelProvider, ok := p.(plugin.EngineCatalogModelProvider); ok {
		model := modelProvider.EngineCatalogModel()
		return &model
	}
	capabilities := p.Capabilities()
	if capabilities.Storage == nil || capabilities.Storage.CatalogModel == nil {
		return nil
	}
	return capabilities.Storage.CatalogModel
}
