package metaitem

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
)

type ObjectStorageCompositeItem struct {
	Bucket string
	Prefix string
	Item   *dataitem.DetectedItem
	Claims dataitem.ResourceClaimSet
}

type ObjectStorageCompositeDetectionError struct {
	Bucket string
	Prefix string
	Err    error
}

func DetectObjectStorageCompositeItems(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	metas []format.ObjectMetadata,
) (map[string]bool, []ObjectStorageCompositeItem, []ObjectStorageCompositeDetectionError) {
	skipPaths := map[string]bool{}
	if contentReader == nil {
		return skipPaths, nil, nil
	}

	groups := objectMetasByParentPrefix(metas)
	items := make([]ObjectStorageCompositeItem, 0)
	warnings := make([]ObjectStorageCompositeDetectionError, 0)
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
		group := unclaimedObjectMetas(groups[groupKey], skipPaths)
		if len(group) < 2 {
			continue
		}
		bucket, prefix := splitObjectCompositeGroupKey(groupKey)
		if prefix == "" {
			continue
		}
		files := objectMetasToFileEntries(bucket, group)
		detection, err := ResolveItems(ctx, dataitem.DirectoryResolveInput{
			ContentReader: contentReader,
			ConnInfo:      connInfo,
			EngineID:      engineID,
			DirPath:       bucket + "/" + prefix,
			Files:         files,
		})
		if err != nil {
			warnings = append(warnings, ObjectStorageCompositeDetectionError{
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
			items = append(items, ObjectStorageCompositeItem{
				Bucket: bucket,
				Prefix: prefix,
				Item:   detected,
				Claims: detection.Claims,
			})
		}
	}
	return skipPaths, items, warnings
}

func InferObjectStorageDataItem(meta format.ObjectMetadata, objectName string) *dataitem.DetectedItem {
	physicalPath := meta.Bucket + "/" + meta.Path
	return dataitem.InferSingleResource(dataitem.SingleResourceInput{
		Name:   objectName,
		Path:   physicalPath,
		Size:   meta.SizeBytes,
		Format: meta.FileType,
	})
}

func ObjectStorageCompositeName(composite ObjectStorageCompositeItem) (name, objectPath string) {
	if composite.Item != nil {
		switch composite.Item.Organization {
		case dataitem.OrganizationSingle, dataitem.OrganizationMulti:
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

func ObjectStorageCompositeMode(item *dataitem.DetectedItem) string {
	if item == nil {
		return "directory"
	}
	switch item.Organization {
	case dataitem.OrganizationSingle:
		return "single"
	case dataitem.OrganizationMulti:
		return "multi"
	case dataitem.OrganizationWhole:
		return "whole"
	default:
		return "directory"
	}
}

func ObjectStorageSingleFileItemType(item *dataitem.DetectedItem) string {
	if item == nil {
		return "object"
	}
	if item.ItemType != "" {
		return item.ItemType
	}
	if rule, ok := dataitem.MatchBuiltinSingleResourceRule(item.Format); ok &&
		rule.Organization == dataitem.OrganizationSingle &&
		rule.ItemType != "" {
		return rule.ItemType
	}
	return "object"
}

type ObjectStorageRelativePathPlan struct {
	Segments   []string
	ExactBase  bool
	SkipReason string
}

type ObjectStorageSingleItemPlan struct {
	ItemType    string
	ItemName    string
	ObjectName  string
	FullName    string
	Fingerprint string
	Attributes  models.JSONMap
	DataItem    *dataitem.DetectedItem
}

type ObjectStorageCompositeItemPlan struct {
	ItemType    string
	ItemName    string
	ObjectPath  string
	ParentPath  string
	FullName    string
	Fingerprint string
	SizeBytes   int64
	Attributes  models.JSONMap
}

func PlanObjectStorageRelativePath(trimmedPath, scanPathPrefix string) ObjectStorageRelativePathPlan {
	trimmedPath = strings.Trim(trimmedPath, "/")
	scanPathPrefix = strings.Trim(scanPathPrefix, "/")

	switch {
	case scanPathPrefix != "" && trimmedPath == scanPathPrefix:
		return ObjectStorageRelativePathPlan{
			ExactBase:  true,
			SkipReason: "trimmed==scanPathPrefix",
		}
	case scanPathPrefix != "" && strings.HasPrefix(trimmedPath, scanPathPrefix+"/"):
		remaining := strings.TrimPrefix(trimmedPath, scanPathPrefix+"/")
		return ObjectStorageRelativePathPlan{
			Segments:   splitObjectStoragePathSegments(remaining),
			SkipReason: "trimmed以scanPathPrefix/开头，去掉前缀",
		}
	case trimmedPath != "":
		return ObjectStorageRelativePathPlan{
			Segments: splitObjectStoragePathSegments(trimmedPath),
		}
	default:
		return ObjectStorageRelativePathPlan{
			SkipReason: "空路径",
		}
	}
}

func PlanObjectStorageSingleItem(engineID uint, meta format.ObjectMetadata, trimmedPath string) ObjectStorageSingleItemPlan {
	objectName := path.Base(strings.Trim(meta.Path, "/"))
	if objectName == "" {
		objectName = strings.Trim(trimmedPath, "/")
	}
	objectName = strings.Trim(objectName, "/")
	if objectName == "" {
		objectName = fmt.Sprintf("object_%d", meta.SizeBytes)
	}

	dataItem := InferObjectStorageDataItem(meta, objectName)
	itemType := ObjectStorageSingleFileItemType(dataItem)
	if itemType == "" {
		itemType = "object"
	}

	dir, name := commonModels.SplitObjectPath(meta.Path)
	attrs := models.JSONMap{
		"bucket":       meta.Bucket,
		"path":         dir,
		"name":         name,
		"file_type":    meta.FileType,
		"object_count": meta.ObjectCount,
	}
	if meta.LastModified != nil {
		attrs["last_modified_at"] = meta.LastModified
	}
	MergeDataItemAttributes(attrs, dataItem)
	ApplyContainerSummary(attrs, dataItem)

	fullName := commonModels.JoinObjectPath(meta.Bucket, dir, name)
	return ObjectStorageSingleItemPlan{
		ItemType:    itemType,
		ItemName:    objectName,
		ObjectName:  objectName,
		FullName:    fullName,
		Fingerprint: commonModels.GenerateItemFingerprint(engineID, fullName),
		Attributes:  attrs,
		DataItem:    dataItem,
	}
}

func PlanObjectStorageCompositeItem(engineID uint, composite ObjectStorageCompositeItem) (ObjectStorageCompositeItemPlan, bool) {
	if composite.Item == nil {
		return ObjectStorageCompositeItemPlan{}, false
	}

	itemName, objectPath := ObjectStorageCompositeName(composite)
	parentPath := ParentObjectPath(objectPath)
	fullName := commonModels.JoinObjectPath(composite.Bucket, parentPath, itemName)

	attrs := models.JSONMap(BuildAttributes(composite.Item))
	if len(composite.Item.Fields) > 0 {
		metaattr.SetSchemaFields(attrs, metaattr.FieldAttributesFromFormat(composite.Item.Fields))
	}
	metaattr.SetStorage(attrs, "bucket", composite.Bucket)
	metaattr.SetStorage(attrs, "path", parentPath)
	metaattr.SetStorage(attrs, "name", itemName)
	metaattr.SetItem(attrs, "mode", ObjectStorageCompositeMode(composite.Item))

	return ObjectStorageCompositeItemPlan{
		ItemType:    composite.Item.ItemType,
		ItemName:    itemName,
		ObjectPath:  objectPath,
		ParentPath:  parentPath,
		FullName:    fullName,
		Fingerprint: commonModels.GenerateItemFingerprint(engineID, fullName),
		SizeBytes:   composite.Item.SizeBytes,
		Attributes:  attrs,
	}, true
}

func UnclaimedObjectMetas(group []format.ObjectMetadata, skipPaths map[string]bool) []format.ObjectMetadata {
	return unclaimedObjectMetas(group, skipPaths)
}

func ObjectMetasByParentPrefix(metas []format.ObjectMetadata) map[string][]format.ObjectMetadata {
	return objectMetasByParentPrefix(metas)
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

func objectMetasByParentPrefix(metas []format.ObjectMetadata) map[string][]format.ObjectMetadata {
	groups := map[string][]format.ObjectMetadata{}
	for _, meta := range metas {
		if meta.NodeType != "object" {
			continue
		}
		parent := strings.Trim(ParentObjectPath(meta.Path), "/")
		if parent == "" {
			continue
		}
		key := meta.Bucket + "\x00" + parent
		groups[key] = append(groups[key], meta)
	}
	return groups
}

func unclaimedObjectMetas(group []format.ObjectMetadata, skipPaths map[string]bool) []format.ObjectMetadata {
	if len(group) == 0 || len(skipPaths) == 0 {
		return group
	}
	filtered := make([]format.ObjectMetadata, 0, len(group))
	for _, meta := range group {
		if !skipPaths[meta.Path] {
			filtered = append(filtered, meta)
		}
	}
	return filtered
}

func objectMetasToFileEntries(bucket string, metas []format.ObjectMetadata) []plugin.FileEntry {
	sort.Slice(metas, func(i, j int) bool {
		return metas[i].Path < metas[j].Path
	})
	files := make([]plugin.FileEntry, 0, len(metas))
	for _, meta := range metas {
		modifiedAt := time.Time{}
		if meta.LastModified != nil {
			modifiedAt = *meta.LastModified
		}
		files = append(files, plugin.FileEntry{
			Name:       path.Base(meta.Path),
			Path:       bucket + "/" + meta.Path,
			Size:       meta.SizeBytes,
			ModifiedAt: modifiedAt,
		})
	}
	return files
}

func splitObjectStoragePathSegments(pathValue string) []string {
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
