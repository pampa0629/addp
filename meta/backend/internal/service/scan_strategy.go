package service

import "github.com/addp/common/engine/plugin"

type catalogScanStrategy string

const (
	catalogScanTabular        catalogScanStrategy = "tabular"
	catalogScanNamespaceItems catalogScanStrategy = "namespace_items"
	catalogScanObject         catalogScanStrategy = "object_catalog"
	catalogScanFile           catalogScanStrategy = "file_catalog"
)

func catalogScanStrategyForPlugin(p plugin.EnginePlugin) (catalogScanStrategy, bool) {
	model := catalogModelForPlugin(p)
	if model == nil {
		return "", false
	}

	switch plugin.CatalogItemTerm(*model) {
	case plugin.CatalogTermTable:
		return catalogScanTabular, true
	case plugin.CatalogTermCollection, plugin.CatalogTermLabel:
		return catalogScanNamespaceItems, true
	case plugin.CatalogTermObject:
		return catalogScanObject, true
	case plugin.CatalogTermFile:
		return catalogScanFile, true
	default:
		return "", false
	}
}
