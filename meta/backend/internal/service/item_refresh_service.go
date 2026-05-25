package service

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metaenrich"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
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
	rowCount := itemRowCountFromAttributes(refreshed)
	sizeBytes := item.SizeBytes
	if sizeBytes == nil {
		sizeBytes = dataitem.DescriptorFromAttributes(refreshed).SizeBytes
	}
	if err := s.db.Model(&item).Updates(map[string]interface{}{
		"attributes":      metaattr.Normalize(refreshed),
		"row_count":       rowCount,
		"size_bytes":      sizeBytes,
		"scanned_at":      time.Now(),
		"scanned_depth":   models.ScannedDepthDeep,
		"data_updated_at": item.DataUpdatedAt,
	}).Error; err != nil {
		return nil, err
	}

	return &models.ScanResponse{
		Status:        "success",
		Message:       "item refreshed",
		ItemsScanned:  1,
		FieldsScanned: fields,
		DurationMs:    time.Since(start).Milliseconds(),
		StartedAt:     start.Format(time.RFC3339),
	}, nil
}

func (s *ScanService) refreshKnownItemAttributes(
	ctx context.Context,
	resource *commonModels.Engine,
	item *models.MetaItem,
	contentReader plugin.ContentReadableProvider,
	force bool,
) (models.JSONMap, int, error) {
	attrs := cloneJSONMap(item.Attributes)
	descriptor := dataitem.DescriptorFromAttributes(attrs)
	detected := detectedItemFromDescriptor(item, descriptor)
	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	catalogPathFor := knownItemCatalogPathResolver(resource.ID, resource.EngineType, descriptor)

	enriched := false
	var err error
	switch descriptor.Layout {
	case dataitem.LayoutMulti:
		detected, enriched, err = metaitem.EnrichKnownMultiTableItem(ctx, contentReader, connInfo, resource.ID, catalogPathFor, detected)
	case dataitem.LayoutSingle:
		physicalPath := firstNonEmpty(descriptor.PhysicalPath, descriptor.EntryPath, pathFromStorage(descriptor), item.FullName)
		detected, enriched, err = metaenrich.EnrichSingleTableFileItem(ctx, contentReader, connInfo, resource.ID, detected, physicalPath, sizeFromDescriptor(descriptor, item), true, catalogPathFor)
		if !enriched {
			enriched, err = enrichKnownSingleNonTableItem(ctx, contentReader, connInfo, resource.ID, catalogPathFor, detected, physicalPath)
		}
	case dataitem.LayoutWhole:
		enriched, err = enrichKnownWholeItem(ctx, contentReader, connInfo, resource.ID, catalogPathFor, detected)
	default:
		return metaattr.Normalize(attrs), 0, fmt.Errorf("item layout is missing or unsupported")
	}
	if err != nil {
		return nil, 0, err
	}

	if enriched {
		attrs = metaattr.JSONMap(detected.Attributes)
	}
	clearObsoleteKnownItemAttributes(attrs, detected)
	metaattr.MergeDataItemAttributes(attrs, metaitem.AttributeInput(detected))
	restoreKnownItemStorage(attrs, descriptor, item)
	return metaattr.Normalize(attrs), len(detected.Fields), nil
}

func clearObsoleteKnownItemAttributes(attrs map[string]interface{}, item *metaitem.DetectedItem) {
	if attrs == nil || item == nil {
		return
	}
	if item.Format == string(format.FormatShapefile) {
		metaattr.RemoveContentIndexTable(attrs)
		metaattr.RemoveContentIndexTable(item.Attributes)
	}
}

func detectedItemFromDescriptor(item *models.MetaItem, descriptor dataitem.ItemDescriptor) *metaitem.DetectedItem {
	size := sizeFromDescriptor(descriptor, item)
	return &metaitem.DetectedItem{
		ResolvedItem: dataitem.ResolvedItem{
			Name:      item.Name,
			FullName:  item.FullName,
			ItemType:  item.ItemType,
			Layout:    descriptor.Layout,
			DataType:  descriptor.DataType,
			Format:    descriptor.Format,
			EntryPath: firstNonEmpty(descriptor.EntryPath, descriptor.PhysicalPath, pathFromStorage(descriptor), item.FullName),
			RefList:   descriptor.Refs,
			SizeBytes: &size,
		},
		PhysicalPath: firstNonEmpty(descriptor.PhysicalPath, pathFromStorage(descriptor), item.FullName),
		Attributes:   clonePlainMap(item.Attributes),
	}
}

func enrichKnownSingleNonTableItem(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	catalogPathFor func(path string) plugin.CatalogPath,
	item *metaitem.DetectedItem,
	path string,
) (bool, error) {
	if item == nil || path == "" {
		return false, nil
	}
	if metaenrich.IsUnknownFormatName(item.Format) && catalogPathFor != nil {
		detectedFormat, err := metaenrich.DetectSingleFileFormat(ctx, contentReader, connInfo, catalogPathFor(path), path)
		if err != nil {
			return false, err
		}
		metaenrich.ApplySingleFileFormat(item, detectedFormat)
	}
	if metaenrich.IsUnknownFormatName(item.Format) {
		return false, nil
	}
	formatType := format.FormatType(item.Format)
	if provider, err := format.GetDocumentInfoProvider(formatType); err == nil {
		rc, err := contentReader.OpenContent(ctx, connInfo, catalogPathFor(path), plugin.ReadOptions{})
		if err != nil {
			return false, err
		}
		defer rc.Close()
		info, err := provider.DescribeDocument(ctx, rc, nil)
		if err != nil {
			return false, err
		}
		metaattr.MergeAttributeMaps(item.Attributes, metaattr.DocumentInfoAttributes(info))
		return true, nil
	}
	if provider, err := format.GetMediaInfoProvider(formatType); err == nil {
		rc, err := contentReader.OpenContent(ctx, connInfo, catalogPathFor(path), plugin.ReadOptions{})
		if err != nil {
			return false, err
		}
		defer rc.Close()
		info, err := provider.DescribeMedia(ctx, rc, nil)
		if err != nil {
			return false, err
		}
		metaattr.MergeAttributeMaps(item.Attributes, metaattr.MediaInfoAttributes(info.Media, info.Spatial))
		return true, nil
	}
	return false, nil
}

func enrichKnownWholeItem(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	catalogPathFor func(path string) plugin.CatalogPath,
	item *metaitem.DetectedItem,
) (bool, error) {
	if item == nil || item.DataType != dataitem.DataTypeContainer {
		return false, nil
	}
	path := firstNonEmpty(item.PhysicalPath, item.EntryPath)
	if path == "" {
		return false, nil
	}
	rc, err := contentReader.OpenContent(ctx, connInfo, catalogPathFor(path), plugin.ReadOptions{})
	if err != nil {
		return false, err
	}
	defer rc.Close()
	if err := metaenrich.EnrichContainerChildren(ctx, item.Attributes, item, rc); err != nil {
		return false, err
	}
	return true, nil
}

func knownItemCatalogPathResolver(engineID uint, engineType string, descriptor dataitem.ItemDescriptor) func(string) plugin.CatalogPath {
	bucket := descriptor.StorageBucket
	return func(rawPath string) plugin.CatalogPath {
		path := strings.Trim(rawPath, "/")
		if bucket != "" {
			if b, objectPath := metapath.SplitObjectPath(path); b == bucket && objectPath != "" {
				path = objectPath
			}
			return plugin.ObjectItemPath(engineID, bucket, path)
		}
		if b, objectPath := metapath.SplitObjectPath(path); b != "" && objectPath != "" && isObjectLikeEngine(engineType) {
			return plugin.ObjectItemPath(engineID, b, objectPath)
		}
		return plugin.FileItemPath(engineID, path)
	}
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

func isObjectLikeEngine(engineType string) bool {
	switch strings.ToLower(strings.TrimSpace(engineType)) {
	case "minio", "s3", "object_storage":
		return true
	default:
		return false
	}
}
