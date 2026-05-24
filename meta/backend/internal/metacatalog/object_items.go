package metacatalog

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	_ "github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

type ObjectCatalogCompositeItem struct {
	Bucket string
	Prefix string
	Item   *metaitem.DetectedItem
	Claims metaitem.ResourceClaimSet
}

type ObjectCatalogCompositeDetectionError struct {
	Bucket string
	Prefix string
	Err    error
}

func DetectObjectCatalogCompositeItems(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	resources []StorageResource,
) (map[string]bool, []ObjectCatalogCompositeItem, []ObjectCatalogCompositeDetectionError) {
	skipPaths := map[string]bool{}
	if contentReader == nil {
		return skipPaths, nil, nil
	}

	groups := objectResourcesByCompositePrefix(resources)
	items := make([]ObjectCatalogCompositeItem, 0)
	warnings := make([]ObjectCatalogCompositeDetectionError, 0)
	groupKeys := make([]string, 0, len(groups))
	for groupKey := range groups {
		groupKeys = append(groupKeys, groupKey)
	}
	sort.Slice(groupKeys, func(i, j int) bool {
		_, leftPrefix := splitObjectCompositeGroupKey(groupKeys[i])
		_, rightPrefix := splitObjectCompositeGroupKey(groupKeys[j])
		leftDepth := strings.Count(strings.Trim(leftPrefix, "/"), "/")
		rightDepth := strings.Count(strings.Trim(rightPrefix, "/"), "/")
		if leftDepth != rightDepth {
			return leftDepth > rightDepth
		}
		return groupKeys[i] < groupKeys[j]
	})

	for _, groupKey := range groupKeys {
		group := unclaimedObjectResources(groups[groupKey], skipPaths)
		if len(group) < 2 {
			continue
		}
		bucket, prefix := splitObjectCompositeGroupKey(groupKey)
		if prefix == "" {
			continue
		}
		files := storageResourcesToFileEntries(group)
		detection, err := metaitem.ResolveItems(ctx, metaitem.DirectoryResolveInput{
			ContentReader:  contentReader,
			ConnInfo:       connInfo,
			EngineID:       engineID,
			CatalogPathFor: plugin.ObjectItemPathForBucket(engineID, bucket),
			DirPath:        prefix,
			Files:          files,
		})
		if err != nil {
			warnings = append(warnings, ObjectCatalogCompositeDetectionError{
				Bucket: bucket,
				Prefix: prefix,
				Err:    err,
			})
			continue
		}
		if detection == nil || len(detection.Items) == 0 {
			continue
		}
		for path, claimed := range detection.Claims {
			if claimed {
				skipPaths[ObjectPathFromClaim(bucket, path)] = true
			}
		}
		for _, detected := range detection.Items {
			if detected == nil {
				continue
			}
			items = append(items, ObjectCatalogCompositeItem{
				Bucket: bucket,
				Prefix: prefix,
				Item:   detected,
				Claims: detection.Claims,
			})
		}
	}
	return skipPaths, items, warnings
}

func InferObjectCatalogDataItem(resource StorageResource, objectName string) *metaitem.DetectedItem {
	physicalPath := resource.FullPath
	return metaitem.InferSingleResource(metaitem.SingleResourceInput{
		Name:   objectName,
		Path:   physicalPath,
		Size:   resource.SizeBytes,
		Format: resource.Format,
	})
}

func ObjectCatalogCompositeName(composite ObjectCatalogCompositeItem) (name, objectPath string) {
	if composite.Item != nil {
		switch composite.Item.Layout {
		case dataitem.LayoutSingle, dataitem.LayoutMulti:
			if composite.Item.EntryPath != "" {
				objectPath = ObjectPathFromClaim(composite.Bucket, composite.Item.EntryPath)
				if objectPath != "" {
					return path.Base(objectPath), objectPath
				}
			}
		}
	}
	objectPath = strings.Trim(composite.Prefix, "/")
	name = path.Base(objectPath)
	if name == "" {
		name = "dataset"
		objectPath = name
	}
	return name, objectPath
}

func ObjectCatalogCompositeMode(item *metaitem.DetectedItem) string {
	if item == nil {
		return "directory"
	}
	switch item.Layout {
	case dataitem.LayoutSingle:
		return "single"
	case dataitem.LayoutMulti:
		return "multi"
	case dataitem.LayoutWhole:
		return "whole"
	default:
		return "directory"
	}
}

type ObjectCatalogRelativePathPlan struct {
	Segments   []string
	ExactBase  bool
	SkipReason string
}

type ObjectCatalogSingleItemPlan struct {
	ItemType    string
	ItemName    string
	ObjectName  string
	FullName    string
	Fingerprint string
	Attributes  models.JSONMap
	DataItem    *metaitem.DetectedItem
}

type ObjectCatalogCompositeItemPlan struct {
	ItemType    string
	ItemName    string
	ObjectPath  string
	ParentPath  string
	FullName    string
	Fingerprint string
	SizeBytes   int64
	Attributes  models.JSONMap
}

func PlanObjectCatalogRelativePath(trimmedPath, scanPathPrefix string) ObjectCatalogRelativePathPlan {
	trimmedPath = strings.Trim(trimmedPath, "/")
	scanPathPrefix = strings.Trim(scanPathPrefix, "/")

	switch {
	case scanPathPrefix != "" && trimmedPath == scanPathPrefix:
		return ObjectCatalogRelativePathPlan{
			ExactBase:  true,
			SkipReason: "trimmed==scanPathPrefix",
		}
	case scanPathPrefix != "" && strings.HasPrefix(trimmedPath, scanPathPrefix+"/"):
		remaining := strings.TrimPrefix(trimmedPath, scanPathPrefix+"/")
		return ObjectCatalogRelativePathPlan{
			Segments:   splitObjectCatalogPathSegments(remaining),
			SkipReason: "trimmed以scanPathPrefix/开头，去掉前缀",
		}
	case trimmedPath != "":
		return ObjectCatalogRelativePathPlan{
			Segments: splitObjectCatalogPathSegments(trimmedPath),
		}
	default:
		return ObjectCatalogRelativePathPlan{
			SkipReason: "空路径",
		}
	}
}

func PlanObjectCatalogSingleItem(engineID uint, resource StorageResource, trimmedPath string, itemType string) ObjectCatalogSingleItemPlan {
	objectName := path.Base(strings.Trim(resource.Path, "/"))
	if objectName == "" {
		objectName = strings.Trim(trimmedPath, "/")
	}
	objectName = strings.Trim(objectName, "/")
	if objectName == "" {
		objectName = fmt.Sprintf("object_%d", resource.SizeBytes)
	}

	dataItem := InferObjectCatalogDataItem(resource, objectName)

	dir, name := commonModels.SplitObjectPath(resource.Path)
	attrs := models.JSONMap{
		"storage": map[string]interface{}{
			"bucket":       resource.RootName,
			"path":         dir,
			"name":         name,
			"total_size":   resource.SizeBytes,
			"object_count": resource.ObjectCount,
		},
	}
	if resource.Format != "" {
		metaattr.SetStorage(attrs, "file_type", resource.Format)
	}
	if resource.LastModified != nil {
		metaattr.SetStorage(attrs, "last_modified_at", resource.LastModified)
	}
	metaattr.MergeDataItemAttributes(attrs, dataItem)
	ApplyContainerSummary(attrs, dataItem)

	fullName := commonModels.JoinObjectPath(resource.RootName, dir, name)
	return ObjectCatalogSingleItemPlan{
		ItemType:    itemType,
		ItemName:    objectName,
		ObjectName:  objectName,
		FullName:    fullName,
		Fingerprint: commonModels.GenerateItemFingerprint(engineID, fullName),
		Attributes:  attrs,
		DataItem:    dataItem,
	}
}

func PlanObjectCatalogCompositeItem(engineID uint, composite ObjectCatalogCompositeItem, itemType string) (ObjectCatalogCompositeItemPlan, bool) {
	if composite.Item == nil {
		return ObjectCatalogCompositeItemPlan{}, false
	}

	itemName, objectPath := ObjectCatalogCompositeName(composite)
	parentPath := ParentObjectPath(objectPath)
	fullName := commonModels.JoinObjectPath(composite.Bucket, parentPath, itemName)

	attrs := models.JSONMap(metaattr.BuildAttributes(composite.Item))
	if len(composite.Item.Fields) > 0 {
		metaattr.SetSchemaFields(attrs, metaattr.FieldAttributes(composite.Item.Fields))
	}
	metaattr.SetStorage(attrs, "bucket", composite.Bucket)
	metaattr.SetStorage(attrs, "path", parentPath)
	metaattr.SetStorage(attrs, "name", itemName)
	metaattr.SetItem(attrs, "mode", ObjectCatalogCompositeMode(composite.Item))

	return ObjectCatalogCompositeItemPlan{
		ItemType:    itemType,
		ItemName:    itemName,
		ObjectPath:  objectPath,
		ParentPath:  parentPath,
		FullName:    fullName,
		Fingerprint: commonModels.GenerateItemFingerprint(engineID, fullName),
		SizeBytes:   composite.Item.Size(),
		Attributes:  attrs,
	}, true
}

func UnclaimedObjectResources(group []StorageResource, skipPaths map[string]bool) []StorageResource {
	return unclaimedObjectResources(group, skipPaths)
}

func ObjectResourcesByParentPrefix(resources []StorageResource) map[string][]StorageResource {
	return objectResourcesByParentPrefix(resources)
}

func ParentObjectPath(pathValue string) string {
	dir := path.Dir(strings.Trim(pathValue, "/"))
	if dir == "." || dir == "/" {
		return ""
	}
	return strings.Trim(dir, "/") + "/"
}

func ObjectPathFromClaim(bucket, claimPath string) string {
	trimmed := strings.Trim(claimPath, "/")
	prefix := strings.Trim(bucket, "/") + "/"
	return strings.TrimPrefix(trimmed, prefix)
}

func objectResourcesByParentPrefix(resources []StorageResource) map[string][]StorageResource {
	groups := map[string][]StorageResource{}
	for _, resource := range resources {
		if resource.NodeType != plugin.CatalogKindObject {
			continue
		}
		parent := strings.Trim(ParentObjectPath(resource.Path), "/")
		if parent == "" {
			continue
		}
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
		if resource.NodeType != plugin.CatalogKindObject {
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

func storageResourcesToFileEntries(resources []StorageResource) []plugin.FileEntry {
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].Path < resources[j].Path
	})
	files := make([]plugin.FileEntry, 0, len(resources))
	for _, resource := range resources {
		entry := resource.FileEntry()
		entry.Path = strings.Trim(resource.Path, "/")
		files = append(files, entry)
	}
	return files
}

func splitObjectCatalogPathSegments(pathValue string) []string {
	pathValue = strings.Trim(pathValue, "/")
	if pathValue == "" {
		return nil
	}
	rawSegments := strings.Split(pathValue, "/")
	segments := make([]string, 0, len(rawSegments))
	for _, segment := range rawSegments {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments
}

func splitObjectCompositeGroupKey(key string) (string, string) {
	parts := strings.SplitN(key, "\x00", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
