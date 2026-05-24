package extractor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	pathpkg "path"
	"reflect"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// MetadataExtractor 元数据提取器，负责对象元数据的提取、缓存和查询。
type MetadataExtractor struct {
	db  *gorm.DB
	log *slog.Logger
}

// NewMetadataExtractor 创建元数据提取器实例。
func NewMetadataExtractor(db *gorm.DB) *MetadataExtractor {
	return &MetadataExtractor{
		db:  db,
		log: logger.With("component", "metadata_extractor"),
	}
}

// ExtractEnhancedMetadata 使用插件提取增强的元数据。
func (e *MetadataExtractor) ExtractEnhancedMetadata(
	engineID uint,
	resource metacatalog.StorageResource,
	baseAttrs models.JSONMap,
) models.JSONMap {
	if len(resource.ExtractedAttributes) > 0 {
		mergeStandardAttributes(baseAttrs, resource.ExtractedAttributes)
		return baseAttrs
	}

	contentType := resource.ContentType
	if contentType == "" {
		contentType = mime.TypeByExtension(pathpkg.Ext(resource.Path))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	formatType := format.MIMEToFormat(contentType)
	if formatType == format.FormatUnknown {
		formatType = format.DetectFormat(resource.Path, nil)
	}
	if _, err := format.GetFormatInfoProvider(formatType); err != nil {
		return baseAttrs
	}

	setExtractionAttribute(baseAttrs, "extractor_available", true)
	metaattr.SetStorage(baseAttrs, "content_type", contentType)

	return baseAttrs
}

func mergeStandardAttributes(dst models.JSONMap, src map[string]interface{}) {
	if dst == nil || len(src) == 0 {
		return
	}
	for _, section := range []string{"storage", "item", "type_info", "format_info", "content_index", "capabilities"} {
		values := interfaceMap(src[section])
		if len(values) == 0 {
			continue
		}
		existing := metaattr.Section(dst, section)
		for namespace, value := range values {
			if valueMap := interfaceMap(value); len(valueMap) > 0 {
				namespaceAttrs := map[string]interface{}{}
				for key, existingValue := range interfaceMap(existing[namespace]) {
					namespaceAttrs[key] = existingValue
				}
				for key, newValue := range valueMap {
					namespaceAttrs[key] = newValue
				}
				existing[namespace] = namespaceAttrs
				continue
			}
			existing[namespace] = value
		}
		dst[section] = existing
	}
}

func interfaceMap(value interface{}) map[string]interface{} {
	if value == nil {
		return nil
	}
	if typed, ok := value.(map[string]interface{}); ok {
		return typed
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Map || rv.Type().Key().Kind() != reflect.String {
		return nil
	}
	result := make(map[string]interface{}, rv.Len())
	iter := rv.MapRange()
	for iter.Next() {
		result[iter.Key().String()] = iter.Value().Interface()
	}
	return result
}

// ExtractEnhancedMetadataWithCache 带缓存检查的元数据提取。
// 如果文件未修改（基于 last_modified 时间），跳过重新提取。
// fullPath: 文件在 bucket 中的完整路径（用于 fingerprint 生成）。
func (e *MetadataExtractor) ExtractEnhancedMetadataWithCache(
	engineID uint,
	resource metacatalog.StorageResource,
	baseAttrs models.JSONMap,
	fullPath string,
) models.JSONMap {
	bucket := commonJSON.String(baseAttrs, "storage", "bucket")
	dir, name := commonModels.SplitObjectPath(fullPath)
	fullName := commonModels.JoinObjectPath(bucket, dir, name)
	fingerprint := commonModels.GenerateItemFingerprint(engineID, fullName)

	var existingItem models.MetaItem
	err := e.db.Where("fingerprint = ?", fingerprint).First(&existingItem).Error

	if err == nil && existingItem.DataUpdatedAt != nil && resource.LastModified != nil {
		existingTime := existingItem.DataUpdatedAt.Truncate(time.Second)
		newTime := resource.LastModified.Truncate(time.Second)

		if existingTime.Equal(newTime) {
			if existingItem.Attributes != nil && len(existingItem.Attributes) > 0 {
				if commonJSON.Bool(existingItem.Attributes, "capabilities.extraction", "metadata_extracted") {
					e.log.Debug("复用缓存的元数据",
						"fingerprint", fingerprint,
						"fullPath", fullPath,
						"last_modified", existingTime)
					for key, value := range existingItem.Attributes {
						baseAttrs[key] = value
					}
					return baseAttrs
				}
			}
		}
	}

	return e.ExtractEnhancedMetadata(engineID, resource, baseAttrs)
}

// GetObjectMetadata 获取指定对象的元数据。
// 用于 Manager 模块预览时查询已扫描的元数据。
func (e *MetadataExtractor) GetObjectMetadata(
	tenantID, engineID uint,
	objectKey string,
) (*models.MetaItem, error) {
	bucket, relativePath := metapath.SplitObjectPath(objectKey)
	if bucket == "" {
		return nil, fmt.Errorf("invalid object key: %s", objectKey)
	}

	var bucketNode models.MetaNode
	err := e.db.Where("tenant_id = ? AND engine_id = ? AND node_type = ? AND name = ?",
		tenantID, engineID, "bucket", bucket).First(&bucketNode).Error
	if err != nil {
		return nil, fmt.Errorf("bucket not found: %w", err)
	}

	objectName := pathpkg.Base(relativePath)
	if objectName == "" {
		objectName = relativePath
	}

	var item models.MetaItem

	if relativePath != "" && relativePath != objectName {
		prefixPath := pathpkg.Dir(relativePath)
		segments := strings.Split(prefixPath, "/")

		currentParent := &bucketNode
		for _, segment := range segments {
			if segment == "" || segment == "." {
				continue
			}

			var prefixNode models.MetaNode
			err := e.db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id = ? AND node_type = ? AND name = ?",
				tenantID, engineID, currentParent.ID, "prefix", segment).First(&prefixNode).Error
			if err != nil {
				return nil, fmt.Errorf("prefix not found: %s", segment)
			}
			currentParent = &prefixNode
		}

		err = e.db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND name = ?",
			tenantID, engineID, currentParent.ID, "object", objectName).First(&item).Error
	} else {
		err = e.db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND name = ?",
			tenantID, engineID, bucketNode.ID, "object", objectName).First(&item).Error
	}

	if err != nil {
		return nil, fmt.Errorf("object metadata not found: %w", err)
	}

	return &item, nil
}

// ExtractObjectMetadataOnDemand 按需提取对象的深度元数据。
// 当 Manager 预览时发现元数据未提取，可以调用此方法触发实时提取。
func (e *MetadataExtractor) ExtractObjectMetadataOnDemand(
	tenantID, engineID uint,
	objectKey string,
	token string,
	objectReader io.Reader,
) (map[string]interface{}, error) {
	contentType := mime.TypeByExtension(pathpkg.Ext(objectKey))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	formatType := format.MIMEToFormat(contentType)
	if formatType == format.FormatUnknown {
		formatType = format.DetectFormat(objectKey, nil)
	}
	if capability, ok := format.GetFormatCapability(formatType); ok && capability.DataType == format.FormatDataTypeMedia {
		provider, err := format.GetMediaInfoProvider(formatType)
		if err != nil {
			return nil, fmt.Errorf("format %s has no media info provider: %w", formatType, err)
		}
		mediaInfo, err := provider.DescribeMedia(context.Background(), objectReader, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to describe media: %w", err)
		}
		item, err := e.GetObjectMetadata(tenantID, engineID, objectKey)
		if err != nil {
			return nil, err
		}
		enhancedAttrs := item.Attributes
		if enhancedAttrs == nil {
			enhancedAttrs = make(models.JSONMap)
		}
		mediaAttrs := MediaInfoAttributes(mediaInfo)
		mergeStandardAttributes(enhancedAttrs, mediaAttrs)
		metaattr.SetStorage(enhancedAttrs, "content_type", contentType)
		enhancedAttrs = metaattr.Normalize(enhancedAttrs)
		if err := e.db.Model(item).Update("attributes", enhancedAttrs).Error; err != nil {
			return nil, err
		}
		return mediaAttrs, nil
	}

	provider, err := format.GetFormatInfoProvider(formatType)
	if err != nil {
		return nil, fmt.Errorf("format %s cannot extract metadata: %w", formatType, err)
	}

	item, err := e.GetObjectMetadata(tenantID, engineID, objectKey)
	if err != nil {
		logger.L().Warn("对象元数据不存在，使用默认值", "object_key", objectKey, "error", err)
	}

	extractedAttrs, err := provider.DescribeFormat(context.Background(), objectReader, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to extract metadata: %w", err)
	}
	if len(extractedAttrs) == 0 {
		return nil, fmt.Errorf("format %s did not return metadata", formatType)
	}
	standardAttrs := formatInfoProviderAttributes(formatType, extractedAttrs)

	if item != nil {
		enhancedAttrs := item.Attributes
		if enhancedAttrs == nil {
			enhancedAttrs = make(models.JSONMap)
		}

		setExtractionAttribute(enhancedAttrs, "metadata_extracted", true)
		setExtractionAttribute(enhancedAttrs, "extracted_metadata", standardAttrs)
		mergeStandardAttributes(enhancedAttrs, standardAttrs)
		metaattr.SetStorage(enhancedAttrs, "content_type", contentType)
		enhancedAttrs = metaattr.Normalize(enhancedAttrs)

		if err := e.db.Model(item).Update("attributes", enhancedAttrs).Error; err != nil {
			e.log.Warn("更新元数据失败", "item_id", item.ID, "error", err)
		}
	}

	return standardAttrs, nil
}

func formatInfoProviderAttributes(formatType format.FormatType, formatAttrs map[string]interface{}) map[string]interface{} {
	if len(formatAttrs) == 0 {
		return nil
	}
	for _, section := range []string{"storage", "item", "type_info", "format_info", "content_index", "capabilities"} {
		if values := interfaceMap(formatAttrs[section]); len(values) > 0 {
			return formatAttrs
		}
	}
	return map[string]interface{}{
		"format_info": map[string]interface{}{
			string(formatType): formatAttrs,
		},
	}
}

func (e *MetadataExtractor) BuildObjectContentIndexOnDemand(
	tenantID, engineID uint,
	objectKey string,
	objectReader io.Reader,
) (models.JSONMap, error) {
	item, err := e.GetObjectMetadata(tenantID, engineID, objectKey)
	if err != nil {
		return nil, err
	}
	formatName := commonJSON.String(item.Attributes, "item", "format")
	if strings.TrimSpace(formatName) == "" {
		return nil, fmt.Errorf("item format is empty: %s", objectKey)
	}
	formatType := format.FormatType(strings.ToLower(strings.TrimSpace(formatName)))
	if !format.SupportsContentIndex(formatType) {
		return nil, fmt.Errorf("format %s does not support content index", formatType)
	}
	provider, err := format.GetTableInfoProvider(formatType)
	if err != nil {
		return nil, fmt.Errorf("format %s cannot build content index: %w", formatType, err)
	}

	tableInfo, err := provider.DescribeTable(context.Background(), objectReader, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build content index for %s: %w", objectKey, err)
	}
	index := tableInfo.ContentIndex
	if index == nil {
		return nil, fmt.Errorf("format %s did not return content index", formatType)
	}
	if index.Source == nil {
		index.Source = map[string]interface{}{}
	}
	if item.SizeBytes != nil {
		index.Source["size_bytes"] = *item.SizeBytes
	}
	if item.DataUpdatedAt != nil {
		index.Source["last_modified_at"] = item.DataUpdatedAt
	}
	if etag := commonJSON.String(item.Attributes, "storage", "etag"); etag != "" {
		index.Source["etag"] = etag
	}

	enhancedAttrs := cloneJSONMap(item.Attributes)
	metaattr.UpsertNested(enhancedAttrs, "content_index", "table", contentIndexAttributes(index))
	if tableInfo.Table != nil && len(tableInfo.Table.Fields) > 0 {
		metaattr.UpsertNested(enhancedAttrs, "type_info", "table", metaattr.TableInfoAttributes(tableInfo.Table))
	}
	enhancedAttrs = metaattr.Normalize(enhancedAttrs)

	if err := e.db.Model(item).Update("attributes", enhancedAttrs).Error; err != nil {
		return nil, err
	}
	return enhancedAttrs, nil
}

func cloneJSONMap(attrs models.JSONMap) models.JSONMap {
	cloned := make(models.JSONMap, len(attrs))
	for key, value := range attrs {
		cloned[key] = value
	}
	return cloned
}

func cloneInterfaceMap(attrs map[string]interface{}) map[string]interface{} {
	if len(attrs) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(attrs))
	for key, value := range attrs {
		cloned[key] = value
	}
	return cloned
}

func contentIndexAttributes(index *datatype.ContentIndex) map[string]interface{} {
	return map[string]interface{}{
		"kind":         index.Kind,
		"data_type":    index.DataType,
		"format":       index.Format,
		"unit":         index.Unit,
		"offset_unit":  index.OffsetUnit,
		"step":         index.Step,
		"row_count":    index.RowCount,
		"header_bytes": index.HeaderBytes,
		"source":       index.Source,
		"anchors":      indexAnchorsAttributes(index.Anchors),
	}
}

func indexAnchorsAttributes(anchors []datatype.ContentIndexAnchor) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(anchors))
	for _, anchor := range anchors {
		result = append(result, map[string]interface{}{
			"row":         anchor.Row,
			"byte_offset": anchor.ByteOffset,
		})
	}
	return result
}

func setExtractionAttribute(attrs models.JSONMap, key string, value interface{}) {
	if attrs == nil || key == "" || value == nil {
		return
	}
	metaattr.SetExtension(attrs, "extraction", key, value)
}
