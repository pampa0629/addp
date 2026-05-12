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
