package preview

import (
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/catalogutil"
)

func isObjectStorageType(resourceType string) bool {
	return catalogutil.ItemTermMatches(resourceType, plugin.CatalogTermObject)
}

func isFileSystemType(resourceType string) bool {
	return catalogutil.ItemTermMatches(resourceType, plugin.CatalogTermFile)
}

func IsContentCatalogEngine(engineType string) bool {
	return isObjectStorageType(engineType) || isFileSystemType(engineType)
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
