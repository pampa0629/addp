package search

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/addp/common/logger"
	"github.com/addp/meta/internal/config"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	"log/slog"
)

const (
	defaultAssetIndexMapping = `{
  "settings": {
    "analysis": {
      "analyzer": {
        "default_text": {
          "tokenizer": "standard",
          "filter": ["lowercase"]
        }
      }
    }
  },
  "mappings": {
    "properties": {
      "asset_id": { "type": "keyword" },
      "tenant_id": { "type": "long" },
      "resource_id": { "type": "long" },
      "resource_name": { "type": "keyword" },
      "resource_type": { "type": "keyword" },
      "asset_type": { "type": "keyword" },
      "name": {
        "type": "text",
        "analyzer": "default_text",
        "fields": {
          "keyword": { "type": "keyword", "ignore_above": 256 }
        }
      },
      "full_name": {
        "type": "text",
        "analyzer": "default_text",
        "fields": {
          "keyword": { "type": "keyword", "ignore_above": 512 }
        }
      },
      "schema": { "type": "keyword" },
      "table_type": { "type": "keyword" },
      "bucket": { "type": "keyword" },
      "path": { "type": "keyword" },
      "relative_path": { "type": "keyword" },
      "description": { "type": "text", "analyzer": "default_text" },
      "tags": { "type": "keyword" },
      "row_count": { "type": "long" },
      "size_bytes": { "type": "long" },
      "object_size_bytes": { "type": "long" },
      "last_modified": { "type": "date" },
      "metadata": { "type": "object" },
      "fields": {
        "type": "nested",
        "properties": {
          "name": { "type": "keyword" },
          "data_type": { "type": "keyword" },
          "column_type": { "type": "keyword" },
          "comment": { "type": "text", "analyzer": "default_text" },
          "ordinal_position": { "type": "integer" },
          "is_nullable": { "type": "boolean" },
          "is_primary_key": { "type": "boolean" },
          "is_unique_key": { "type": "boolean" }
        }
      },
      "updated_at": { "type": "date" }
    }
  }
}`
	defaultDocumentIndexMapping = `{
  "settings": {
    "analysis": {
      "analyzer": {
        "default_text": {
          "tokenizer": "standard",
          "filter": ["lowercase"]
        }
      }
    }
  },
  "mappings": {
    "properties": {
      "document_id": { "type": "keyword" },
      "asset_id": { "type": "keyword" },
      "tenant_id": { "type": "long" },
      "resource_id": { "type": "long" },
      "resource_name": { "type": "keyword" },
      "resource_type": { "type": "keyword" },
      "bucket": { "type": "keyword" },
      "relative_path": { "type": "keyword" },
      "file_name": {
        "type": "text",
        "analyzer": "default_text",
        "fields": {
          "keyword": { "type": "keyword", "ignore_above": 256 }
        }
      },
      "file_path": { "type": "keyword" },
      "document_type": { "type": "keyword" },
      "title": { "type": "text", "analyzer": "default_text" },
      "author": { "type": "keyword" },
      "keywords": { "type": "keyword" },
      "content": { "type": "text", "analyzer": "default_text" },
      "content_preview": { "type": "text", "analyzer": "default_text" },
      "content_type": { "type": "keyword" },
      "file_size": { "type": "long" },
      "word_count": { "type": "integer" },
      "page_count": { "type": "integer" },
      "last_modified": { "type": "date" },
      "created_date": { "type": "date" },
      "modified_date": { "type": "date" },
      "metadata": { "type": "object" },
      "updated_at": { "type": "date" }
    }
  }
}`
	defaultIndexRefresh = "true"
)

// Indexer 封装 Elasticsearch 操作
type Indexer struct {
	client        *elasticsearch.Client
	assetIndex    string
	documentIndex string
	enabled       bool
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
	AssetID         string                 `json:"asset_id"`
	TenantID        uint                   `json:"tenant_id"`
	ResourceID      uint                   `json:"resource_id"`
	ResourceName    string                 `json:"resource_name,omitempty"`
	ResourceType    string                 `json:"resource_type,omitempty"`
	AssetType       string                 `json:"asset_type"`
	Name            string                 `json:"name"`
	FullName        string                 `json:"full_name,omitempty"`
	Schema          string                 `json:"schema,omitempty"`
	TableType       string                 `json:"table_type,omitempty"`
	Bucket          string                 `json:"bucket,omitempty"`
	Path            string                 `json:"path,omitempty"`
	RelativePath    string                 `json:"relative_path,omitempty"`
	Description     string                 `json:"description,omitempty"`
	Tags            []string               `json:"tags,omitempty"`
	RowCount        *int64                 `json:"row_count,omitempty"`
	SizeBytes       *int64                 `json:"size_bytes,omitempty"`
	ObjectSizeBytes *int64                 `json:"object_size_bytes,omitempty"`
	LastModified    *time.Time             `json:"last_modified,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	Fields          []FieldRecord          `json:"fields,omitempty"`
	UpdatedAt       time.Time              `json:"updated_at"`
}

// DocumentRecord 描述可全文搜索的文档数据
type DocumentRecord struct {
	DocumentID     string                 `json:"document_id"`
	AssetID        string                 `json:"asset_id"`
	TenantID       uint                   `json:"tenant_id"`
	ResourceID     uint                   `json:"resource_id"`
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

// NewIndexer 创建索引器（若未配置 Elasticsearch URL，则返回禁用状态）
func NewIndexer(cfg *config.Config) (*Indexer, error) {
	indexer := &Indexer{
		assetIndex:    cfg.ElasticsearchAssetIndex,
		documentIndex: cfg.ElasticsearchDocumentIndex,
		enabled:       cfg.ElasticsearchURL != "",
		log:           logger.With("component", "meta_search_indexer"),
	}

	if !indexer.enabled {
		indexer.log.Info("Elasticsearch 未配置，已禁用搜索索引功能")
		return indexer, nil
	}

	esCfg := elasticsearch.Config{
		Addresses: []string{cfg.ElasticsearchURL},
	}
	if cfg.ElasticsearchUsername != "" || cfg.ElasticsearchPassword != "" {
		esCfg.Username = cfg.ElasticsearchUsername
		esCfg.Password = cfg.ElasticsearchPassword
	}
	if cfg.ElasticsearchAPIKey != "" {
		esCfg.APIKey = cfg.ElasticsearchAPIKey
	}
	if cfg.ElasticsearchDisableTLSVerify {
		esCfg.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - 控制由配置决定
		}
	}

	client, err := elasticsearch.NewClient(esCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create elasticsearch client: %w", err)
	}
	indexer.client = client

	if err := indexer.ensureIndex(indexer.assetIndex, defaultAssetIndexMapping); err != nil {
		return nil, err
	}
	if indexer.documentIndex != "" {
		if err := indexer.ensureIndex(indexer.documentIndex, defaultDocumentIndexMapping); err != nil {
			return nil, err
		}
	}

	return indexer, nil
}

// Enabled 判断是否启用了索引功能
func (i *Indexer) Enabled() bool {
	return i != nil && i.enabled && i.client != nil
}

func (i *Indexer) ensureIndex(name, mapping string) error {
	if name == "" {
		return fmt.Errorf("index name cannot be empty")
	}

	exists, err := i.client.Indices.Exists([]string{name})
	if err != nil {
		return fmt.Errorf("failed to check index %s: %w", name, err)
	}
	defer exists.Body.Close()
	if exists.StatusCode == http.StatusOK {
		return nil
	}
	if exists.StatusCode != http.StatusNotFound {
		return fmt.Errorf("unexpected response when checking index %s: %s", name, exists.String())
	}

	res, err := i.client.Indices.Create(name, i.client.Indices.Create.WithBody(bytes.NewReader([]byte(mapping))))
	if err != nil {
		return fmt.Errorf("failed to create index %s: %w", name, err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("failed to create index %s: %s", name, res.String())
	}
	i.log.Info("已创建 Elasticsearch 索引", "index", name)
	return nil
}

// IndexAsset 写入/更新资产信息
func (i *Indexer) IndexAsset(ctx context.Context, record *AssetRecord) error {
	if !i.Enabled() || record == nil || record.AssetID == "" {
		return nil
	}
	record.UpdatedAt = time.Now().UTC()
	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal asset record: %w", err)
	}

	req := esapi.IndexRequest{
		Index:      i.assetIndex,
		DocumentID: record.AssetID,
		Body:       bytes.NewReader(body),
		Refresh:    defaultIndexRefresh,
	}
	res, err := req.Do(ctx, i.client)
	if err != nil {
		return fmt.Errorf("failed to index asset: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("index asset error: %s", res.String())
	}
	return nil
}

// IndexDocument 写入/更新文档全文索引
func (i *Indexer) IndexDocument(ctx context.Context, record *DocumentRecord) error {
	if !i.Enabled() || i.documentIndex == "" || record == nil || record.DocumentID == "" {
		return nil
	}
	record.UpdatedAt = time.Now().UTC()
	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal document record: %w", err)
	}
	req := esapi.IndexRequest{
		Index:      i.documentIndex,
		DocumentID: record.DocumentID,
		Body:       bytes.NewReader(body),
		Refresh:    defaultIndexRefresh,
	}
	res, err := req.Do(ctx, i.client)
	if err != nil {
		return fmt.Errorf("failed to index document: %w", err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("index document error: %s", res.String())
	}
	return nil
}

// DeleteDocument 按 ID 删除文档索引
func (i *Indexer) DeleteDocument(ctx context.Context, documentID string) error {
	if !i.Enabled() || i.documentIndex == "" || documentID == "" {
		return nil
	}
	req := esapi.DeleteRequest{
		Index:      i.documentIndex,
		DocumentID: documentID,
		Refresh:    defaultIndexRefresh,
	}
	res, err := req.Do(ctx, i.client)
	if err != nil {
		return fmt.Errorf("failed to delete document: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	if res.IsError() {
		return fmt.Errorf("delete document error: %s", res.String())
	}
	return nil
}

// DeleteObjects 删除指定 Bucket/路径下的对象索引
func (i *Indexer) DeleteObjects(ctx context.Context, tenantID, resourceID uint, bucket, relativePath string) error {
	if !i.Enabled() || bucket == "" {
		return nil
	}

	assetQuery := buildObjectDeleteQuery(tenantID, resourceID, bucket, relativePath)
	if err := i.deleteByQuery(ctx, i.assetIndex, assetQuery); err != nil {
		return err
	}
	if i.documentIndex != "" {
		docQuery := buildDocumentDeleteQuery(tenantID, resourceID, bucket, relativePath)
		if err := i.deleteByQuery(ctx, i.documentIndex, docQuery); err != nil {
			return err
		}
	}
	return nil
}

// DeleteTables 删除某租户资源下指定 Schema 的表索引
func (i *Indexer) DeleteTables(ctx context.Context, tenantID, resourceID uint, schemaName string) error {
	if !i.Enabled() || schemaName == "" {
		return nil
	}

	query := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": []interface{}{
					map[string]interface{}{"term": map[string]interface{}{"tenant_id": tenantID}},
					map[string]interface{}{"term": map[string]interface{}{"resource_id": resourceID}},
					map[string]interface{}{"term": map[string]interface{}{"asset_type": "table"}},
					map[string]interface{}{"term": map[string]interface{}{"schema": schemaName}},
				},
			},
		},
	}
	return i.deleteByQuery(ctx, i.assetIndex, query)
}

func (i *Indexer) deleteByQuery(ctx context.Context, index string, body map[string]interface{}) error {
	if index == "" || body == nil {
		return nil
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal delete-by-query body: %w", err)
	}
	req := esapi.DeleteByQueryRequest{
		Index:   []string{index},
		Body:    bytes.NewReader(payload),
		Refresh: esapi.BoolPtr(true),
	}
	res, err := req.Do(ctx, i.client)
	if err != nil {
		return fmt.Errorf("failed to execute delete-by-query on %s: %w", index, err)
	}
	defer res.Body.Close()
	if res.IsError() {
		return fmt.Errorf("delete-by-query error on %s: %s", index, res.String())
	}
	return nil
}

func buildObjectDeleteQuery(tenantID, resourceID uint, bucket, relativePath string) map[string]interface{} {
	must := []interface{}{
		map[string]interface{}{"term": map[string]interface{}{"tenant_id": tenantID}},
		map[string]interface{}{"term": map[string]interface{}{"resource_id": resourceID}},
		map[string]interface{}{"term": map[string]interface{}{"bucket": bucket}},
	}

	// asset index uses "asset_type"
	mustAssetType := map[string]interface{}{"term": map[string]interface{}{"asset_type": "object"}}
	must = append(must, mustAssetType)

	var filter []interface{}
	if relativePath != "" {
		filter = append(filter, map[string]interface{}{
			"bool": map[string]interface{}{
				"should": []interface{}{
					map[string]interface{}{"term": map[string]interface{}{"relative_path": relativePath}},
					map[string]interface{}{"prefix": map[string]interface{}{"relative_path": relativePath + "/"}},
				},
			},
		})
	}

	return map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must":   must,
				"filter": filter,
			},
		},
	}
}

func buildDocumentDeleteQuery(tenantID, resourceID uint, bucket, relativePath string) map[string]interface{} {
	must := []interface{}{
		map[string]interface{}{"term": map[string]interface{}{"tenant_id": tenantID}},
		map[string]interface{}{"term": map[string]interface{}{"resource_id": resourceID}},
		map[string]interface{}{"term": map[string]interface{}{"bucket": bucket}},
	}

	var filter []interface{}
	if relativePath != "" {
		filter = append(filter, map[string]interface{}{
			"bool": map[string]interface{}{
				"should": []interface{}{
					map[string]interface{}{"term": map[string]interface{}{"relative_path": relativePath}},
					map[string]interface{}{"prefix": map[string]interface{}{"relative_path": relativePath + "/"}},
				},
			},
		})
	}

	return map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must":   must,
				"filter": filter,
			},
		},
	}
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
