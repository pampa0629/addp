package service

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scantask"
)

func (s *ScanService) RefreshItem(ctx context.Context, engineID, tenantID, itemID uint, token string, force bool) (*models.ScanResponse, error) {
	start := time.Now()
	if itemID == 0 {
		return nil, fmt.Errorf("item_id is required")
	}

	var item models.MetaItem
	if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, itemID).First(&item).Error; err != nil {
		return nil, fmt.Errorf("item target not found: %w", err)
	}
	if engineID > 0 && item.EngineID != engineID {
		return nil, fmt.Errorf("item engine_id does not match request engine_id")
	}
	if engineID == 0 {
		engineID = item.EngineID
	}

	resource, err := s.engineService.GetResourceByID(engineID, tenantID, token)
	if err != nil {
		return nil, err
	}
	p, err := plugin.Get(resource.EngineType)
	if err != nil {
		return nil, fmt.Errorf("unsupported engine type: %s", resource.EngineType)
	}
	contentReader, ok := p.(plugin.ContentReadableProvider)
	if !ok {
		return nil, fmt.Errorf("engine %s does not implement ContentReadableProvider", resource.EngineType)
	}

	refreshed, fields, err := s.refreshKnownItemAttributes(ctx, resource, &item, contentReader, force)
	if err != nil {
		return nil, err
	}
	refreshed = metaattr.Normalize(refreshed)
	descriptor := knownItemDescriptorFromMetaAttributes(refreshed)
	rowCount := itemRowCountFromMetaAttributes(refreshed)
	sizeBytes := item.SizeBytes
	if sizeBytes == nil {
		sizeBytes = descriptor.SizeBytes
	}
	if err := s.db.Model(&item).Updates(map[string]interface{}{
		"attributes":      refreshed,
		"row_count":       rowCount,
		"size_bytes":      sizeBytes,
		"scanned_at":      time.Now(),
		"scanned_depth":   models.ScannedDepthDeep,
		"data_updated_at": item.DataUpdatedAt,
	}).Error; err != nil {
		return nil, err
	}

	item.Attributes = refreshed
	item.RowCount = rowCount
	item.SizeBytes = sizeBytes
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	catalogPathFor := knownItemCatalogPathResolver(resource.ID, p, descriptor)
	physicalPath := knownItemPhysicalPath(descriptor, &item)
	if contentHash, err := computeContentSHA256(ctx, contentReader, connInfo, catalogPathFor(physicalPath)); err != nil {
		return nil, err
	} else {
		setStorageContentHash(item.Attributes, contentHash)
	}
	extraction, err := extractKnownItemDocumentText(ctx, item.Attributes, contentReader, connInfo, resource.ID, catalogPathFor, &item, descriptor, physicalPath)
	if err != nil {
		return nil, err
	}
	extractedText := extraction.Text
	item.Attributes = metaattr.Normalize(item.Attributes)
	if err := s.db.Model(&item).Updates(map[string]interface{}{
		"attributes": item.Attributes,
	}).Error; err != nil {
		return nil, err
	}
	if s.indexerService != nil && catalogModelItemTerm(p) == plugin.CatalogTermObject && descriptor.StorageBucket != "" {
		objectPath := knownItemObjectPath(descriptor, physicalPath)
		if objectPath != "" {
			catalogResource := metacatalog.StorageResource{
				RootName:     descriptor.StorageBucket,
				Path:         objectPath,
				FullPath:     item.FullName,
				NodeType:     item.ItemType,
				Format:       descriptor.Format,
				SizeBytes:    sizeFromDescriptor(descriptor, &item),
				ObjectCount:  1,
				LastModified: item.DataUpdatedAt,
				CatalogPath:  catalogPathFor(objectPath),
			}
			if s.indexerService.IndexCatalogAsset(resource, tenantID, resource.ID, catalogResource, objectPath, item.FullName, &item, extractedText) {
				if extractedText != "" {
					extraction.Counts.Indexed++
				}
			} else if extractedText != "" {
				extraction.Counts.IndexFailed++
			}
		}
	}

	return &models.ScanResponse{
		Status:        "success",
		Message:       "item refreshed",
		ItemsScanned:  1,
		FieldsScanned: fields,
		DurationMs:    time.Since(start).Milliseconds(),
		StartedAt:     start.Format(time.RFC3339),
		Extraction:    extractionStatsModel(extraction.Counts),
	}, nil
}

func extractionStatsModel(counts scantask.ExtractionCounts) *models.ExtractionScanStats {
	if counts.Empty() {
		return nil
	}
	return &models.ExtractionScanStats{
		Documents:   counts.Documents,
		Extracted:   counts.Extracted,
		Unsupported: counts.Unsupported,
		Failed:      counts.Failed,
		Indexed:     counts.Indexed,
		IndexFailed: counts.IndexFailed,
	}
}

func (s *ScanService) refreshKnownItemAttributes(
	ctx context.Context,
	resource *commonModels.Engine,
	item *models.MetaItem,
	contentReader plugin.ContentReadableProvider,
	force bool,
) (models.JSONMap, int, error) {
	attrs := cloneJSONMap(item.Attributes)
	descriptor := knownItemDescriptorFromMetaAttributes(attrs)
	detected := detectedItemFromDescriptor(item, descriptor)
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	catalogPathFor := knownItemCatalogPathResolver(resource.ID, contentReader, descriptor)

	var err error
	switch descriptor.Layout {
	case format.LayoutMulti:
		clearStaleKnownMultiTableAccessIndex(attrs, detected)
		detected, _, err = metaitem.EnrichKnownMultiTableItem(ctx, contentReader, connInfo, resource.ID, catalogPathFor, detected)
	case format.LayoutSingle, format.LayoutWhole:
		physicalPath := knownItemPhysicalPath(descriptor, item)
		if detected, err = enrichKnownResourceAttributes(ctx, attrs, contentReader, connInfo, resource.ID, catalogPathFor, detected, physicalPath, sizeFromDescriptor(descriptor, item)); err != nil {
			return nil, 0, err
		}
	default:
		return metaattr.Normalize(attrs), 0, fmt.Errorf("item layout is missing or unsupported")
	}
	if err != nil {
		return nil, 0, err
	}

	if detected != nil && len(detected.Attributes) > 0 {
		attrs = metaattr.JSONMap(detected.Attributes)
	}
	metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(detected))
	restoreKnownItemStorage(attrs, descriptor, item)
	return metaattr.Normalize(attrs), len(detected.Fields), nil
}

func extractKnownItemDocumentText(
	ctx context.Context,
	attrs models.JSONMap,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	catalogPathFor func(path string) plugin.CatalogPath,
	item *models.MetaItem,
	descriptor dataitem.ItemDescriptor,
	physicalPath string,
) (documentExtractionResult, error) {
	if attrs == nil || contentReader == nil || catalogPathFor == nil || item == nil || descriptor.DataType != datatype.Document || physicalPath == "" {
		return documentExtractionResult{}, nil
	}
	detected := detectedItemFromDescriptor(item, descriptor)
	resource := metacatalog.StorageResource{
		RootName:     descriptor.StorageBucket,
		Path:         physicalPath,
		FullPath:     item.FullName,
		NodeType:     item.ItemType,
		Format:       descriptor.Format,
		SizeBytes:    sizeFromDescriptor(descriptor, item),
		ObjectCount:  1,
		LastModified: item.DataUpdatedAt,
		CatalogPath:  catalogPathFor(physicalPath),
	}
	return extractCatalogDocumentText(ctx, attrs, contentReader, connInfo, engineID, resource, detected), nil
}

func enrichKnownResourceAttributes(
	ctx context.Context,
	attrs models.JSONMap,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	catalogPathFor func(path string) plugin.CatalogPath,
	item *metaitem.DetectedItem,
	physicalPath string,
	sizeBytes int64,
) (*metaitem.DetectedItem, error) {
	enriched, _, err := metaenrich.EnrichResourceAttributes(ctx, attrs, metaenrich.ResourceAttributesInput{
		ContentReader:      contentReader,
		ConnInfo:           connInfo,
		EngineID:           engineID,
		Item:               item,
		PhysicalPath:       physicalPath,
		SizeBytes:          sizeBytes,
		IncludeAccessIndex: true,
		CatalogPathFor:     catalogPathFor,
	})
	if enriched != nil {
		enriched.Attributes = attrs
	}
	return enriched, err
}

func clearStaleKnownMultiTableAccessIndex(attrs map[string]interface{}, item *metaitem.DetectedItem) {
	if attrs == nil || item == nil {
		return
	}
	if item.Layout != format.LayoutMulti || item.DataType != datatype.Table {
		return
	}
	metaattr.RemoveAccessIndexTable(attrs)
	metaattr.RemoveAccessIndexTable(item.Attributes)
}

func detectedItemFromDescriptor(item *models.MetaItem, descriptor dataitem.ItemDescriptor) *metaitem.DetectedItem {
	size := sizeFromDescriptor(descriptor, item)
	return &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Name:               item.Name,
			FullName:           item.FullName,
			ItemType:           item.ItemType,
			Layout:             descriptor.Layout,
			DataType:           descriptor.DataType,
			Format:             descriptor.Format,
			PrimaryContentPath: knownItemPrimaryContentPath(descriptor, item),
			RefList:            descriptor.Refs,
			SizeBytes:          &size,
		},
		PhysicalPath: knownItemPhysicalPath(descriptor, item),
		Attributes:   clonePlainMap(item.Attributes),
	}
}

func knownItemDescriptorFromMetaAttributes(attrs map[string]interface{}) dataitem.ItemDescriptor {
	return dataitem.DescriptorFromAttributes(attrs)
}

func knownItemPrimaryContentPath(descriptor dataitem.ItemDescriptor, item *models.MetaItem) string {
	return firstNonEmpty(descriptor.PrimaryContentPath, descriptor.PhysicalPath, pathFromStorage(descriptor), knownItemFullName(item))
}

func knownItemPhysicalPath(descriptor dataitem.ItemDescriptor, item *models.MetaItem) string {
	return firstNonEmpty(descriptor.PhysicalPath, descriptor.PrimaryContentPath, pathFromStorage(descriptor), knownItemFullName(item))
}

func knownItemObjectPath(descriptor dataitem.ItemDescriptor, physicalPath string) string {
	objectPath := pathFromStorage(descriptor)
	if objectPath != "" {
		return objectPath
	}
	objectPath = strings.Trim(physicalPath, "/")
	if bucket, parsedPath := metapath.SplitObjectPath(objectPath); bucket == descriptor.StorageBucket && parsedPath != "" {
		return parsedPath
	}
	return objectPath
}

func knownItemFullName(item *models.MetaItem) string {
	if item == nil {
		return ""
	}
	return item.FullName
}

func knownItemCatalogPathResolver(engineID uint, provider plugin.EnginePlugin, descriptor dataitem.ItemDescriptor) func(string) plugin.CatalogPath {
	bucket := descriptor.StorageBucket
	itemTerm := catalogModelItemTerm(provider)
	return func(rawPath string) plugin.CatalogPath {
		path := strings.Trim(rawPath, "/")
		if bucket != "" {
			if b, objectPath := metapath.SplitObjectPath(path); b == bucket && objectPath != "" {
				path = objectPath
			}
			return plugin.ObjectItemPath(engineID, bucket, path)
		}
		if b, objectPath := metapath.SplitObjectPath(path); b != "" && objectPath != "" && itemTerm == plugin.CatalogTermObject {
			return plugin.ObjectItemPath(engineID, b, objectPath)
		}
		return plugin.FileItemPath(engineID, path)
	}
}

func catalogModelItemTerm(provider plugin.EnginePlugin) string {
	modelProvider, ok := provider.(plugin.CatalogModelProvider)
	if !ok {
		return ""
	}
	return plugin.CatalogLeafTerm(modelProvider.CatalogModel())
}

func restoreKnownItemStorage(attrs models.JSONMap, descriptor dataitem.ItemDescriptor, item *models.MetaItem) {
	if descriptor.StorageBucket != "" {
		metaattr.SetStorage(attrs, "bucket", descriptor.StorageBucket)
	}
	if descriptor.StoragePath != "" {
		metaattr.SetStorage(attrs, "path", descriptor.StoragePath)
	}
	if descriptor.StorageName != "" {
		metaattr.SetStorage(attrs, "name", descriptor.StorageName)
	}
	if descriptor.PhysicalPath != "" {
		metaattr.SetStorage(attrs, "physical_path", descriptor.PhysicalPath)
	}
	if item != nil && item.FullName != "" {
		metaattr.SetItem(attrs, "full_name", item.FullName)
	}
}

func pathFromStorage(descriptor dataitem.ItemDescriptor) string {
	if descriptor.StorageName == "" {
		return strings.Trim(descriptor.StoragePath, "/")
	}
	return strings.Trim(path.Join(strings.Trim(descriptor.StoragePath, "/"), descriptor.StorageName), "/")
}

func sizeFromDescriptor(descriptor dataitem.ItemDescriptor, item *models.MetaItem) int64 {
	if descriptor.SizeBytes != nil {
		return *descriptor.SizeBytes
	}
	if item != nil && item.SizeBytes != nil {
		return *item.SizeBytes
	}
	return 0
}

func cloneJSONMap(input models.JSONMap) models.JSONMap {
	output := models.JSONMap{}
	for key, value := range input {
		output[key] = value
	}
	return output
}

func clonePlainMap(input models.JSONMap) map[string]interface{} {
	output := map[string]interface{}{}
	for key, value := range input {
		output[key] = value
	}
	return output
}
