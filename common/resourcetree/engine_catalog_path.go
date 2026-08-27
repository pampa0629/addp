package resourcetree

import (
	"fmt"
	"strings"

	"github.com/addp/common/engine/plugin"
)

// EngineCatalogPathFromLocator converts ADDP ResourceLocator business paths
// into provider EngineCatalogPath values with an explicit structural root segment.
func EngineCatalogPathFromLocator(model plugin.EngineCatalogModelSpec, loc *ResourceLocator) (plugin.EngineCatalogPath, error) {
	if loc == nil {
		return plugin.EngineCatalogPath{}, fmt.Errorf("resource locator is required")
	}
	if loc.EngineID == 0 {
		return plugin.EngineCatalogPath{}, fmt.Errorf("resource locator engine_id is required")
	}
	switch strings.TrimSpace(model.RootTerm) {
	case plugin.EngineCatalogTermServer:
		return serverCatalogPathFromLocator(model, loc)
	case plugin.EngineCatalogTermService:
		if len(model.Levels) == 1 {
			return singleLevelServiceCatalogPathFromLocator(model, loc)
		}
		return objectCatalogPathFromLocator(loc)
	case plugin.EngineCatalogTermRoot:
		return fileCatalogPathFromLocator(loc)
	default:
		return plugin.EngineCatalogPath{}, fmt.Errorf("unsupported catalog root term: %s", model.RootTerm)
	}
}

func singleLevelServiceCatalogPathFromLocator(model plugin.EngineCatalogModelSpec, loc *ResourceLocator) (plugin.EngineCatalogPath, error) {
	if len(loc.Path) == 0 {
		if !isRootLocatorType(loc.Type) {
			return plugin.EngineCatalogPath{}, fmt.Errorf("service catalog root locator requires root type, got %s", loc.Type)
		}
		return plugin.EngineCatalogRootPath(model, loc.EngineID), nil
	}
	if len(loc.Path) != 1 {
		return plugin.EngineCatalogPath{}, fmt.Errorf("service catalog leaf requires exactly one business segment")
	}
	level := model.Levels[0]
	kind, err := catalogKindForResourceType(loc.Type, level)
	if err != nil {
		return plugin.EngineCatalogPath{}, err
	}
	name := strings.TrimSpace(loc.Path[0])
	if name == "" {
		return plugin.EngineCatalogPath{}, fmt.Errorf("service catalog leaf name is required")
	}
	path := plugin.EngineCatalogRootPath(model, loc.EngineID)
	path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: level.Term, Kind: kind, Name: name})
	return path, nil
}

func serverCatalogPathFromLocator(model plugin.EngineCatalogModelSpec, loc *ResourceLocator) (plugin.EngineCatalogPath, error) {
	if len(loc.Path) == 0 {
		if !isRootLocatorType(loc.Type) {
			return plugin.EngineCatalogPath{}, fmt.Errorf("catalog root locator requires root type, got %s", loc.Type)
		}
		return plugin.EngineCatalogRootPath(model, loc.EngineID), nil
	}
	if len(model.Levels) < 2 {
		return plugin.EngineCatalogPath{}, fmt.Errorf("server catalog model requires branch and leaf levels")
	}

	branchLevel := model.Levels[0]
	leafLevel := model.Levels[len(model.Levels)-1]
	branchName := strings.TrimSpace(loc.Path[0])
	if branchName == "" {
		return plugin.EngineCatalogPath{}, fmt.Errorf("catalog branch segment is required")
	}
	path := plugin.EngineCatalogRootPath(model, loc.EngineID)
	path.Segments = append(path.Segments, plugin.EngineCatalogSegment{
		Term: branchLevel.Term,
		Kind: firstCatalogKind(branchLevel, plugin.EngineCatalogKindNamespace),
		Name: branchName,
	})

	if len(loc.Path) == 1 {
		if !resourceTypeMatchesLevel(loc.Type, branchLevel) {
			return plugin.EngineCatalogPath{}, fmt.Errorf("catalog leaf path requires branch and %s segments", leafLevel.Term)
		}
		return path, nil
	}
	if len(loc.Path) > 2 {
		return plugin.EngineCatalogPath{}, fmt.Errorf("catalog path for %s requires exactly two business segments", leafLevel.Term)
	}
	leafName := strings.TrimSpace(loc.Path[1])
	if leafName == "" {
		return plugin.EngineCatalogPath{}, fmt.Errorf("catalog leaf segment is required")
	}
	leafKind, err := catalogKindForResourceType(loc.Type, leafLevel)
	if err != nil {
		return plugin.EngineCatalogPath{}, err
	}
	path.Segments = append(path.Segments, plugin.EngineCatalogSegment{
		Term: leafLevel.Term,
		Kind: leafKind,
		Name: leafName,
	})
	return path, nil
}

func objectCatalogPathFromLocator(loc *ResourceLocator) (plugin.EngineCatalogPath, error) {
	if len(loc.Path) == 0 {
		if !isRootLocatorType(loc.Type) {
			return plugin.EngineCatalogPath{}, fmt.Errorf("object catalog root locator requires root type, got %s", loc.Type)
		}
		return plugin.ObjectRootPath(loc.EngineID), nil
	}

	bucket := strings.TrimSpace(loc.Path[0])
	if bucket == "" {
		return plugin.EngineCatalogPath{}, fmt.Errorf("object catalog bucket segment is required")
	}
	switch loc.Type {
	case TypeBucket:
		if len(loc.Path) != 1 {
			return plugin.EngineCatalogPath{}, fmt.Errorf("bucket locator requires exactly one business segment")
		}
		return plugin.ObjectDirectoryPath(loc.EngineID, bucket, ""), nil
	case TypeDirectory, TypePrefix:
		return plugin.ObjectDirectoryPath(loc.EngineID, bucket, strings.Join(loc.Path[1:], "/")), nil
	case TypeObject:
		if len(loc.Path) < 2 {
			return plugin.EngineCatalogPath{}, fmt.Errorf("object locator requires bucket and object segments")
		}
		return plugin.ObjectItemPath(loc.EngineID, bucket, strings.Join(loc.Path[1:], "/")), nil
	default:
		return plugin.EngineCatalogPath{}, fmt.Errorf("unsupported object catalog locator type: %s", loc.Type)
	}
}

func fileCatalogPathFromLocator(loc *ResourceLocator) (plugin.EngineCatalogPath, error) {
	if len(loc.Path) == 0 {
		if !isRootLocatorType(loc.Type) {
			return plugin.EngineCatalogPath{}, fmt.Errorf("file catalog root locator requires root type, got %s", loc.Type)
		}
		return plugin.FileRootPath(loc.EngineID), nil
	}
	switch loc.Type {
	case TypeDirectory, TypeDir:
		return plugin.FileDirectoryPath(loc.EngineID, strings.Join(loc.Path, "/")), nil
	case TypeFile:
		return plugin.FileItemPath(loc.EngineID, strings.Join(loc.Path, "/")), nil
	default:
		return plugin.EngineCatalogPath{}, fmt.Errorf("unsupported file catalog locator type: %s", loc.Type)
	}
}

func isRootLocatorType(resourceType ResourceType) bool {
	return IsRootResourceType(resourceType)
}

func firstCatalogKind(level plugin.EngineCatalogLevelSpec, fallback string) string {
	if len(level.Kinds) > 0 && strings.TrimSpace(level.Kinds[0]) != "" {
		return level.Kinds[0]
	}
	return fallback
}

func resourceTypeMatchesLevel(resourceType ResourceType, level plugin.EngineCatalogLevelSpec) bool {
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

func catalogKindForResourceType(resourceType ResourceType, level plugin.EngineCatalogLevelSpec) (string, error) {
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
