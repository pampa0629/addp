package scanflow

import "github.com/addp/common/engine/plugin"

func CatalogLeafTermForPlugin(p plugin.EnginePlugin, fallback string) string {
	if modelProvider, ok := p.(plugin.CatalogModelProvider); ok {
		if term := plugin.CatalogLeafTerm(modelProvider.CatalogModel()); term != "" {
			return term
		}
	}
	return fallback
}

func FirstBusinessBranchLevelForPlugin(p plugin.EnginePlugin) (plugin.CatalogLevelSpec, bool) {
	model := CatalogModelForPlugin(p)
	if model == nil {
		return plugin.CatalogLevelSpec{}, false
	}
	return plugin.CatalogFirstBusinessBranch(*model)
}

func FirstBusinessBranchTermForPlugin(p plugin.EnginePlugin) string {
	if level, ok := FirstBusinessBranchLevelForPlugin(p); ok && level.Term != "" {
		return level.Term
	}
	return plugin.CatalogTermDatabase
}

func NamespaceTermForPlugin(p plugin.EnginePlugin) string {
	return FirstBusinessBranchTermForPlugin(p)
}

func CatalogRootTermForPlugin(p plugin.EnginePlugin) string {
	if model := CatalogModelForPlugin(p); model != nil && model.RootTerm != "" {
		return model.RootTerm
	}
	return plugin.CatalogTermServer
}

func CatalogModelForPlugin(p plugin.EnginePlugin) *plugin.CatalogModelSpec {
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
