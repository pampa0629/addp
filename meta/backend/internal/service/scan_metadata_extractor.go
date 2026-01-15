package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime"
	pathpkg "path"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/format"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// MetadataExtractor 元数据提取器，负责对象元数据的提取、缓存和查询
type MetadataExtractor struct {
	db  *gorm.DB
	log *slog.Logger
}

// NewMetadataExtractor 创建元数据提取器实例
func NewMetadataExtractor(db *gorm.DB) *MetadataExtractor {
	return &MetadataExtractor{
		db:  db,
		log: logger.With("component", "metadata_extractor"),
	}
}

// ============================================================================
// 核心元数据提取方法
// ============================================================================

// ExtractEnhancedMetadata 使用插件提取增强的元数据
func (e *MetadataExtractor) ExtractEnhancedMetadata(
	engineID uint,
	meta format.ObjectMetadata,
	baseAttrs models.JSONMap,
) models.JSONMap {
	// 如果 S3Scanner 已经提取了元数据，直接使用
	if meta.ExtractedMetadata != nil && meta.ExtractedMetadata.CustomAttrs != nil {
		if text, ok := meta.ExtractedMetadata.CustomAttrs["plain_text"].(string); ok && text != "" {
			baseAttrs["plain_text_preview"] = previewText(text, documentPreviewRuneLimit)
		}
		// 将提取的 CustomAttrs 合并到 baseAttrs 中，跳过大字段
		for key, value := range meta.ExtractedMetadata.CustomAttrs {
			if key == "plain_text" {
				continue
			}
			baseAttrs[key] = value
		}

		// 添加基本信息
		baseAttrs["metadata_extracted"] = true
		baseAttrs["file_type_friendly"] = meta.ExtractedMetadata.BasicInfo.FileType
		if meta.ExtractedMetadata.BasicInfo.ContentType != "" {
			baseAttrs["content_type"] = meta.ExtractedMetadata.BasicInfo.ContentType
		}

		return baseAttrs
	}

	// 如果没有提取元数据，检查是否有可用的提取器
	contentType := mime.TypeByExtension(pathpkg.Ext(meta.Path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 获取适配的元数据提取器
	extractor := format.GetExtractor(contentType)
	if extractor == nil {
		// 没有合适的提取器，返回基础属性
		return baseAttrs
	}

	// 标记有提取器可用（但本次扫描未提取）
	baseAttrs["extractor_available"] = true
	baseAttrs["content_type"] = contentType

	return baseAttrs
}

// ExtractEnhancedMetadataWithCache 带缓存检查的元数据提取
// 如果文件未修改（基于 last_modified 时间），跳过重新提取
// fullPath: 文件在 bucket 中的完整路径（用于 fingerprint 生成）
func (e *MetadataExtractor) ExtractEnhancedMetadataWithCache(
	engineID uint,
	meta format.ObjectMetadata,
	baseAttrs models.JSONMap,
	fullPath string,
) models.JSONMap {
	// 生成 fingerprint 以查找已有记录 - 使用传入的 fullPath 确保一致性
	bucket, _ := baseAttrs["bucket"].(string)
	// 拆分完整路径为目录和文件名
	dir, name := commonModels.SplitObjectPath(fullPath)
	// 两步计算方式：先拼接 full_name，再计算指纹
	fullName := commonModels.JoinObjectPath(bucket, dir, name)
	fingerprint := commonModels.GenerateItemFingerprint(engineID, fullName)

	// 查找已有的 meta_item 记录
	var existingItem models.MetaItem
	err := e.db.Where("fingerprint = ?", fingerprint).First(&existingItem).Error

	// 如果找到已有记录，检查 last_modified 是否变化
	if err == nil && existingItem.DataUpdatedAt != nil && meta.LastModified != nil {
		// 比较时间（精确到秒）
		existingTime := existingItem.DataUpdatedAt.Truncate(time.Second)
		newTime := meta.LastModified.Truncate(time.Second)

		if existingTime.Equal(newTime) {
			// 文件未变化，复用已有的 metadata
			if existingItem.Attributes != nil && len(existingItem.Attributes) > 0 {
				// 检查是否已经提取过元数据
				if extracted, ok := existingItem.Attributes["metadata_extracted"].(bool); ok && extracted {
					e.log.Debug("复用缓存的元数据",
						"fingerprint", fingerprint,
						"fullPath", fullPath,
						"last_modified", existingTime)
					// 将已有的 attributes 合并到 baseAttrs
					for key, value := range existingItem.Attributes {
						baseAttrs[key] = value
					}
					return baseAttrs
				}
			}
		}
	}

	// 文件是新的或已更新，执行完整的元数据提取
	return e.ExtractEnhancedMetadata(engineID, meta, baseAttrs)
}

// ============================================================================
// 元数据查询方法
// ============================================================================

// GetObjectMetadata 获取指定对象的元数据
// 用于 Manager 模块预览时查询已扫描的元数据
func (e *MetadataExtractor) GetObjectMetadata(
	tenantID, engineID uint,
	objectKey string,
) (*models.MetaItem, error) {
	// 解析对象路径
	bucket, relativePath := splitObjectPath(objectKey)
	if bucket == "" {
		return nil, fmt.Errorf("invalid object key: %s", objectKey)
	}

	// 查找 bucket 节点
	var bucketNode models.MetaNode
	err := e.db.Where("tenant_id = ? AND engine_id = ? AND node_type = ? AND name = ?",
		tenantID, engineID, "bucket", bucket).First(&bucketNode).Error
	if err != nil {
		return nil, fmt.Errorf("bucket not found: %w", err)
	}

	// 查找对象 item
	objectName := pathpkg.Base(relativePath)
	if objectName == "" {
		objectName = relativePath
	}

	var item models.MetaItem

	// 如果有相对路径，需要先找到对应的 prefix 节点
	if relativePath != "" && relativePath != objectName {
		// 构建 prefix 路径
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

		// 在最终的 prefix 节点下查找对象
		err = e.db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND name = ?",
			tenantID, engineID, currentParent.ID, "object", objectName).First(&item).Error
	} else {
		// 直接在 bucket 下查找对象
		err = e.db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND name = ?",
			tenantID, engineID, bucketNode.ID, "object", objectName).First(&item).Error
	}

	if err != nil {
		return nil, fmt.Errorf("object metadata not found: %w", err)
	}

	return &item, nil
}

// ExtractObjectMetadataOnDemand 按需提取对象的深度元数据
// 当 Manager 预览时发现元数据未提取，可以调用此方法触发实时提取
func (e *MetadataExtractor) ExtractObjectMetadataOnDemand(
	tenantID, engineID uint,
	objectKey string,
	token string,
	objectReader io.Reader,
) (*format.ExtractedMetadata, error) {
	// 检测内容类型
	contentType := mime.TypeByExtension(pathpkg.Ext(objectKey))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 获取元数据提取器
	extractor := format.GetExtractor(contentType)
	if extractor == nil {
		return nil, fmt.Errorf("no extractor available for content type: %s", contentType)
	}

	// 获取对象的基本信息
	item, err := e.GetObjectMetadata(tenantID, engineID, objectKey)
	if err != nil {
		// 如果元数据不存在，使用默认值
		logger.L().Warn("对象元数据不存在，使用默认值", "object_key", objectKey, "error", err)
	}

	// 构建提取输入
	input := format.ExtractInput{
		EngineID:    engineID,
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
		if etag, ok := item.Attributes["etag"].(string); ok {
			input.ETag = etag
		}
	}

	// 调用提取器
	ctx := context.Background()
	metadata, err := extractor.Extract(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to extract metadata: %w", err)
	}

	// 如果元数据 item 存在，更新 attributes
	if item != nil {
		enhancedAttrs := item.Attributes
		if enhancedAttrs == nil {
			enhancedAttrs = make(models.JSONMap)
		}

		// 合并提取的元数据到 attributes
		enhancedAttrs["extracted_metadata"] = map[string]interface{}{
			"basic_info":   metadata.BasicInfo,
			"custom_attrs": metadata.CustomAttrs,
		}

		if metadata.SchemaInfo != nil {
			enhancedAttrs["schema_info"] = metadata.SchemaInfo
		}

		// 更新数据库
		if err := e.db.Model(item).Update("attributes", enhancedAttrs).Error; err != nil {
			e.log.Warn("更新元数据失败", "item_id", item.ID, "error", err)
		}
	}

	return metadata, nil
}

// ============================================================================
// 对象路径处理辅助函数
// ============================================================================

// sanitizeObjectPath 清理对象路径（去除前后空格和斜杠）
func sanitizeObjectPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, "/")
	return path
}

// splitObjectPath 分割对象路径为 bucket 和相对路径
// 例如："my-bucket/folder/file.txt" -> ("my-bucket", "folder/file.txt")
func splitObjectPath(path string) (string, string) {
	clean := sanitizeObjectPath(path)
	if clean == "" {
		return "", ""
	}
	parts := strings.SplitN(clean, "/", 2)
	bucket := parts[0]
	if bucket == "" {
		return "", ""
	}
	if len(parts) == 1 {
		return bucket, ""
	}
	return bucket, parts[1]
}

// prepareObjectPaths 准备对象路径列表（去重、排序）
// 优先级：paths > fallback > scanner.AllowedBuckets()
func prepareObjectPaths(
	paths, fallback []string,
	scanner format.ObjectStorageScanner,
) []string {
	pathSet := map[string]struct{}{}
	for _, p := range paths {
		clean := sanitizeObjectPath(p)
		if clean != "" {
			pathSet[clean] = struct{}{}
		}
	}

	if len(pathSet) == 0 {
		for _, p := range fallback {
			clean := sanitizeObjectPath(p)
			if clean != "" {
				pathSet[clean] = struct{}{}
			}
		}
	}

	if len(pathSet) == 0 {
		for _, bucket := range scanner.AllowedBuckets() {
			clean := sanitizeObjectPath(bucket)
			if clean != "" {
				pathSet[clean] = struct{}{}
			}
		}
	}

	var result []string
	for p := range pathSet {
		result = append(result, p)
	}
	sort.Strings(result)
	return result
}

// ============================================================================
// 文本处理辅助函数
// ============================================================================

// truncateRunes 截断字符串到指定 rune 数量
func truncateRunes(text string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// previewText 生成文本预览（截断到指定长度）
func previewText(text string, limit int) string {
	return truncateRunes(text, limit)
}
