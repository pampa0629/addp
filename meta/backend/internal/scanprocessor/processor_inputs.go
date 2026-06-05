package scanprocessor

import (
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
)

func FileSingleInput(
	resource *commonModels.Engine,
	tenantID uint,
	parentNode *models.MetaNode,
	file metaitem.StorageFileRef,
	detected *metaitem.DetectedItem,
	itemType string,
	itemName string,
	fullName string,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	scanDepth string,
) input {
	return input{
		Resource:           resource,
		TenantID:           tenantID,
		EngineID:           resource.ID,
		ParentNode:         parentNode,
		ItemType:           itemType,
		ItemName:           itemName,
		FullName:           fullName,
		Attributes:         metaattr.JSONMap(metaattr.BuildAttributes(metaitem.AttributeInput(detected))),
		Detected:           detected,
		ContentReader:      contentReader,
		ConnInfo:           connInfo,
		CatalogPath:        file.CatalogPath,
		CatalogPathFor:     func(string) plugin.CatalogPath { return file.CatalogPath },
		PhysicalPath:       file.Path,
		IndexPath:          file.Path,
		IndexRelativePath:  file.Path,
		SizeBytes:          file.Size,
		DataUpdatedAt:      fileModifiedAtPtr(file.ModifiedAt),
		ScanDepth:          scanDepth,
		IncludeAccessIndex: true,
	}
}

func FileDetectedInput(
	resource *commonModels.Engine,
	tenantID uint,
	parentNode *models.MetaNode,
	itemPlan metacatalog.FileCatalogDetectedItemPlan,
	detected *metaitem.DetectedItem,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	scanDepth string,
) input {
	return input{
		Resource:           resource,
		TenantID:           tenantID,
		EngineID:           resource.ID,
		ParentNode:         parentNode,
		ItemType:           itemPlan.ItemType,
		ItemName:           itemPlan.ItemName,
		FullName:           itemPlan.FullName,
		Attributes:         itemPlan.Attributes,
		Detected:           detected,
		ContentReader:      contentReader,
		ConnInfo:           connInfo,
		CatalogPathFor:     plugin.FileItemPathForEngine(resource.ID),
		PhysicalPath:       detectedItemContentPath(detected, itemPlan.FullName),
		IndexPath:          itemPlan.FullName,
		IndexRelativePath:  itemPlan.FullName,
		SizeBytes:          itemPlan.SizeBytes,
		ScanDepth:          scanDepth,
		IncludeAccessIndex: true,
	}
}

func ObjectSingleInput(
	resource *commonModels.Engine,
	tenantID, engineID uint,
	parentNode *models.MetaNode,
	itemPlan metacatalog.ObjectCatalogSingleItemPlan,
	catalogResource metacatalog.StorageResource,
	attrs models.JSONMap,
	trimmedPath string,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	scanDepth string,
) input {
	return input{
		Resource:           resource,
		TenantID:           tenantID,
		EngineID:           engineID,
		ParentNode:         parentNode,
		ItemType:           itemPlan.ItemType,
		ItemName:           itemPlan.ItemName,
		FullName:           itemPlan.FullName,
		Attributes:         attrs,
		Detected:           itemPlan.DataItem,
		ContentReader:      contentReader,
		ConnInfo:           connInfo,
		CatalogPath:        catalogResource.CatalogPath,
		CatalogPathFor:     func(string) plugin.CatalogPath { return catalogResource.CatalogPath },
		PhysicalPath:       catalogResource.FullPath,
		IndexRootName:      catalogResource.RootName,
		IndexPath:          catalogResource.Path,
		IndexRelativePath:  trimmedPath,
		SizeBytes:          catalogResource.SizeBytes,
		DataUpdatedAt:      catalogResource.LastModified,
		ScanDepth:          scanDepth,
		IncludeAccessIndex: true,
	}
}

func ObjectCompositeInput(
	resource *commonModels.Engine,
	tenantID, engineID uint,
	parentNode *models.MetaNode,
	itemPlan metacatalog.ObjectCatalogCompositeItemPlan,
	composite metacatalog.ObjectCatalogCompositeItem,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	scanDepth string,
) input {
	return input{
		Resource:           resource,
		TenantID:           tenantID,
		EngineID:           engineID,
		ParentNode:         parentNode,
		ItemType:           itemPlan.ItemType,
		ItemName:           itemPlan.ItemName,
		FullName:           itemPlan.FullName,
		Attributes:         itemPlan.Attributes,
		Detected:           composite.Item,
		ContentReader:      contentReader,
		ConnInfo:           connInfo,
		CatalogPathFor:     plugin.ObjectItemPathForBucket(engineID, composite.Bucket),
		PhysicalPath:       detectedItemContentPath(composite.Item, itemPlan.ObjectPath),
		IndexRootName:      composite.Bucket,
		IndexPath:          itemPlan.ObjectPath,
		IndexRelativePath:  strings.Trim(itemPlan.ObjectPath, "/"),
		SizeBytes:          itemPlan.SizeBytes,
		ScanDepth:          scanDepth,
		IncludeAccessIndex: true,
	}
}

func KnownItemInput(
	resource *commonModels.Engine,
	tenantID uint,
	parentNode *models.MetaNode,
	item models.MetaItem,
	attrs models.JSONMap,
	detected *metaitem.DetectedItem,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	catalogPathFor func(string) plugin.CatalogPath,
	physicalPath string,
	indexRootName string,
	indexPath string,
	sizeBytes int64,
) input {
	return input{
		Resource:           resource,
		TenantID:           tenantID,
		EngineID:           resource.ID,
		ParentNode:         parentNode,
		ExistingItemID:     item.ID,
		ItemType:           item.ItemType,
		ItemName:           item.Name,
		FullName:           item.FullName,
		Attributes:         attrs,
		Detected:           detected,
		ContentReader:      contentReader,
		ConnInfo:           connInfo,
		CatalogPathFor:     catalogPathFor,
		PhysicalPath:       physicalPath,
		IndexRootName:      indexRootName,
		IndexPath:          indexPath,
		IndexRelativePath:  strings.Trim(indexPath, "/"),
		SizeBytes:          sizeBytes,
		DataUpdatedAt:      item.DataUpdatedAt,
		ScanDepth:          models.ScannedDepthDeep,
		IncludeAccessIndex: true,
		StrictDeepEnrich:   true,
	}
}

func detectedItemContentPath(item *metaitem.DetectedItem, fallback string) string {
	if item == nil {
		return strings.Trim(fallback, "/")
	}
	if item.PrimaryContentPath != "" {
		return strings.Trim(item.PrimaryContentPath, "/")
	}
	if item.PhysicalPath != "" {
		return strings.Trim(item.PhysicalPath, "/")
	}
	return strings.Trim(fallback, "/")
}

func splitCatalogResourcePath(value string) (dir, name string) {
	value = strings.Trim(value, "/")
	if value == "" {
		return "", ""
	}
	idx := strings.LastIndex(value, "/")
	if idx < 0 {
		return "", value
	}
	return value[:idx+1], value[idx+1:]
}

func fileModifiedAtPtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func itemRowCountFromMetaAttributes(attrs map[string]interface{}) *int64 {
	tableInfo := datatype.TableInfoFromPayload(commonJSON.Section(attrs, "type_info.table"), "")
	if tableInfo == nil || tableInfo.RowCount == nil || *tableInfo.RowCount <= 0 {
		return nil
	}
	rowCount := *tableInfo.RowCount
	return &rowCount
}
