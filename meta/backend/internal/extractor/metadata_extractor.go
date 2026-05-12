package extractor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	pathpkg "path"
	"strings"
	"time"

	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/metatext"
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
	meta format.ObjectMetadata,
	baseAttrs models.JSONMap,
) models.JSONMap {
	// 如果 S3Scanner 已经提取了元数据，直接使用。
	if meta.ExtractedMetadata != nil && meta.ExtractedMetadata.CustomAttrs != nil {
		if text, ok := meta.ExtractedMetadata.CustomAttrs["plain_text"].(string); ok && text != "" {
			setExtractionAttribute(baseAttrs, "plain_text_preview", metatext.PreviewText(text, metatext.DocumentPreviewRuneLimit))
		}
		applyExtractedMetadataExtensions(baseAttrs, meta.ExtractedMetadata)

		setExtractionAttribute(baseAttrs, "metadata_extracted", true)
		setExtractionAttribute(baseAttrs, "extracted_metadata", buildExtractedMetadataPayload(meta.ExtractedMetadata))
		if meta.ExtractedMetadata.BasicInfo.ContentType != "" {
			metaattr.SetStorage(baseAttrs, "content_type", meta.ExtractedMetadata.BasicInfo.ContentType)
		}

		return baseAttrs
	}

	contentType := mime.TypeByExtension(pathpkg.Ext(meta.Path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	extractor := format.GetExtractor(contentType)
	if extractor == nil {
		return baseAttrs
	}

	setExtractionAttribute(baseAttrs, "extractor_available", true)
	metaattr.SetStorage(baseAttrs, "content_type", contentType)

	return baseAttrs
}

// ExtractEnhancedMetadataWithCache 带缓存检查的元数据提取。
// 如果文件未修改（基于 last_modified 时间），跳过重新提取。
// fullPath: 文件在 bucket 中的完整路径（用于 fingerprint 生成）。
func (e *MetadataExtractor) ExtractEnhancedMetadataWithCache(
	engineID uint,
	meta format.ObjectMetadata,
	baseAttrs models.JSONMap,
	fullPath string,
) models.JSONMap {
	bucket := commonJSON.String(baseAttrs, "storage", "bucket")
	dir, name := commonModels.SplitObjectPath(fullPath)
	fullName := commonModels.JoinObjectPath(bucket, dir, name)
	fingerprint := commonModels.GenerateItemFingerprint(engineID, fullName)

	var existingItem models.MetaItem
	err := e.db.Where("fingerprint = ?", fingerprint).First(&existingItem).Error

	if err == nil && existingItem.DataUpdatedAt != nil && meta.LastModified != nil {
		existingTime := existingItem.DataUpdatedAt.Truncate(time.Second)
		newTime := meta.LastModified.Truncate(time.Second)

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

	return e.ExtractEnhancedMetadata(engineID, meta, baseAttrs)
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
) (*format.ExtractedMetadata, error) {
	contentType := mime.TypeByExtension(pathpkg.Ext(objectKey))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	extractor := format.GetExtractor(contentType)
	if extractor == nil {
		return nil, fmt.Errorf("no extractor available for content type: %s", contentType)
	}

	item, err := e.GetObjectMetadata(tenantID, engineID, objectKey)
	if err != nil {
		logger.L().Warn("对象元数据不存在，使用默认值", "object_key", objectKey, "error", err)
	}

	input := format.ExtractInput{
		ObjectKey:   objectKey,
		ContentType: contentType,
		Reader:      objectReader,
	}

	if item != nil {
		if item.SizeBytes != nil {
			input.Size = *item.SizeBytes
		}
		if item.DataUpdatedAt != nil {
			input.LastModified = *item.DataUpdatedAt
		}
		if etag := commonJSON.String(item.Attributes, "storage", "etag"); etag != "" {
			input.ETag = etag
		}
	}

	metadata, err := extractor.Extract(context.Background(), input)
	if err != nil {
		return nil, fmt.Errorf("failed to extract metadata: %w", err)
	}

	if item != nil {
		enhancedAttrs := item.Attributes
		if enhancedAttrs == nil {
			enhancedAttrs = make(models.JSONMap)
		}

		setExtractionAttribute(enhancedAttrs, "metadata_extracted", true)
		setExtractionAttribute(enhancedAttrs, "extracted_metadata", buildExtractedMetadataPayload(metadata))
		if metadata.SchemaInfo != nil {
			setExtractionAttribute(enhancedAttrs, "schema_info", metadata.SchemaInfo)
		}
		applyExtractedMetadataExtensions(enhancedAttrs, metadata)
		enhancedAttrs = metaattr.Normalize(enhancedAttrs)

		if err := e.db.Model(item).Update("attributes", enhancedAttrs).Error; err != nil {
			e.log.Warn("更新元数据失败", "item_id", item.ID, "error", err)
		}
	}

	return metadata, nil
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
	indexInfo := tableInfo.GetContentIndexInfo()
	if indexInfo == nil || indexInfo.Table == nil {
		return nil, fmt.Errorf("format %s did not return content index", formatType)
	}
	index := indexInfo.Table
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
	if len(tableInfo.Fields) > 0 {
		tableAttrs := map[string]interface{}{
			"fields": metaattr.FieldAttributesFromFormat(tableInfo.Fields),
		}
		if tableInfo.RowCount != nil {
			tableAttrs["row_count"] = *tableInfo.RowCount
		}
		metaattr.UpsertNested(enhancedAttrs, "type_info", "table", tableAttrs)
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

func contentIndexAttributes(index *format.ContentIndex) map[string]interface{} {
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

func indexAnchorsAttributes(anchors []format.ContentIndexAnchor) []map[string]interface{} {
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

func buildExtractedMetadataPayload(metadata *format.ExtractedMetadata) map[string]interface{} {
	if metadata == nil {
		return nil
	}
	payload := map[string]interface{}{
		"basic_info":   metadata.BasicInfo,
		"custom_attrs": metadata.CustomAttrs,
	}
	if metadata.SchemaInfo != nil {
		payload["schema_info"] = metadata.SchemaInfo
	}
	return payload
}

func applyExtractedMetadataExtensions(attrs models.JSONMap, metadata *format.ExtractedMetadata) {
	if attrs == nil || metadata == nil {
		return
	}
	if metadata.BasicInfo.FileType != "" || metadata.BasicInfo.Encoding != "" {
		document := map[string]interface{}{}
		if metadata.BasicInfo.FileType != "" {
			document["file_type_friendly"] = metadata.BasicInfo.FileType
		}
		if metadata.BasicInfo.Encoding != "" {
			document["encoding"] = metadata.BasicInfo.Encoding
		}
		metaattr.UpsertNested(attrs, "type_info", "document", document)
	}
	for key, value := range metadata.CustomAttrs {
		if key == "plain_text" {
			continue
		}
		switch standardExtensionForMetadataKey(key) {
		case "media":
			metaattr.SetExtension(attrs, "media", key, value)
		case "document":
			metaattr.SetExtension(attrs, "document", key, value)
		case "statistics":
			metaattr.SetExtension(attrs, "statistics", key, value)
		case "spatial":
			if values, ok := value.(map[string]interface{}); ok {
				metaattr.UpsertNested(attrs, "capabilities", "spatial", values)
			}
		default:
			metaattr.SetExtension(attrs, "unqualified", key, value)
		}
	}
	if metadata.SchemaInfo != nil {
		metaattr.SetExtension(attrs, "statistics", "row_count", metadata.SchemaInfo.RowCount)
		metaattr.SetExtension(attrs, "statistics", "column_count", len(metadata.SchemaInfo.Columns))
	}
}

func standardExtensionForMetadataKey(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "kind", "width", "height", "format", "color_space", "color_mode", "bit_depth", "has_alpha",
		"duration", "duration_seconds", "codec", "frame_rate", "sample_rate", "channels":
		return "media"
	case "document_type", "title", "author", "keywords", "page_count", "word_count",
		"char_count", "created_date", "modified_date", "file_type_friendly", "encoding":
		return "document"
	case "row_count", "column_count", "feature_count", "object_count", "record_count":
		return "statistics"
	case "spatial":
		return "spatial"
	default:
		return ""
	}
}
