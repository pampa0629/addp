package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/addp/common/format"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/search"
)

const (
	documentPreviewRuneLimit = 400
	documentContentRuneLimit = 20000
)

// IndexerService 负责管理 Meilisearch 索引
// 提取自 ScanService，消除循环依赖
type IndexerService struct {
	indexer *search.Indexer
	log     *slog.Logger
}

// NewIndexerService 创建索引服务
func NewIndexerService(indexer *search.Indexer, log *slog.Logger) *IndexerService {
	return &IndexerService{
		indexer: indexer,
		log:     log,
	}
}

// IndexTableAsset 索引表资产到 Meilisearch
func (s *IndexerService) IndexTableAsset(resource *commonModels.Engine, tenantID uint, schemaName string, tableInfo format.ScannerTableInfo, fields []format.ScannerFieldInfo, item *models.MetaItem) {
	if s.indexer == nil || !s.indexer.Enabled() || resource == nil || item == nil {
		return
	}

	metadata := search.NormalizeMap(copyJSONMap(item.Attributes))
	if metadata == nil {
		metadata = map[string]interface{}{}
	}
	delete(metadata, "fields")

	fieldRecords := make([]search.FieldRecord, 0, len(fields))
	for _, field := range fields {
		fieldRecords = append(fieldRecords, search.FieldRecord{
			Name:            field.Name,
			DataType:        field.DataType,
			ColumnType:      field.ColumnType,
			Comment:         field.Comment,
			OrdinalPosition: field.OrdinalPosition,
			IsNullable:      field.IsNullable,
			IsPrimaryKey:    field.IsPrimaryKey,
			IsUniqueKey:     field.IsUniqueKey,
		})
	}

	record := &search.AssetRecord{
		AssetID:       item.Fingerprint,
		TenantID:      tenantID,
		EngineID:      resource.ID,
		EngineName:    resource.Name,
		EngineType:    resource.EngineType,
		AssetType:     "table",
		Name:          item.Name,
		FullName:      item.FullName,
		Schema:        schemaName,
		TableType:     tableInfo.Type,
		Description:   tableInfo.Comment,
		RowCount:      item.RowCount,
		SizeBytes:     item.SizeBytes,
		Metadata:      metadata,
		Fields:        fieldRecords,
		DataUpdatedAt: item.DataUpdatedAt,
	}

	if err := s.indexer.IndexAsset(context.Background(), record); err != nil {
		s.log.Warn("索引表元数据失败", "fingerprint", item.Fingerprint, "schema", schemaName, "error", err)
	}
}

// IndexObjectAsset 索引对象资产到 Meilisearch（统一索引）
func (s *IndexerService) IndexObjectAsset(resource *commonModels.Engine, tenantID, engineID uint, meta format.ObjectMetadata, relativePath, fullName string, item *models.MetaItem) {
	if s.indexer == nil || !s.indexer.Enabled() || resource == nil || item == nil {
		return
	}

	attributes := copyJSONMap(item.Attributes)
	metadata := search.NormalizeMap(attributes)
	if metadata == nil {
		metadata = map[string]interface{}{}
	}

	tags := extractStringSlice(metadata["tags"])
	if len(tags) > 0 {
		delete(metadata, "tags")
	}

	plainText := ""
	if meta.ExtractedMetadata != nil && meta.ExtractedMetadata.CustomAttrs != nil {
		if v, ok := meta.ExtractedMetadata.CustomAttrs["plain_text"].(string); ok {
			plainText = v
		}
	}
	if plainText == "" {
		plainText = extractedPlainTextFromAttributes(metadata)
	}
	delete(metadata, "plain_text")

	// 准备文档内容字段
	truncatedContent := truncateRunes(plainText, documentContentRuneLimit)
	contentPreview := previewText(truncatedContent, documentPreviewRuneLimit)

	// 拆分meta.Path为目录和文件名
	// meta.Path 是完整路径（如 "image/开会.jpg"）
	dir, _ := commonModels.SplitObjectPath(meta.Path)

	// 构建统一的资产记录（包含文档内容字段）
	assetRecord := &search.AssetRecord{
		AssetID:       item.Fingerprint,
		TenantID:      tenantID,
		EngineID:      engineID,
		EngineName:    resource.Name,
		EngineType:    resource.EngineType,
		AssetType:     "object",
		Name:          item.Name,
		FullName:      fullName,
		Bucket:        meta.Bucket,
		Path:          dir, // 只存储目录路径（如 "image/"）
		Metadata:      metadata,
		SizeBytes:     item.SizeBytes,
		DataUpdatedAt: meta.LastModified,
		// 文档内容字段（深度扫描才有）
		Content:        truncatedContent,
		ContentPreview: contentPreview,
	}

	if len(tags) > 0 {
		assetRecord.Tags = tags
	}
	if desc := stringFromAttributes(metadata, "document", "file_type_friendly"); desc != "" {
		assetRecord.Description = desc
	}

	// 提取文档元数据
	if meta.ExtractedMetadata != nil {
		if basic := meta.ExtractedMetadata.BasicInfo; basic.ContentType != "" {
			assetRecord.ContentType = basic.ContentType
		}
	}
	if assetRecord.ContentType == "" {
		assetRecord.ContentType = commonJSON.String(metadata, "storage", "content_type")
	}

	if value := stringFromAttributes(metadata, "document", "document_type"); value != "" {
		assetRecord.DocumentType = value
	}
	if value := stringFromAttributes(metadata, "document", "title"); value != "" {
		assetRecord.Title = value
	}
	if value := stringFromAttributes(metadata, "document", "author"); value != "" {
		assetRecord.Author = value
	}
	if keywords := stringSliceFromAttributes(metadata, "document", "keywords"); len(keywords) > 0 {
		assetRecord.Keywords = keywords
	}
	if wc := intFromAttributes(metadata, "document", "word_count"); wc > 0 {
		assetRecord.WordCount = wc
	}
	if pc := intFromAttributes(metadata, "document", "page_count"); pc > 0 {
		assetRecord.PageCount = pc
	}
	if created := timeFromAttributes(metadata, "document", "created_date"); created != nil {
		assetRecord.CreatedDate = created
	}
	if modified := timeFromAttributes(metadata, "document", "modified_date"); modified != nil {
		assetRecord.ModifiedDate = modified
	}

	// 统一索引（包含基础资产信息和文档内容）
	if err := s.indexer.IndexAsset(context.Background(), assetRecord); err != nil {
		s.log.Warn("索引对象资产失败", "fingerprint", item.Fingerprint, "bucket", meta.Bucket, "path", meta.Path, "error", err)
	}
}

// DeleteTablesFromIndex 从索引中删除表
func (s *IndexerService) DeleteTablesFromIndex(tenantID, engineID uint, schemaName string) {
	if s.indexer == nil || !s.indexer.Enabled() || schemaName == "" {
		return
	}
	if err := s.indexer.DeleteTables(context.Background(), tenantID, engineID, schemaName); err != nil {
		s.log.Warn("删除表索引失败", "schema", schemaName, "engine_id", engineID, "error", err)
	}
}

// DeleteObjectsFromIndex 从索引中删除对象
func (s *IndexerService) DeleteObjectsFromIndex(tenantID, engineID uint, bucketName, path string) {
	if s.indexer == nil || !s.indexer.Enabled() || bucketName == "" {
		return
	}
	if err := s.indexer.DeleteObjects(context.Background(), tenantID, engineID, bucketName, path); err != nil {
		s.log.Warn("删除对象索引失败", "bucket", bucketName, "path", path, "error", err)
	}
}

// 辅助函数（从 scan_service.go 复制）

func copyJSONMap(m models.JSONMap) map[string]interface{} {
	if m == nil {
		return nil
	}
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

func cloneInterfaceMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		switch vv := v.(type) {
		case map[string]interface{}:
			result[k] = cloneInterfaceMap(vv)
		case []interface{}:
			arr := make([]interface{}, 0, len(vv))
			for _, item := range vv {
				switch nested := item.(type) {
				case map[string]interface{}:
					arr = append(arr, cloneInterfaceMap(nested))
				default:
					arr = append(arr, nested)
				}
			}
			result[k] = arr
		default:
			result[k] = vv
		}
	}
	return result
}

func extractStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if item != "" {
				out = append(out, item)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, raw := range v {
			if s, ok := raw.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

func getStringFromMap(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	if val, ok := metadata[key]; ok {
		switch v := val.(type) {
		case string:
			return v
		case fmt.Stringer:
			return v.String()
		case []byte:
			return string(v)
		default:
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

func stringFromAttributes(metadata map[string]interface{}, extensionNamespace, key string) string {
	return getStringFromMap(extensionAttributes(metadata, extensionNamespace), key)
}

func stringSliceFromAttributes(metadata map[string]interface{}, extensionNamespace, key string) []string {
	return extractStringSlice(valueFromExtension(metadata, extensionNamespace, key))
}

func intFromAttributes(metadata map[string]interface{}, extensionNamespace, key string) int {
	return intFromInterface(valueFromExtension(metadata, extensionNamespace, key))
}

func timeFromAttributes(metadata map[string]interface{}, extensionNamespace, key string) *time.Time {
	return extractTimePtr(valueFromExtension(metadata, extensionNamespace, key))
}

func extractedPlainTextFromAttributes(metadata map[string]interface{}) string {
	extraction := commonJSON.Section(metadata, "capabilities.extraction")
	if extraction == nil {
		return ""
	}
	extracted, ok := extraction["extracted_metadata"].(map[string]interface{})
	if !ok {
		return ""
	}
	customAttrs, ok := extracted["custom_attrs"].(map[string]interface{})
	if !ok {
		return ""
	}
	text, _ := customAttrs["plain_text"].(string)
	return text
}

func valueFromExtension(metadata map[string]interface{}, namespace, key string) interface{} {
	if section := standardAttributeSection(metadata, namespace); section != nil {
		return section[key]
	}
	return nil
}

func extensionAttributes(metadata map[string]interface{}, namespace string) map[string]interface{} {
	return standardAttributeSection(metadata, namespace)
}

func standardAttributeSection(metadata map[string]interface{}, namespace string) map[string]interface{} {
	if metadata == nil || namespace == "" {
		return nil
	}
	for _, root := range standardAttributeRoots(namespace) {
		if section := commonJSON.Section(metadata, root+"."+namespace); section != nil {
			return section
		}
	}
	return nil
}

func standardAttributeRoots(namespace string) []string {
	switch namespace {
	case "media", "document", "container", "table", "graph":
		return []string{"type_info"}
	case "spatial", "statistics", "extraction", "semantic", "temporal", "partitioning", "indexing":
		return []string{"capabilities"}
	default:
		return []string{"format_info"}
	}
}

func intFromInterface(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case json.Number:
		if i64, err := v.Int64(); err == nil {
			return int(i64)
		}
	case string:
		if v == "" {
			return 0
		}
		if i64, err := strconv.ParseInt(v, 10, 64); err == nil {
			return int(i64)
		}
	}
	return 0
}

func extractTimePtr(value interface{}) *time.Time {
	switch v := value.(type) {
	case time.Time:
		if v.IsZero() {
			return nil
		}
		vv := v
		return &vv
	case *time.Time:
		if v == nil || v.IsZero() {
			return nil
		}
		vv := v.UTC()
		return &vv
	case string:
		if v == "" {
			return nil
		}
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			return &parsed
		}
	}
	return nil
}
