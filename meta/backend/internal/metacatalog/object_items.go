package metacatalog

import (
	"fmt"
	"path"
	"strings"

	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

type ObjectCatalogCompositeItem struct {
	Bucket string
	Prefix string
	Item   *metaitem.DetectedItem
	Claims metaitem.ResourceClaimSet
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
		case format.LayoutSingle, format.LayoutMulti:
			if composite.Item.PrimaryContentPath != "" {
				objectPath = ObjectPathFromClaim(composite.Bucket, composite.Item.PrimaryContentPath)
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
	case format.LayoutSingle:
		return "single"
	case format.LayoutMulti:
		return "multi"
	case format.LayoutWhole:
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
	if resource.LastModified != nil {
		metaattr.SetStorage(attrs, "last_modified_at", resource.LastModified)
	}
	metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(dataItem))
	metaitem.ApplyContainerSummary(attrs, dataItem)

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

	qualifyObjectDetectedItemPaths(composite.Bucket, composite.Item)
	itemName, objectPath := ObjectCatalogCompositeName(composite)
	parentPath := ParentObjectPath(objectPath)
	fullName := commonModels.JoinObjectPath(composite.Bucket, parentPath, itemName)

	attrs := models.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(composite.Item)))
	if len(composite.Item.Fields) > 0 {
		metaattr.SetTableFields(attrs, composite.Item.Fields)
	}
	metaattr.SetStorage(attrs, "bucket", composite.Bucket)
	metaattr.SetStorage(attrs, "path", parentPath)
	metaattr.SetStorage(attrs, "name", itemName)
	metaattr.SetStorage(attrs, "physical_path", fullName)
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

func qualifyObjectDetectedItemPaths(bucket string, item *metaitem.DetectedItem) {
	if item == nil || strings.Trim(bucket, "/") == "" {
		return
	}
	item.PrimaryContentPath = qualifyObjectContentPath(bucket, item.PrimaryContentPath)
	item.ScopePath = qualifyObjectContentPath(bucket, item.ScopePath)
	if item.PhysicalPath != "" {
		item.PhysicalPath = qualifyObjectContentPath(bucket, item.PhysicalPath)
	}
	for i := range item.RefList {
		item.RefList[i].Path = qualifyObjectContentPath(bucket, item.RefList[i].Path)
	}
	if len(item.RefPaths) > 0 {
		qualified := map[string]string{}
		for role, pathValue := range item.RefPaths {
			qualified[role] = qualifyObjectContentPath(bucket, pathValue)
		}
		item.RefPaths = qualified
	}
}

func qualifyObjectContentPath(bucket, pathValue string) string {
	pathValue = strings.Trim(pathValue, "/")
	if pathValue == "" {
		return ""
	}
	objectPath := ObjectPathFromClaim(bucket, pathValue)
	if objectPath == "" {
		return pathValue
	}
	return strings.Trim(strings.Trim(bucket, "/")+"/"+objectPath, "/")
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
