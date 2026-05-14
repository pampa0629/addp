package service

import (
	"strings"

	"github.com/addp/common/engine/plugin"
)

func isObjectStorageType(resourceType string) bool {
	return catalogItemTermMatches(resourceType, plugin.CatalogTermObject)
}

func isFileSystemType(resourceType string) bool {
	return catalogItemTermMatches(resourceType, plugin.CatalogTermFile)
}

func previewRequestCatalogItemTerm(req *PreviewRequest) string {
	if req == nil {
		return ""
	}
	itemType := strings.ToLower(strings.TrimSpace(req.ItemType))
	switch itemType {
	case plugin.CatalogTermObject, plugin.CatalogTermFile:
		return itemType
	}
	if req.Engine == nil {
		return itemType
	}
	switch {
	case isObjectStorageType(req.Engine.EngineType):
		return plugin.CatalogTermObject
	case isFileSystemType(req.Engine.EngineType):
		return plugin.CatalogTermFile
	default:
		return itemType
	}
}

func catalogItemTermMatches(engineType, term string) bool {
	if strings.TrimSpace(term) == "" {
		return false
	}
	p, err := plugin.Get(strings.TrimSpace(engineType))
	if err != nil {
		return false
	}
	if modelProvider, ok := p.(plugin.CatalogModelProvider); ok {
		return plugin.CatalogItemTerm(modelProvider.CatalogModel()) == term
	}
	capabilities := p.Capabilities()
	if capabilities.Storage == nil || capabilities.Storage.CatalogModel == nil {
		return false
	}
	return plugin.CatalogItemTerm(*capabilities.Storage.CatalogModel) == term
}
