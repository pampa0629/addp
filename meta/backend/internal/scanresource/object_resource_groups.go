package scanresource

import (
	"sort"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/metaitem"
)

func UnclaimedObjectResources(group []StorageResource, skipPaths map[string]bool) []StorageResource {
	return unclaimedObjectResources(group, skipPaths)
}

func ObjectResourcesByParentPrefix(resources []StorageResource) map[string][]StorageResource {
	return objectResourcesByParentPrefix(resources)
}

func ObjectResourcesByPartitionRootPrefix(resources []StorageResource) map[string][]StorageResource {
	return objectResourcesByPartitionRootPrefix(resources)
}

func StorageResourcesToFileRefs(resources []StorageResource) []metaitem.StorageFileRef {
	return storageResourcesToFileRefs(resources)
}

func SplitObjectCompositeGroupKey(key string) (string, string) {
	return splitObjectCompositeGroupKey(key)
}

func objectResourcesByParentPrefix(resources []StorageResource) map[string][]StorageResource {
	groups := map[string][]StorageResource{}
	for _, resource := range resources {
		if resource.NodeType != plugin.EngineCatalogKindObject {
			continue
		}
		parent := strings.Trim(ParentObjectPath(resource.Path), "/")
		key := resource.RootName + "\x00" + parent
		groups[key] = append(groups[key], resource)
	}
	return groups
}

func objectResourcesByCompositePrefix(resources []StorageResource) map[string][]StorageResource {
	groups := objectResourcesByParentPrefix(resources)
	for key, group := range objectResourcesByPartitionRootPrefix(resources) {
		groups[key] = append(groups[key], group...)
	}
	return groups
}

func objectResourcesByPartitionRootPrefix(resources []StorageResource) map[string][]StorageResource {
	groups := map[string][]StorageResource{}
	for _, resource := range resources {
		if resource.NodeType != plugin.EngineCatalogKindObject {
			continue
		}
		prefix := partitionRootPrefix(resource.Path)
		if prefix == "" {
			continue
		}
		key := resource.RootName + "\x00" + prefix
		groups[key] = append(groups[key], resource)
	}
	return groups
}

func partitionRootPrefix(objectPath string) string {
	parent := strings.Trim(ParentObjectPath(objectPath), "/")
	if parent == "" {
		return ""
	}
	segments := splitObjectCatalogPathSegments(parent)
	for i, segment := range segments {
		if isPartitionPathSegment(segment) {
			if i == 0 {
				return ""
			}
			return strings.Join(segments[:i], "/")
		}
	}
	return ""
}

func isPartitionPathSegment(segment string) bool {
	segment = strings.TrimSpace(segment)
	return segment != "" && (strings.Contains(segment, "=") || strings.HasPrefix(segment, "_"))
}

func unclaimedObjectResources(group []StorageResource, skipPaths map[string]bool) []StorageResource {
	if len(group) == 0 || len(skipPaths) == 0 {
		return group
	}
	filtered := make([]StorageResource, 0, len(group))
	for _, resource := range group {
		if !skipPaths[resource.Path] {
			filtered = append(filtered, resource)
		}
	}
	return filtered
}

func storageResourcesToFileRefs(resources []StorageResource) []metaitem.StorageFileRef {
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].Path < resources[j].Path
	})
	files := make([]metaitem.StorageFileRef, 0, len(resources))
	for _, resource := range resources {
		entry := resource.StorageFileRef()
		entry.Path = strings.Trim(resource.Path, "/")
		files = append(files, entry)
	}
	return files
}

func splitObjectCompositeGroupKey(key string) (string, string) {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
