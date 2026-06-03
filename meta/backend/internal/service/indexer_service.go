package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/metatext"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/search"
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
func (s *IndexerService) IndexTableAsset(resource *commonModels.Engine, tenantID uint, schemaName string, tableInfo datatype.TableInfo, fields []datatype.FieldInfo, item *models.MetaItem) {
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
			Name:         field.Name,
			DataType:     string(field.Type),
			ColumnType:   string(field.Type),
			Comment:      field.Comment,
			IsNullable:   field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
		})
	}

	record := &search.AssetRecord{
		AssetID:       item.Fingerprint,
		DocumentID:    item.Fingerprint,
		Locator:       metaItemLocator(resource.ID, resource.EngineType, "table", item.FullName, &item.ID),
		TenantID:      tenantID,
		EngineID:      resource.ID,
		EngineName:    resource.Name,
		EngineType:    resource.EngineType,
		AssetType:     "table",
		Name:          item.Name,
		FullName:      item.FullName,
		Schema:        schemaName,
		TableKind:     tableKindForIndex(tableInfo),
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

func tableKindForIndex(tableInfo datatype.TableInfo) string {
	if tableInfo.Kind != "" {
		return tableInfo.Kind
	}
	return "table"
}

// IndexCatalogAsset 索引 catalog single item 资产到 Meilisearch（统一索引）。
func (s *IndexerService) IndexCatalogAsset(resource *commonModels.Engine, tenantID, engineID uint, catalogResource metacatalog.StorageResource, relativePath, fullName string, item *models.MetaItem, extractedText string) bool {
	if s.indexer == nil || !s.indexer.Enabled() || resource == nil || item == nil {
		return false
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

	plainText := extractedText
	delete(metadata, "plain_text")

	// 准备文档内容字段
	truncatedContent := metatext.TruncateRunes(plainText, metatext.DocumentContentRuneLimit)
	contentPreview := metatext.PreviewText(truncatedContent, metatext.DocumentPreviewRuneLimit)

	dir, _ := splitCatalogResourcePath(catalogResource.Path)
	assetType := strings.TrimSpace(item.ItemType)
	if assetType == "" {
		assetType = "item"
	}

	// 构建统一的资产记录（包含文档内容字段）
	assetRecord := &search.AssetRecord{
		AssetID:       item.Fingerprint,
		DocumentID:    item.Fingerprint,
		ContentHash:   stringFromStandardAttributes(metadata, "storage", "content_hash"),
		Locator:       metaItemLocator(engineID, resource.EngineType, assetType, fullName, &item.ID),
		TenantID:      tenantID,
		EngineID:      engineID,
		EngineName:    resource.Name,
		EngineType:    resource.EngineType,
		AssetType:     assetType,
		Name:          item.Name,
		FullName:      fullName,
		Bucket:        catalogResource.RootName,
		Path:          dir,
		Metadata:      metadata,
		SizeBytes:     item.SizeBytes,
		DataUpdatedAt: catalogResource.LastModified,
		// 文档内容字段（深度扫描才有）
		Content:        truncatedContent,
		ContentPreview: contentPreview,
	}

	if len(tags) > 0 {
		assetRecord.Tags = tags
	}
	assetRecord.ContentType = commonJSON.String(metadata, "storage", "content_type")

	if value := stringFromStandardAttributes(metadata, "item", "format"); value != "" {
		assetRecord.DocumentType = value
	}
	if value := stringFromStandardAttributes(metadata, "type_info.document", "title"); value != "" {
		assetRecord.Title = value
	}
	if value := stringFromStandardAttributes(metadata, "format_info."+assetRecord.DocumentType, "author"); value != "" {
		assetRecord.Author = value
	}
	if keywords := stringSliceFromStandardAttributes(metadata, "capabilities.extraction", "keywords"); len(keywords) > 0 {
		assetRecord.Keywords = keywords
	}
	if wc := intFromStandardAttributes(metadata, "type_info.document", "word_count"); wc > 0 {
		assetRecord.WordCount = wc
	}
	if pc := intFromStandardAttributes(metadata, "type_info.document", "page_count"); pc > 0 {
		assetRecord.PageCount = pc
	}
	if created := timeFromStandardAttributes(metadata, "capabilities.extraction", "created_date"); created != nil {
		assetRecord.CreatedDate = created
	}
	if modified := timeFromStandardAttributes(metadata, "capabilities.extraction", "modified_date"); modified != nil {
		assetRecord.ModifiedDate = modified
	}

	// 统一索引（包含基础资产信息和文档内容）
	if err := s.indexer.IndexAsset(context.Background(), assetRecord); err != nil {
		s.log.Warn("索引 catalog 资产失败", "fingerprint", item.Fingerprint, "root", catalogResource.RootName, "path", catalogResource.Path, "error", err)
		return false
	}
	return true
}

func metaItemLocator(engineID uint, engineType, itemType, fullName string, itemID *uint) string {
	loc := resourcetree.LocatorFromFullName(engineID, engineType, itemType, fullName, itemID)
	if loc == nil {
		return ""
	}
	return loc.ToURI()
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

func stringFromStandardAttributes(metadata map[string]interface{}, sectionPath, key string) string {
	return getStringFromMap(standardAttributes(metadata, sectionPath), key)
}

func stringSliceFromStandardAttributes(metadata map[string]interface{}, sectionPath, key string) []string {
	return extractStringSlice(valueFromStandardAttributes(metadata, sectionPath, key))
}

func intFromStandardAttributes(metadata map[string]interface{}, sectionPath, key string) int {
	return intFromInterface(valueFromStandardAttributes(metadata, sectionPath, key))
}

func timeFromStandardAttributes(metadata map[string]interface{}, sectionPath, key string) *time.Time {
	return extractTimePtr(valueFromStandardAttributes(metadata, sectionPath, key))
}

func valueFromStandardAttributes(metadata map[string]interface{}, sectionPath, key string) interface{} {
	if section := standardAttributes(metadata, sectionPath); section != nil {
		return section[key]
	}
	return nil
}

func standardAttributes(metadata map[string]interface{}, sectionPath string) map[string]interface{} {
	return commonJSON.Section(metadata, sectionPath)
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
