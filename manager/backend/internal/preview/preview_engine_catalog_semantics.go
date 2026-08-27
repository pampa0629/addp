package preview

import (
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/manager/internal/resourceutil"
)

func isObjectStorageType(resourceType string) bool {
	return resourceutil.ItemTermMatches(resourceType, plugin.EngineCatalogTermObject)
}

func isFileSystemType(resourceType string) bool {
	return resourceutil.ItemTermMatches(resourceType, plugin.EngineCatalogTermFile)
}

func IsContentCatalogEngine(engineType string) bool {
	return isObjectStorageType(engineType) || isFileSystemType(engineType)
}

func previewRequestCatalogLeafTerm(req *PreviewRequest) string {
	if req == nil {
		return ""
	}
	itemType := strings.ToLower(strings.TrimSpace(req.ItemType))
	switch itemType {
	case plugin.EngineCatalogTermObject, plugin.EngineCatalogTermFile:
		return itemType
	}
	if req.Engine == nil {
		return itemType
	}
	switch {
	case isObjectStorageType(req.Engine.EngineType):
		return plugin.EngineCatalogTermObject
	case isFileSystemType(req.Engine.EngineType):
		return plugin.EngineCatalogTermFile
	default:
		return itemType
	}
}
