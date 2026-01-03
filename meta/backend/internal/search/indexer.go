package search

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/meta/internal/config"
	"github.com/meilisearch/meilisearch-go"
	"log/slog"
)

// Meilisearch 不需要预定义 mapping，在 ensureIndexes 中通过 API 配置

// Indexer 封装 Meilisearch 操作
type Indexer struct {
	client        *meilisearch.Client
	assetIndex    string
	documentIndex string
	enabled       bool
	mu            sync.RWMutex
	log           *slog.Logger
}

// FieldRecord 用于索引字段信息
type FieldRecord struct {
	Name            string `json:"name"`
	DataType        string `json:"data_type,omitempty"`
	ColumnType      string `json:"column_type,omitempty"`
	Comment         string `json:"comment,omitempty"`
	OrdinalPosition int    `json:"ordinal_position,omitempty"`
	IsNullable      bool   `json:"is_nullable,omitempty"`
	IsPrimaryKey    bool   `json:"is_primary_key,omitempty"`
	IsUniqueKey     bool   `json:"is_unique_key,omitempty"`
}

// AssetRecord 描述结构化或对象资产
type AssetRecord struct {
	AssetID       string                 `json:"asset_id"`
	TenantID      uint                   `json:"tenant_id"`
	EngineID      uint                   `json:"engine_id"`
	ResourceName  string                 `json:"resource_name,omitempty"`
	ResourceType  string                 `json:"resource_type,omitempty"`
	AssetType     string                 `json:"asset_type"`
	Name          string                 `json:"name"`
	FullName      string                 `json:"full_name,omitempty"`
	Schema        string                 `json:"schema,omitempty"`
	TableType     string                 `json:"table_type,omitempty"`
	Bucket        string                 `json:"bucket,omitempty"`
	Path          string                 `json:"path,omitempty"`
	RelativePath  string                 `json:"relative_path,omitempty"`
	Description   string                 `json:"description,omitempty"`
	Tags          []string               `json:"tags,omitempty"`
	RowCount      *int64                 `json:"row_count,omitempty"`
	SizeBytes     *int64                 `json:"size_bytes,omitempty"`
	DataUpdatedAt *time.Time             `json:"data_updated_at,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	Fields        []FieldRecord          `json:"fields,omitempty"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

// DocumentRecord 描述可全文搜索的文档数据
type DocumentRecord struct {
	DocumentID     string                 `json:"document_id"`
	AssetID        string                 `json:"asset_id"`
	TenantID       uint                   `json:"tenant_id"`
	EngineID       uint                   `json:"engine_id"`
	ResourceName   string                 `json:"resource_name,omitempty"`
	ResourceType   string                 `json:"resource_type,omitempty"`
	Bucket         string                 `json:"bucket,omitempty"`
	RelativePath   string                 `json:"relative_path,omitempty"`
	FileName       string                 `json:"file_name"`
	FilePath       string                 `json:"file_path,omitempty"`
	DocumentType   string                 `json:"document_type,omitempty"`
	Title          string                 `json:"title,omitempty"`
	Author         string                 `json:"author,omitempty"`
	Keywords       []string               `json:"keywords,omitempty"`
	Content        string                 `json:"content"`
	ContentPreview string                 `json:"content_preview,omitempty"`
	ContentType    string                 `json:"content_type,omitempty"`
	FileSize       int64                  `json:"file_size,omitempty"`
	WordCount      int                    `json:"word_count,omitempty"`
	PageCount      int                    `json:"page_count,omitempty"`
	LastModified   *time.Time             `json:"last_modified,omitempty"`
	CreatedDate    *time.Time             `json:"created_date,omitempty"`
	ModifiedDate   *time.Time             `json:"modified_date,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	UpdatedAt      time.Time              `json:"updated_at"`
}

// NewIndexer 创建索引器（若未配置 Meilisearch URL，则返回禁用状态）
func NewIndexer(cfg *config.Config) (*Indexer, error) {
	idx := &Indexer{
		assetIndex:    strings.TrimSpace(cfg.MeilisearchAssetIndex),
		documentIndex: strings.TrimSpace(cfg.MeilisearchDocumentIndex),
		enabled:       strings.TrimSpace(cfg.MeilisearchURL) != "",
		log:           logger.With("component", "meta_indexer"),
	}

	if !idx.enabled {
		idx.log.Info("Meilisearch 未配置，索引功能已禁用")
		return idx, nil
	}

	// 创建 Meilisearch 客户端
	client := meilisearch.NewClient(meilisearch.ClientConfig{
		Host:   cfg.MeilisearchURL,
		APIKey: cfg.MeilisearchMasterKey,
	})
	idx.client = client

	// 初始化索引配置
	if err := idx.ensureIndexes(); err != nil {
		return nil, fmt.Errorf("failed to initialize indexes: %w", err)
	}

	idx.log.Info("Meilisearch 索引器已启用",
		"asset_index", idx.assetIndex,
		"document_index", idx.documentIndex,
	)

	return idx, nil
}

// Enabled 判断是否启用了索引功能
func (i *Indexer) Enabled() bool {
	return i != nil && i.enabled && i.client != nil
}

// ensureIndexes 确保 Meilisearch 索引存在并配置正确
func (i *Indexer) ensureIndexes() error {
	// 确保资产索引存在并设置主键
	_, err := i.client.CreateIndex(&meilisearch.IndexConfig{
		Uid:        i.assetIndex,
		PrimaryKey: "id",
	})
	// 忽略索引已存在的错误
	if err != nil && !strings.Contains(err.Error(), "index_already_exists") {
		return fmt.Errorf("failed to create asset index: %w", err)
	}

	// 配置资产索引
	assetIndex := i.client.Index(i.assetIndex)

	// 设置可搜索字段
	_, err = assetIndex.UpdateSearchableAttributes(&[]string{
		"name",
		"full_name",
		"description",
		"tags",
		"fields.name",    // 嵌套字段搜索
		"fields.comment",
	})
	if err != nil {
		return fmt.Errorf("failed to update asset searchable attributes: %w", err)
	}

	// 设置可过滤字段
	_, err = assetIndex.UpdateFilterableAttributes(&[]string{
		"tenant_id",
		"engine_id",
		"resource_type",
		"asset_type",
		"schema",
		"bucket",
		"table_type",
	})
	if err != nil {
		return fmt.Errorf("failed to update asset filterable attributes: %w", err)
	}

	// 设置可排序字段
	_, err = assetIndex.UpdateSortableAttributes(&[]string{
		"last_modified",
		"size_bytes",
		"row_count",
	})
	if err != nil {
		return fmt.Errorf("failed to update asset sortable attributes: %w", err)
	}

	// 配置文档索引
	if i.documentIndex != "" {
		// 确保文档索引存在并设置主键
		_, err := i.client.CreateIndex(&meilisearch.IndexConfig{
			Uid:        i.documentIndex,
			PrimaryKey: "id",
		})
		// 忽略索引已存在的错误
		if err != nil && !strings.Contains(err.Error(), "index_already_exists") {
			return fmt.Errorf("failed to create document index: %w", err)
		}

		docIndex := i.client.Index(i.documentIndex)

		_, err = docIndex.UpdateSearchableAttributes(&[]string{
			"title",
			"file_name",
			"content",
			"content_preview",
			"keywords",
			"author",
		})
		if err != nil {
			return fmt.Errorf("failed to update document searchable attributes: %w", err)
		}

		_, err = docIndex.UpdateFilterableAttributes(&[]string{
			"tenant_id",
			"engine_id",
			"resource_type",
			"document_type",
			"bucket",
		})
		if err != nil {
			return fmt.Errorf("failed to update document filterable attributes: %w", err)
		}
	}

	i.log.Info("索引配置已更新",
		"asset_index", i.assetIndex,
		"document_index", i.documentIndex,
	)

	return nil
}

// IndexAsset 写入/更新资产信息
func (i *Indexer) IndexAsset(ctx context.Context, record *AssetRecord) error {
	if !i.Enabled() || record == nil || record.AssetID == "" {
		return nil
	}

	record.UpdatedAt = time.Now().UTC()

	// 将 record 转换为 map (Meilisearch 需要主键字段)
	doc := map[string]interface{}{
		"id":                 record.AssetID, // Meilisearch 主键
		"asset_id":           record.AssetID,
		"tenant_id":          record.TenantID,
		"engine_id":        record.EngineID,
		"resource_name":      record.ResourceName,
		"resource_type":      record.ResourceType,
		"asset_type":         record.AssetType,
		"name":               record.Name,
		"full_name":          record.FullName,
		"schema":             record.Schema,
		"table_type":         record.TableType,
		"bucket":             record.Bucket,
		"path":               record.Path,
		"relative_path":      record.RelativePath,
		"description":        record.Description,
		"tags":               record.Tags,
		"row_count":          record.RowCount,
		"size_bytes":         record.SizeBytes,
		"data_updated_at":    record.DataUpdatedAt,
		"metadata":           record.Metadata,
		"fields":             record.Fields,
		"updated_at":         record.UpdatedAt,
	}

	// 单条写入
	index := i.client.Index(i.assetIndex)
	task, err := index.AddDocuments([]map[string]interface{}{doc})
	if err != nil {
		return fmt.Errorf("failed to index asset: %w", err)
	}

	i.log.Debug("资产已索引",
		"asset_id", record.AssetID,
		"task_uid", task.TaskUID,
	)

	return nil
}

// IndexDocument 写入/更新文档全文索引
func (i *Indexer) IndexDocument(ctx context.Context, record *DocumentRecord) error {
	if !i.Enabled() || i.documentIndex == "" {
		return nil
	}

	record.UpdatedAt = time.Now().UTC()

	doc := map[string]interface{}{
		"id":              record.DocumentID, // Meilisearch 主键
		"document_id":     record.DocumentID,
		"asset_id":        record.AssetID,
		"tenant_id":       record.TenantID,
		"engine_id":     record.EngineID,
		"resource_name":   record.ResourceName,
		"resource_type":   record.ResourceType,
		"bucket":          record.Bucket,
		"relative_path":   record.RelativePath,
		"file_name":       record.FileName,
		"file_path":       record.FilePath,
		"document_type":   record.DocumentType,
		"title":           record.Title,
		"author":          record.Author,
		"keywords":        record.Keywords,
		"content":         record.Content,
		"content_preview": record.ContentPreview,
		"content_type":    record.ContentType,
		"file_size":       record.FileSize,
		"word_count":      record.WordCount,
		"page_count":      record.PageCount,
		"last_modified":   record.LastModified,
		"created_date":    record.CreatedDate,
		"modified_date":   record.ModifiedDate,
		"metadata":        record.Metadata,
		"updated_at":      record.UpdatedAt,
	}

	index := i.client.Index(i.documentIndex)
	task, err := index.AddDocuments([]map[string]interface{}{doc})
	if err != nil {
		return fmt.Errorf("failed to index document: %w", err)
	}

	i.log.Debug("文档已索引",
		"document_id", record.DocumentID,
		"task_uid", task.TaskUID,
	)

	return nil
}

// DeleteDocument 按 ID 删除文档索引
func (i *Indexer) DeleteDocument(ctx context.Context, documentID string) error {
	if !i.Enabled() || i.documentIndex == "" || documentID == "" {
		return nil
	}

	index := i.client.Index(i.documentIndex)
	task, err := index.DeleteDocument(documentID)
	if err != nil {
		return fmt.Errorf("failed to delete document %s: %w", documentID, err)
	}

	i.log.Info("文档已删除",
		"document_id", documentID,
		"task_uid", task.TaskUID,
	)

	return nil
}

// DeleteObjects 删除指定 Bucket/路径下的对象索引
func (i *Indexer) DeleteObjects(ctx context.Context, tenantID, engineID uint, bucket, relativePath string) error {
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

	if relativePath != "" {
		// 支持前缀匹配
		filters = append(filters, fmt.Sprintf("relative_path ^= '%s'", escapeFilterValue(relativePath)))
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
		"path", relativePath,
		"task_uid", task.TaskUID,
	)

	// 删除文档索引中的记录
	if i.documentIndex != "" {
		docIndex := i.client.Index(i.documentIndex)
		task, err := docIndex.DeleteDocumentsByFilter(filterStr)
		if err != nil {
			i.log.Warn("删除文档索引失败", "error", err)
		} else {
			i.log.Info("对象文档已删除", "task_uid", task.TaskUID)
		}
	}

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
