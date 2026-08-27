package scanflow

import (
	"fmt"
	"path"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

func KnownItemDescriptorFromAttributes(attrs map[string]interface{}) dataitem.ItemDescriptor {
	return dataitem.DescriptorFromAttributes(attrs)
}

func ValidateKnownItemRefreshDescriptor(descriptor dataitem.ItemDescriptor, item *models.MetaItem) error {
	switch descriptor.Layout {
	case format.LayoutMulti:
		if descriptor.Format == "" || len(descriptor.Refs) == 0 {
			return fmt.Errorf("item refs are incomplete; rescan the parent node or submit complete refs")
		}
	case format.LayoutSingle, format.LayoutWhole:
		if KnownItemPhysicalPath(descriptor, item) == "" {
			return fmt.Errorf("item physical path is missing; rescan the parent node")
		}
	default:
		return fmt.Errorf("item layout is missing or unsupported")
	}
	return nil
}

func KnownItemDetectedItem(item *models.MetaItem, descriptor dataitem.ItemDescriptor) *metaitem.DetectedItem {
	size := KnownItemSize(descriptor, item)
	return &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Name:               item.Name,
			FullName:           item.FullName,
			ItemType:           item.ItemType,
			Layout:             descriptor.Layout,
			DataType:           descriptor.DataType,
			Format:             descriptor.Format,
			PrimaryContentPath: KnownItemPrimaryContentPath(descriptor, item),
			RefList:            descriptor.Refs,
			SizeBytes:          &size,
		},
		PhysicalPath: KnownItemPhysicalPath(descriptor, item),
		Attributes:   copyKnownItemAttributes(item.Attributes),
	}
}

func KnownItemPrimaryContentPath(descriptor dataitem.ItemDescriptor, item *models.MetaItem) string {
	if descriptor.Layout == format.LayoutWhole {
		return FirstNonEmpty(knownItemPrimaryRefPath(descriptor), descriptor.PrimaryContentPath, knownItemFullName(item), descriptor.ScopePath, descriptor.PhysicalPath, knownItemPathFromStorage(descriptor))
	}
	return FirstNonEmpty(descriptor.PrimaryContentPath, descriptor.PhysicalPath, knownItemPathFromStorage(descriptor), knownItemFullName(item))
}

func KnownItemPhysicalPath(descriptor dataitem.ItemDescriptor, item *models.MetaItem) string {
	if descriptor.Layout == format.LayoutWhole {
		return FirstNonEmpty(knownItemFullName(item), descriptor.ScopePath, descriptor.PhysicalPath, knownItemPathFromStorage(descriptor))
	}
	return FirstNonEmpty(descriptor.PhysicalPath, descriptor.PrimaryContentPath, knownItemPathFromStorage(descriptor), knownItemFullName(item))
}

func KnownItemObjectPath(descriptor dataitem.ItemDescriptor, physicalPath string) string {
	objectPath := knownItemPathFromStorage(descriptor)
	if objectPath != "" {
		return objectPath
	}
	objectPath = strings.Trim(physicalPath, "/")
	if bucket, parsedPath, err := plugin.SplitObjectRefPath(objectPath); err == nil && bucket == descriptor.StorageBucket {
		return parsedPath
	}
	return objectPath
}

func KnownItemCatalogPathResolver(engineID uint, provider plugin.EnginePlugin, descriptor dataitem.ItemDescriptor) func(string) plugin.EngineCatalogPath {
	bucket := descriptor.StorageBucket
	itemTerm := knownItemCatalogModelItemTerm(provider)
	return func(rawPath string) plugin.EngineCatalogPath {
		pathValue := strings.Trim(rawPath, "/")
		if bucket != "" {
			return plugin.ObjectItemPathFromBucketRef(engineID, bucket, pathValue)
		}
		if b, objectPath, err := plugin.SplitObjectRefPath(pathValue); err == nil && itemTerm == plugin.EngineCatalogTermObject {
			return plugin.ObjectItemPath(engineID, b, objectPath)
		}
		return plugin.FileItemPath(engineID, pathValue)
	}
}

func KnownItemSize(descriptor dataitem.ItemDescriptor, item *models.MetaItem) int64 {
	if descriptor.SizeBytes != nil {
		return *descriptor.SizeBytes
	}
	if item != nil && item.SizeBytes != nil {
		return *item.SizeBytes
	}
	return 0
}

func knownItemCatalogModelItemTerm(provider plugin.EnginePlugin) string {
	modelProvider, ok := provider.(plugin.EngineCatalogModelProvider)
	if !ok {
		return ""
	}
	return plugin.EngineCatalogLeafTerm(modelProvider.EngineCatalogModel())
}

func knownItemPathFromStorage(descriptor dataitem.ItemDescriptor) string {
	if descriptor.StorageName == "" {
		return strings.Trim(descriptor.StoragePath, "/")
	}
	return strings.Trim(path.Join(strings.Trim(descriptor.StoragePath, "/"), descriptor.StorageName), "/")
}

func knownItemPrimaryRefPath(descriptor dataitem.ItemDescriptor) string {
	for _, ref := range descriptor.Refs {
		if ref.Primary && strings.TrimSpace(ref.Path) != "" {
			return strings.Trim(ref.Path, "/")
		}
	}
	return ""
}

func knownItemFullName(item *models.MetaItem) string {
	if item == nil {
		return ""
	}
	return item.FullName
}

func copyKnownItemAttributes(input models.JSONMap) map[string]interface{} {
	output := map[string]interface{}{}
	for key, value := range input {
		output[key] = value
	}
	return output
}
