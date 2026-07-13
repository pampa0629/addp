package resourcetree

import (
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
)

// ProviderCatalogPathFromLocator converts ADDP ResourceLocator business paths
// into provider CatalogPath values with an explicit structural root segment.
func ProviderCatalogPathFromLocator(model plugin.CatalogModelSpec, loc *ResourceLocator) (plugin.CatalogPath, error) {
	if loc == nil {
		return plugin.CatalogPath{}, fmt.Errorf("resource locator is required")
	}
	if loc.EngineID == 0 {
		return plugin.CatalogPath{}, fmt.Errorf("resource locator engine_id is required")
	}
	switch strings.TrimSpace(model.RootTerm) {
	case plugin.CatalogTermServer:
		return serverCatalogPathFromLocator(model, loc)
	case plugin.CatalogTermService:
		if len(model.Levels) == 1 {
			return singleLevelServiceCatalogPathFromLocator(model, loc)
		}
		return objectCatalogPathFromLocator(loc)
	case plugin.CatalogTermRoot:
		return fileCatalogPathFromLocator(loc)
	default:
		return plugin.CatalogPath{}, fmt.Errorf("unsupported catalog root term: %s", model.RootTerm)
	}
}

func singleLevelServiceCatalogPathFromLocator(model plugin.CatalogModelSpec, loc *ResourceLocator) (plugin.CatalogPath, error) {
	if len(loc.Path) == 0 {
		if !isRootLocatorType(loc.Type) {
			return plugin.CatalogPath{}, fmt.Errorf("service catalog root locator requires root type, got %s", loc.Type)
		}
		return plugin.CatalogRootPath(model, loc.EngineID), nil
	}
	if len(loc.Path) != 1 {
		return plugin.CatalogPath{}, fmt.Errorf("service catalog leaf requires exactly one business segment")
	}
	level := model.Levels[0]
	kind, err := catalogKindForResourceType(loc.Type, level)
	if err != nil {
		return plugin.CatalogPath{}, err
	}
	name := strings.TrimSpace(loc.Path[0])
	if name == "" {
		return plugin.CatalogPath{}, fmt.Errorf("service catalog leaf name is required")
	}
	path := plugin.CatalogRootPath(model, loc.EngineID)
	path.Segments = append(path.Segments, plugin.CatalogSegment{Term: level.Term, Kind: kind, Name: name})
	return path, nil
}

func serverCatalogPathFromLocator(model plugin.CatalogModelSpec, loc *ResourceLocator) (plugin.CatalogPath, error) {
	if len(loc.Path) == 0 {
		if !isRootLocatorType(loc.Type) {
			return plugin.CatalogPath{}, fmt.Errorf("catalog root locator requires root type, got %s", loc.Type)
		}
		return plugin.CatalogRootPath(model, loc.EngineID), nil
	}
	if len(model.Levels) < 2 {
		return plugin.CatalogPath{}, fmt.Errorf("server catalog model requires branch and leaf levels")
	}

	branchLevel := model.Levels[0]
	leafLevel := model.Levels[len(model.Levels)-1]
	branchName := strings.TrimSpace(loc.Path[0])
	if branchName == "" {
		return plugin.CatalogPath{}, fmt.Errorf("catalog branch segment is required")
	}
	path := plugin.CatalogRootPath(model, loc.EngineID)
	path.Segments = append(path.Segments, plugin.CatalogSegment{
		Term: branchLevel.Term,
		Kind: firstCatalogKind(branchLevel, plugin.CatalogKindNamespace),
		Name: branchName,
	})

	if len(loc.Path) == 1 {
		if !resourceTypeMatchesLevel(loc.Type, branchLevel) {
			return plugin.CatalogPath{}, fmt.Errorf("catalog leaf path requires branch and %s segments", leafLevel.Term)
		}
		return path, nil
	}
	if len(loc.Path) > 2 {
		return plugin.CatalogPath{}, fmt.Errorf("catalog path for %s requires exactly two business segments", leafLevel.Term)
	}
	leafName := strings.TrimSpace(loc.Path[1])
	if leafName == "" {
		return plugin.CatalogPath{}, fmt.Errorf("catalog leaf segment is required")
	}
	leafKind, err := catalogKindForResourceType(loc.Type, leafLevel)
	if err != nil {
		return plugin.CatalogPath{}, err
	}
	path.Segments = append(path.Segments, plugin.CatalogSegment{
		Term: leafLevel.Term,
		Kind: leafKind,
		Name: leafName,
	})
	return path, nil
}

func objectCatalogPathFromLocator(loc *ResourceLocator) (plugin.CatalogPath, error) {
	if len(loc.Path) == 0 {
		if !isRootLocatorType(loc.Type) {
			return plugin.CatalogPath{}, fmt.Errorf("object catalog root locator requires root type, got %s", loc.Type)
		}
		return plugin.ObjectRootPath(loc.EngineID), nil
	}

	bucket := strings.TrimSpace(loc.Path[0])
	if bucket == "" {
		return plugin.CatalogPath{}, fmt.Errorf("object catalog bucket segment is required")
	}
	switch loc.Type {
	case TypeBucket:
		if len(loc.Path) != 1 {
			return plugin.CatalogPath{}, fmt.Errorf("bucket locator requires exactly one business segment")
		}
		return plugin.ObjectDirectoryPath(loc.EngineID, bucket, ""), nil
	case TypeDirectory, TypePrefix:
		return plugin.ObjectDirectoryPath(loc.EngineID, bucket, strings.Join(loc.Path[1:], "/")), nil
	case TypeObject:
		if len(loc.Path) < 2 {
			return plugin.CatalogPath{}, fmt.Errorf("object locator requires bucket and object segments")
		}
		return plugin.ObjectItemPath(loc.EngineID, bucket, strings.Join(loc.Path[1:], "/")), nil
	default:
		return plugin.CatalogPath{}, fmt.Errorf("unsupported object catalog locator type: %s", loc.Type)
	}
}

func fileCatalogPathFromLocator(loc *ResourceLocator) (plugin.CatalogPath, error) {
	if len(loc.Path) == 0 {
		if !isRootLocatorType(loc.Type) {
			return plugin.CatalogPath{}, fmt.Errorf("file catalog root locator requires root type, got %s", loc.Type)
		}
		return plugin.FileRootPath(loc.EngineID), nil
	}
	switch loc.Type {
	case TypeDirectory, TypeDir:
		return plugin.FileDirectoryPath(loc.EngineID, strings.Join(loc.Path, "/")), nil
	case TypeFile:
		return plugin.FileItemPath(loc.EngineID, strings.Join(loc.Path, "/")), nil
	default:
		return plugin.CatalogPath{}, fmt.Errorf("unsupported file catalog locator type: %s", loc.Type)
	}
}

func isRootLocatorType(resourceType ResourceType) bool {
	return IsRootResourceType(resourceType)
}

func firstCatalogKind(level plugin.CatalogLevelSpec, fallback string) string {
	if len(level.Kinds) > 0 && strings.TrimSpace(level.Kinds[0]) != "" {
		return level.Kinds[0]
	}
	return fallback
}

func resourceTypeMatchesLevel(resourceType ResourceType, level plugin.CatalogLevelSpec) bool {
	value := strings.TrimSpace(string(resourceType))
	if value == "" {
		return false
	}
	if value == level.Term {
		return true
	}
	for _, kind := range level.Kinds {
		if value == kind {
			return true
		}
	}
	return false
}

func catalogKindForResourceType(resourceType ResourceType, level plugin.CatalogLevelSpec) (string, error) {
	value := strings.TrimSpace(string(resourceType))
	if value == "" {
		return "", fmt.Errorf("catalog leaf locator type is required")
	}
	for _, kind := range level.Kinds {
		if value == kind {
			return kind, nil
		}
	}
	if value == level.Term {
		return firstCatalogKind(level, level.Term), nil
	}
	return "", fmt.Errorf("locator type %s does not match catalog leaf %s", resourceType, level.Term)
}
