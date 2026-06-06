package search

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// IndexAsset 写入/更新资产信息（包含表、对象和文档内容）
func (i *Indexer) IndexAsset(ctx context.Context, record *AssetRecord) error {
	if !i.Enabled() || record == nil || record.AssetID == "" {
		return nil
	}

	record.UpdatedAt = time.Now().UTC()

	// 将 record 转换为 map (Meilisearch 需要主键字段)
	doc := map[string]interface{}{
		"id":              record.AssetID, // Meilisearch 主键
		"asset_id":        record.AssetID,
		"document_id":     record.DocumentID,
		"content_hash":    record.ContentHash,
		"locator":         record.Locator,
		"tenant_id":       record.TenantID,
		"engine_id":       record.EngineID,
		"engine_name":     record.EngineName,
		"engine_type":     record.EngineType,
		"asset_type":      record.AssetType,
		"name":            record.Name,
		"full_name":       record.FullName,
		"schema":          record.Schema,
		"table_kind":      record.TableKind,
		"bucket":          record.Bucket,
		"path":            record.Path, // 目录路径
		"description":     record.Description,
		"tags":            record.Tags,
		"row_count":       record.RowCount,
		"size_bytes":      record.SizeBytes,
		"content_type":    record.ContentType,
		"data_updated_at": record.DataUpdatedAt,
		"metadata":        record.Metadata,
		"fields":          record.Fields,
		// 文档内容字段（深度扫描才有）
		"content":         record.Content,
		"content_preview": record.ContentPreview,
		"document_type":   record.DocumentType,
		"title":           record.Title,
		"author":          record.Author,
		"keywords":        record.Keywords,
		"word_count":      record.WordCount,
		"page_count":      record.PageCount,
		"created_date":    record.CreatedDate,
		"modified_date":   record.ModifiedDate,
		"updated_at":      record.UpdatedAt,
	}

	// 单条写入
	index := i.client.Index(i.assetIndex)
	task, err := index.AddDocuments([]map[string]interface{}{doc})
	if err != nil {
		return fmt.Errorf("failed to index asset: %w", err)
	}

	i.log.Debug("资产已索引",
		"asset_id", record.AssetID,
		"asset_type", record.AssetType,
		"name", record.Name,
		"task_uid", task.TaskUID,
	)

	return nil
}

// DeleteObjects 删除指定 Bucket/路径下的对象索引
func (i *Indexer) DeleteObjects(ctx context.Context, tenantID, engineID uint, bucket, path string) error {
	if !i.Enabled() {
		return nil
	}

	// 构建过滤条件
	filters := []string{
		fmt.Sprintf("tenant_id = %d", tenantID),
		fmt.Sprintf("engine_id = %d", engineID),
		fmt.Sprintf("asset_type = 'object'"),
	}

	if bucket != "" {
		filters = append(filters, fmt.Sprintf("bucket = '%s'", escapeFilterValue(bucket)))
	}

	if path != "" {
		// path 是目录路径（以/结尾），使用前缀匹配
		filters = append(filters, fmt.Sprintf("path ^= '%s'", escapeFilterValue(path)))
	}

	filterStr := strings.Join(filters, " AND ")

	// 删除资产索引中的记录
	assetIndex := i.client.Index(i.assetIndex)
	task, err := assetIndex.DeleteDocumentsByFilter(filterStr)
	if err != nil {
		return fmt.Errorf("failed to delete objects from asset index: %w", err)
	}

	i.log.Info("对象资产已删除",
		"tenant_id", tenantID,
		"engine_id", engineID,
		"bucket", bucket,
		"path", path,
		"task_uid", task.TaskUID,
	)

	return nil
}

// escapeFilterValue 转义过滤值中的特殊字符
func escapeFilterValue(value string) string {
	// Meilisearch 过滤字符串需要转义单引号
	return strings.ReplaceAll(value, "'", "\\'")
}

// DeleteTables 删除某租户资源下指定 Schema 的表索引
func (i *Indexer) DeleteTables(ctx context.Context, tenantID, engineID uint, schemaName string) error {
	if !i.Enabled() || i.assetIndex == "" {
		return nil
	}

	filters := []string{
		fmt.Sprintf("tenant_id = %d", tenantID),
		fmt.Sprintf("engine_id = %d", engineID),
		fmt.Sprintf("asset_type = 'table'"),
	}

	if schemaName != "" {
		filters = append(filters, fmt.Sprintf("schema = '%s'", escapeFilterValue(schemaName)))
	}

	filterStr := strings.Join(filters, " AND ")

	index := i.client.Index(i.assetIndex)
	task, err := index.DeleteDocumentsByFilter(filterStr)
	if err != nil {
		return fmt.Errorf("failed to delete tables: %w", err)
	}

	i.log.Info("表资产已删除",
		"tenant_id", tenantID,
		"engine_id", engineID,
		"schema", schemaName,
		"task_uid", task.TaskUID,
	)

	return nil
}

// NormalizeMap 递归转换 map 中的时间等类型，便于 JSON 序列化
func NormalizeMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	out := make(map[string]interface{}, len(input))
	for k, v := range input {
		out[k] = normalizeValue(v)
	}
	return out
}

func normalizeValue(value interface{}) interface{} {
	switch val := value.(type) {
	case time.Time:
		return val.UTC()
	case *time.Time:
		if val == nil {
			return nil
		}
		return val.UTC()
	case map[string]interface{}:
		return NormalizeMap(val)
	case []interface{}:
		arr := make([]interface{}, 0, len(val))
		for _, item := range val {
			arr = append(arr, normalizeValue(item))
		}
		return arr
	default:
		return value
	}
}
