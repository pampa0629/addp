package resourcetree

import (
	"fmt"
	"strings"
	"unicode/utf8"

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
	const maxLevels = 128
	if model.PathVersion != plugin.EngineCatalogPathVersion || model.RootTerm != plugin.EngineCatalogTermServer || len(model.Levels) == 0 || len(model.Levels) > maxLevels || len(loc.Path) > maxLevels {
		return plugin.EngineCatalogPath{}, fmt.Errorf("invalid or oversized server catalog model/path")
	}
	for i, level := range model.Levels {
		if level.Term == "" || len(level.Kinds) == 0 || (level.Role != plugin.EngineCatalogRoleBranch && level.Role != plugin.EngineCatalogRoleLeaf) || (level.Role == plugin.EngineCatalogRoleLeaf && i != len(model.Levels)-1) {
			return plugin.EngineCatalogPath{}, fmt.Errorf("invalid server catalog level")
		}
		for _, kind := range level.Kinds {
			if kind == "" {
				return plugin.EngineCatalogPath{}, fmt.Errorf("catalog kind is required")
			}
		}
	}
	if len(loc.Path) == 0 {
		if !isRootLocatorType(loc.Type) {
			return plugin.EngineCatalogPath{}, fmt.Errorf("catalog root locator requires root type")
		}
		return plugin.EngineCatalogRootPath(model, loc.EngineID), nil
	}
	for _, name := range loc.Path {
		if name == "" || len(name) > 1024 || !utf8.ValidString(name) || strings.ContainsRune(name, 0) {
			return plugin.EngineCatalogPath{}, fmt.Errorf("invalid catalog segment name")
		}
	}
	// Name-only locators can omit optional levels only when the full path and
	// endpoint type have exactly one interpretation. Memoization bounds optional
	// matching to O(levels * path length), including deliberately ambiguous input.
	type matchedLevel struct {
		index int
		next  *matchedLevel
	}
	type match struct {
		head  *matchedLevel
		count int
	}
	memo := map[[2]int]match{}
	var resolve func(int, int) match
	resolve = func(levelIndex, pathIndex int) match {
		key := [2]int{levelIndex, pathIndex}
		if found, ok := memo[key]; ok {
			return found
		}
		if levelIndex >= len(model.Levels) {
			return match{}
		}
		level := model.Levels[levelIndex]
		found := match{}
		if pathIndex == len(loc.Path)-1 {
			if resourceTypeMatchesLevel(loc.Type, level) {
				found = match{head: &matchedLevel{index: levelIndex}, count: 1}
			}
		} else if level.Role == plugin.EngineCatalogRoleBranch {
			child := resolve(levelIndex+1, pathIndex+1)
			if child.count > 0 {
				found = match{head: &matchedLevel{index: levelIndex, next: child.head}, count: child.count}
			}
		}
		if level.Optional {
			skipped := resolve(levelIndex+1, pathIndex)
			if skipped.count > 0 {
				if found.count == 0 {
					found.head = skipped.head
				}
				found.count += skipped.count
				if found.count > 2 {
					found.count = 2
				}
			}
		}
		memo[key] = found
		return found
	}
	found := resolve(0, 0)
	if found.count == 0 {
		return plugin.EngineCatalogPath{}, fmt.Errorf("locator does not match declared catalog levels")
	}
	if found.count != 1 {
		return plugin.EngineCatalogPath{}, fmt.Errorf("locator has ambiguous optional catalog levels")
	}
	path := plugin.EngineCatalogRootPath(model, loc.EngineID)
	for i, entry := 0, found.head; entry != nil; i, entry = i+1, entry.next {
		level := model.Levels[entry.index]
		kind := level.Kinds[0]
		if entry.next == nil {
			var err error
			kind, err = catalogKindForResourceType(loc.Type, level)
			if err != nil {
				return plugin.EngineCatalogPath{}, err
			}
		}
		path.Segments = append(path.Segments, plugin.EngineCatalogSegment{Term: level.Term, Kind: kind, Name: loc.Path[i]})
	}
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
