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
	client     *meilisearch.Client
	assetIndex string
	enabled    bool
	mu         sync.RWMutex
	log        *slog.Logger
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

// AssetRecord 统一资产记录（包含表、对象、文档内容）
// 基础扫描只填充基本字段，深度扫描填充完整内容
type AssetRecord struct {
	// ===== 基础字段（所有资产，基础扫描即写） =====
	AssetID       string                 `json:"asset_id"`
	DocumentID    string                 `json:"document_id,omitempty"` // 文件SHA256指纹（对象特有）
	TenantID      uint                   `json:"tenant_id"`
	EngineID      uint                   `json:"engine_id"`
	EngineName    string                 `json:"engine_name,omitempty"`
	EngineType    string                 `json:"engine_type,omitempty"`
	AssetType     string                 `json:"asset_type"` // "table" | "object"
	Name          string                 `json:"name"`
	FullName      string                 `json:"full_name,omitempty"`
	Description   string                 `json:"description,omitempty"`
	Tags          []string               `json:"tags,omitempty"`

	// ===== 表特有字段 =====
	Schema        string         `json:"schema,omitempty"`
	TableType     string         `json:"table_type,omitempty"`
	Fields        []FieldRecord  `json:"fields,omitempty"`
	RowCount      *int64         `json:"row_count,omitempty"`

	// ===== 对象特有字段 =====
	Bucket        string     `json:"bucket,omitempty"`
	Path          string     `json:"path,omitempty"`
	RelativePath  string     `json:"relative_path,omitempty"`
	SizeBytes     *int64     `json:"size_bytes,omitempty"`
	ContentType   string     `json:"content_type,omitempty"`
	DataUpdatedAt *time.Time `json:"data_updated_at,omitempty"`

	// ===== 文档内容字段（深度扫描才写） =====
	Content        string     `json:"content,omitempty"`          // 全文内容
	ContentPreview string     `json:"content_preview,omitempty"`  // 内容预览
	DocumentType   string     `json:"document_type,omitempty"`    // pdf/docx/txt
	Title          string     `json:"title,omitempty"`
	Author         string     `json:"author,omitempty"`
	Keywords       []string   `json:"keywords,omitempty"`
	WordCount      int        `json:"word_count,omitempty"`
	PageCount      int        `json:"page_count,omitempty"`
	CreatedDate    *time.Time `json:"created_date,omitempty"`
	ModifiedDate   *time.Time `json:"modified_date,omitempty"`

	// ===== 通用字段 =====
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

// NewIndexer 创建索引器（若未配置 Meilisearch URL，则返回禁用状态）
func NewIndexer(cfg *config.Config) (*Indexer, error) {
	idx := &Indexer{
		assetIndex: strings.TrimSpace(cfg.MeilisearchAssetIndex),
		enabled:    strings.TrimSpace(cfg.MeilisearchURL) != "",
		log:        logger.With("component", "meta_indexer"),
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
	)

	return idx, nil
}

// Enabled 判断是否启用了索引功能
func (i *Indexer) Enabled() bool {
	return i != nil && i.enabled && i.client != nil
}

// AssetIndexName 返回资产索引名称
func (i *Indexer) AssetIndexName() string {
	return i.assetIndex
}

// Client 返回 Meilisearch 客户端（用于高级操作）
func (i *Indexer) Client() *meilisearch.Client {
	return i.client
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

	// 设置可搜索字段（按权重排序）
	_, err = assetIndex.UpdateSearchableAttributes(&[]string{
		"name",               // 文件名/表名 - 最高权重
		"title",              // 文档标题
		"full_name",          // 完整路径名
		"content_preview",    // 内容预览（中等权重）
		"description",        // 描述
		"tags",               // 标签
		"content",            // 全文内容（权重较低但范围广）
		"fields.name",        // 表字段名
		"fields.comment",     // 字段注释
		"keywords",           // 关键词
		"author",             // 作者
	})
	if err != nil {
		return fmt.Errorf("failed to update asset searchable attributes: %w", err)
	}

	// 设置可过滤字段
	_, err = assetIndex.UpdateFilterableAttributes(&[]string{
		"tenant_id",
		"engine_id",
		"engine_type",
		"asset_type",     // 可过滤表/对象
		"schema",
		"bucket",
		"table_type",
		"document_type",  // 可过滤文档类型
	})
	if err != nil {
		return fmt.Errorf("failed to update asset filterable attributes: %w", err)
	}

	// 设置可排序字段
	_, err = assetIndex.UpdateSortableAttributes(&[]string{
		"data_updated_at",
		"size_bytes",
		"row_count",
		"word_count",
		"page_count",
	})
	if err != nil {
		return fmt.Errorf("failed to update asset sortable attributes: %w", err)
	}

	i.log.Info("索引配置已更新", "index", i.assetIndex)

	return nil
}

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
		"document_id":     record.DocumentID, // 文件指纹（用于混合检索去重）
		"tenant_id":       record.TenantID,
		"engine_id":       record.EngineID,
		"engine_name":     record.EngineName,
		"engine_type":     record.EngineType,
		"asset_type":      record.AssetType,
		"name":            record.Name,
		"full_name":       record.FullName,
		"schema":          record.Schema,
		"table_type":      record.TableType,
		"bucket":          record.Bucket,
		"path":            record.Path,
		"relative_path":   record.RelativePath,
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
