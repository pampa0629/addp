package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/embedding"
	"github.com/addp/common/logger"
	"github.com/addp/common/vectorstore"
	"github.com/addp/manager/internal/config"
	"github.com/meilisearch/meilisearch-go"
	"log/slog"
)

var (
	// ErrFullTextSearchDisabled 在未配置搜索引擎时返回
	ErrFullTextSearchDisabled = errors.New("full text search is not configured")
)

const (
	queryEmbeddingRuneLimit = 3500
)

// FullTextSearchService 提供全文检索能力
type FullTextSearchService struct {
	client            *meilisearch.Client
	documentIndex     string
	assetIndex        string
	enabled           bool
	log               *slog.Logger
	vectorStore       *vectorstore.PgVectorStore
	textEmbedder      embedding.TextEmbedder
	models            map[embedding.Modality]string
	embeddingTimeout  time.Duration
	vectorTopK        int
	vectorMaxDistance float64
}

// FullTextDocument 表示检索结果中的单个文档
type FullTextDocument struct {
	DocumentID     string                 `json:"document_id"`
	AssetID        string                 `json:"asset_id"`
	Score          float64                `json:"score"`
	ResourceID     uint                   `json:"resource_id"`
	ResourceName   string                 `json:"resource_name,omitempty"`
	ResourceType   string                 `json:"resource_type,omitempty"`
	Bucket         string                 `json:"bucket,omitempty"`
	Schema         string                 `json:"schema,omitempty"`
	RelativePath   string                 `json:"relative_path,omitempty"`
	ObjectKey      string                 `json:"object_key,omitempty"`
	FileName       string                 `json:"file_name"`
	DocumentType   string                 `json:"document_type,omitempty"`
	Title          string                 `json:"title,omitempty"`
	Author         string                 `json:"author,omitempty"`
	Keywords       []string               `json:"keywords,omitempty"`
	ContentPreview string                 `json:"content_preview,omitempty"`
	ContentType    string                 `json:"content_type,omitempty"`
	FileSize       int64                  `json:"file_size,omitempty"`
	WordCount      int                    `json:"word_count,omitempty"`
	PageCount      int                    `json:"page_count,omitempty"`
	LastModified   string                 `json:"last_modified,omitempty"`
	CreatedDate    string                 `json:"created_date,omitempty"`
	ModifiedDate   string                 `json:"modified_date,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	Highlights     map[string][]string    `json:"highlights,omitempty"`
}

// VectorDocument 表示向量检索结果
type VectorDocument struct {
	DocumentID     string                 `json:"document_id"`
	AssetID        string                 `json:"asset_id,omitempty"`
	TenantID       uint                   `json:"tenant_id,omitempty"`
	ResourceID     uint                   `json:"resource_id,omitempty"`
	Score          float64                `json:"score"`
	Distance       float64                `json:"distance"`
	Model          string                 `json:"model"`
	Modality       string                 `json:"modality"`
	Title          string                 `json:"title,omitempty"`
	FileName       string                 `json:"file_name,omitempty"`
	ResourceName   string                 `json:"resource_name,omitempty"`
	ResourceType   string                 `json:"resource_type,omitempty"`
	Bucket         string                 `json:"bucket,omitempty"`
	RelativePath   string                 `json:"relative_path,omitempty"`
	ContentPreview string                 `json:"content_preview,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// FullTextSearchResult 总体响应结构
type FullTextSearchResult struct {
	Total      int                `json:"total"`
	Page       int                `json:"page"`
	PageSize   int                `json:"page_size"`
	Hits       []FullTextDocument `json:"results"`
	VectorHits []VectorDocument   `json:"vector_hits,omitempty"`
}

// NewFullTextSearchService 构建全文检索服务
func NewFullTextSearchService(cfg *config.Config) (*FullTextSearchService, error) {
	svc := &FullTextSearchService{
		documentIndex: strings.TrimSpace(cfg.MeilisearchDocumentIndex),
		assetIndex:    strings.TrimSpace(cfg.MeilisearchAssetIndex),
		enabled:       strings.TrimSpace(cfg.MeilisearchURL) != "",
		log:           logger.With("component", "manager_fulltext_search"),
		models: map[embedding.Modality]string{
			embedding.ModalityText:     cfg.EmbeddingService.TextModel,
			embedding.ModalityDocument: cfg.EmbeddingService.TextModel,
			embedding.ModalityImage:    cfg.EmbeddingService.ImageModel,
			embedding.ModalityAudio:    cfg.EmbeddingService.AudioModel,
			embedding.ModalityVideo:    cfg.EmbeddingService.VideoModel,
		},
		embeddingTimeout:  cfg.EmbeddingService.Timeout,
		vectorTopK:        10,
		vectorMaxDistance: cfg.VectorSearchMaxDistance,
	}
	if svc.embeddingTimeout <= 0 {
		svc.embeddingTimeout = 15 * time.Second
	}
	if svc.vectorMaxDistance <= 0 {
		svc.vectorMaxDistance = 0.35
	}

	if !svc.enabled {
		svc.log.Info("Meilisearch 未配置，全文检索功能已禁用")
		return svc, nil
	}

	// 创建 Meilisearch 客户端
	client := meilisearch.NewClient(meilisearch.ClientConfig{
		Host:   cfg.MeilisearchURL,
		APIKey: cfg.MeilisearchMasterKey,
	})
	svc.client = client

	// 初始化索引配置
	if err := svc.initIndexes(); err != nil {
		return nil, fmt.Errorf("failed to initialize indexes: %w", err)
	}

	svc.log.Info("全文检索服务已启用",
		"document_index", svc.documentIndex,
		"asset_index", svc.assetIndex,
		"url", cfg.MeilisearchURL,
	)

	if strings.TrimSpace(cfg.EmbeddingService.BaseURL) != "" {
		if err := svc.initVectorComponents(cfg); err != nil {
			svc.log.Warn("向量检索初始化失败", "error", err)
		}
	} else {
		svc.log.Info("未配置向量化服务，向量检索保持禁用状态")
	}
	return svc, nil
}

func (s *FullTextSearchService) initVectorComponents(cfg *config.Config) error {
	store, err := vectorstore.NewPgVectorStore(context.Background(), cfg.VectorDB)
	if err != nil {
		return fmt.Errorf("init vector store: %w", err)
	}

	textModel := strings.TrimSpace(s.models[embedding.ModalityText])
	if textModel == "" {
		textModel = "bge-large-zh"
	}
	docModel := strings.TrimSpace(s.models[embedding.ModalityDocument])
	if docModel == "" {
		docModel = textModel
	}

	models := map[embedding.Modality]string{
		embedding.ModalityText:     textModel,
		embedding.ModalityDocument: docModel,
	}
	if img := strings.TrimSpace(s.models[embedding.ModalityImage]); img != "" {
		models[embedding.ModalityImage] = img
	}
	if audio := strings.TrimSpace(s.models[embedding.ModalityAudio]); audio != "" {
		models[embedding.ModalityAudio] = audio
	}
	if video := strings.TrimSpace(s.models[embedding.ModalityVideo]); video != "" {
		models[embedding.ModalityVideo] = video
	}

	client, err := embedding.NewHTTPEmbeddingClient(embedding.ServiceConfig{
		BaseURL: cfg.EmbeddingService.BaseURL,
		APIKey:  cfg.EmbeddingService.APIKey,
		Timeout: s.embeddingTimeout,
		Models:  models,
	})
	if err != nil {
		store.Close()
		return fmt.Errorf("init embedding client: %w", err)
	}

	s.vectorStore = store
	s.textEmbedder = client
	s.models = models
	s.log.Info("向量检索能力已启用",
		"vector_schema", cfg.VectorDB.Schema,
		"vector_table", cfg.VectorDB.Table,
		"text_model", textModel,
	)
	return nil
}

// initIndexes 初始化 Meilisearch 索引配置
func (s *FullTextSearchService) initIndexes() error {
	// 配置文档索引
	docIndex := s.client.Index(s.documentIndex)

	// 设置可搜索字段 (按权重排序)
	_, err := docIndex.UpdateSearchableAttributes(&[]string{
		"title",           // 最高权重
		"file_name",       // 次高权重
		"content_preview", // 次高权重
		"content",         // 正文内容
		"metadata.description",
		"metadata.tags",
	})
	if err != nil {
		return fmt.Errorf("failed to update searchable attributes: %w", err)
	}

	// 设置可过滤字段
	_, err = docIndex.UpdateFilterableAttributes(&[]string{
		"tenant_id",
		"resource_id",
		"resource_type",
		"document_type",
		"bucket",
	})
	if err != nil {
		return fmt.Errorf("failed to update filterable attributes: %w", err)
	}

	// 设置可排序字段
	_, err = docIndex.UpdateSortableAttributes(&[]string{
		"created_date",
		"last_modified",
		"file_size",
	})
	if err != nil {
		return fmt.Errorf("failed to update sortable attributes: %w", err)
	}

	s.log.Info("索引配置已更新", "index", s.documentIndex)
	return nil
}

// Enabled 返回全文检索是否可用
func (s *FullTextSearchService) Enabled() bool {
	return s != nil && s.enabled && s.client != nil && s.documentIndex != ""
}

// SearchDocuments 执行全文检索
func (s *FullTextSearchService) SearchDocuments(
	ctx context.Context,
	tenantID *uint,
	query string,
	page int,
	pageSize int,
) (*FullTextSearchResult, error) {
	if !s.Enabled() {
		return nil, ErrFullTextSearchDisabled
	}

	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query cannot be empty")
	}
	query = truncateQueryRunes(query, queryEmbeddingRuneLimit)

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	} else if pageSize > 50 {
		pageSize = 50
	}

	offset := (page - 1) * pageSize

	// 构建过滤条件
	var filter string
	if tenantID != nil {
		filter = fmt.Sprintf("tenant_id = %d", *tenantID)
	}

	// 构建搜索请求
	searchReq := &meilisearch.SearchRequest{
		Query:                 query,
		Filter:                filter,
		AttributesToHighlight: []string{"title", "file_name", "content", "content_preview", "metadata.tags", "metadata.summary"},
		HighlightPreTag:       "<mark>",
		HighlightPostTag:      "</mark>",
		Offset:                int64(offset),
		Limit:                 int64(pageSize),
		ShowMatchesPosition:   false,
	}

	// 执行搜索
	index := s.client.Index(s.documentIndex)
	resp, err := index.Search(query, searchReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute search: %w", err)
	}

	// 解析结果
	result := &FullTextSearchResult{
		Total:    int(resp.EstimatedTotalHits),
		Page:     page,
		PageSize: pageSize,
		Hits:     make([]FullTextDocument, 0, len(resp.Hits)),
	}

	for _, hit := range resp.Hits {
		doc := mapMeilisearchHit(hit)
		result.Hits = append(result.Hits, doc)
	}

	// 向量检索 (保持不变)
	if s.vectorStore != nil && s.textEmbedder != nil {
		vectorHits, err := s.vectorSearch(ctx, tenantID, query)
		if err != nil {
			s.log.Warn("向量检索失败，已忽略", "error", err)
		} else {
			result.VectorHits = vectorHits
			// 合并去重逻辑
			existing := make(map[string]struct{}, len(result.Hits))
			for _, doc := range result.Hits {
				if doc.DocumentID == "" {
					continue
				}
				existing[doc.DocumentID] = struct{}{}
			}
			for _, vdoc := range vectorHits {
				if vdoc.DocumentID == "" {
					continue
				}
				if _, ok := existing[vdoc.DocumentID]; ok {
					continue
				}
				converted := vectorDocumentToFullText(vdoc)
				result.Hits = append(result.Hits, converted)
				existing[converted.DocumentID] = struct{}{}
			}
		}
	}

	return result, nil
}

func (s *FullTextSearchService) vectorSearch(ctx context.Context, tenantID *uint, query string) ([]VectorDocument, error) {
	if s.vectorStore == nil || s.textEmbedder == nil {
		return nil, nil
	}

	timeout := s.embeddingTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	embedCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	textModel := s.models[embedding.ModalityText]
	input := []embedding.TextInput{{
		ID:       "query",
		Text:     query,
		Language: "",
		Metadata: map[string]string{
			"source": "manager_search",
			"model":  textModel,
		},
	}}

	embedResult, err := s.textEmbedder.EmbedText(embedCtx, input)
	if err != nil {
		return nil, fmt.Errorf("embed query text: %w", err)
	}
	if embedResult == nil || len(embedResult.Embeddings) == 0 {
		return nil, nil
	}

	queryEmbedding := embedResult.Embeddings[0]
	queryVector := queryEmbedding.Vector
	if len(queryVector) == 0 {
		return nil, nil
	}

	docModel := s.models[embedding.ModalityDocument]
	if strings.TrimSpace(docModel) == "" {
		docModel = queryEmbedding.Model
		if strings.TrimSpace(docModel) == "" {
			docModel = textModel
		}
	}

	type modalityQuery struct {
		modality embedding.Modality
		model    string
	}

	modQueries := make([]modalityQuery, 0, 3)
	if trimmed := strings.TrimSpace(docModel); trimmed != "" {
		modQueries = append(modQueries, modalityQuery{modality: embedding.ModalityDocument, model: trimmed})
	}
	if imgModel := strings.TrimSpace(s.models[embedding.ModalityImage]); imgModel != "" {
		modQueries = append(modQueries, modalityQuery{modality: embedding.ModalityImage, model: imgModel})
	}
	if videoModel := strings.TrimSpace(s.models[embedding.ModalityVideo]); videoModel != "" {
		modQueries = append(modQueries, modalityQuery{modality: embedding.ModalityVideo, model: videoModel})
	}
	if len(modQueries) == 0 {
		return nil, nil
	}

	tenantVal := uint(0)
	if tenantID != nil {
		tenantVal = *tenantID
	}
	s.log.Debug("触发向量检索",
		"tenant_id", tenantVal,
		"query", query,
		"top_k", s.vectorTopK,
		"max_distance", s.vectorMaxDistance,
	)

	queryCtx, cancelQuery := context.WithTimeout(ctx, timeout)
	defer cancelQuery()

	type resultKey string
	makeKey := func(id string) resultKey { return resultKey(id) }

	accumulator := make(map[resultKey]VectorDocument)

	for _, mq := range modQueries {
		opts := vectorstore.QueryOptions{
			Modalities:  []embedding.Modality{mq.modality},
			Model:       mq.model,
			TopK:        s.vectorTopK,
			IncludeMeta: true,
		}
		if s.vectorMaxDistance > 0 && mq.modality == embedding.ModalityDocument {
			opts.MaxDistance = s.vectorMaxDistance
		}
		if tenantID != nil {
			opts.TenantID = tenantID
		}

		s.log.Debug("向量检索子查询",
			"tenant_id", tenantVal,
			"query", query,
			"modality", string(mq.modality),
			"model", mq.model,
			"top_k", opts.TopK,
			"max_distance", opts.MaxDistance,
		)

		results, err := s.vectorStore.QuerySimilar(queryCtx, queryVector, opts)
		if err != nil {
			return nil, fmt.Errorf("query vector store (%s): %w", mq.modality, err)
		}

		subPreview := buildSearchResultPreview(results, 5)
		s.log.Info("向量检索子查询结果",
			"tenant_id", tenantVal,
			"query", query,
			"modality", string(mq.modality),
			"model", mq.model,
			"hit_count", len(results),
			"preview", subPreview,
		)

		for _, item := range results {
			doc := VectorDocument{
				DocumentID: item.Record.ObjectID,
				AssetID:    item.Record.AssetID,
				Model:      item.Record.Model,
				Modality:   string(item.Record.Modality),
				Distance:   item.Distance,
				Score:      similarityFromDistance(item.Distance),
			}
			if item.Record.TenantID != nil {
				doc.TenantID = *item.Record.TenantID
			}
			if item.Record.ResourceID != nil {
				doc.ResourceID = *item.Record.ResourceID
			}
			if meta := item.Record.Metadata; meta != nil {
				assignString(meta, "title", &doc.Title)
				assignString(meta, "file_name", &doc.FileName)
				assignString(meta, "resource_name", &doc.ResourceName)
				assignString(meta, "resource_type", &doc.ResourceType)
				assignString(meta, "bucket", &doc.Bucket)
				assignString(meta, "relative_path", &doc.RelativePath)
				assignString(meta, "content_preview", &doc.ContentPreview)
				doc.Metadata = meta
			}

			key := makeKey(doc.DocumentID)
			if existing, ok := accumulator[key]; ok {
				if doc.Distance < existing.Distance {
					s.log.Debug("向量检索命中更新",
						"document_id", doc.DocumentID,
						"previous_distance", existing.Distance,
						"new_distance", doc.Distance,
						"previous_modality", existing.Modality,
						"new_modality", doc.Modality,
						"previous_model", existing.Model,
						"new_model", doc.Model,
					)
					accumulator[key] = doc
				}
				continue
			}
			accumulator[key] = doc
		}
	}

	vectorDocs := make([]VectorDocument, 0, len(accumulator))
	for _, doc := range accumulator {
		vectorDocs = append(vectorDocs, doc)
	}

	sort.Slice(vectorDocs, func(i, j int) bool {
		if vectorDocs[i].Distance == vectorDocs[j].Distance {
			return vectorDocs[i].DocumentID < vectorDocs[j].DocumentID
		}
		return vectorDocs[i].Distance < vectorDocs[j].Distance
	})

	if len(vectorDocs) > s.vectorTopK && s.vectorTopK > 0 {
		vectorDocs = vectorDocs[:s.vectorTopK]
	}

	preview := make([]map[string]any, 0, min(len(vectorDocs), 3))
	for idx := 0; idx < len(vectorDocs) && idx < 3; idx++ {
		doc := vectorDocs[idx]
		preview = append(preview, map[string]any{
			"document_id": doc.DocumentID,
			"modality":    doc.Modality,
			"model":       doc.Model,
			"distance":    doc.Distance,
			"file_name":   doc.FileName,
		})
	}

	s.log.Info("向量检索完成",
		"tenant_id", tenantVal,
		"query", query,
		"hit_count", len(vectorDocs),
		"preview", preview,
	)

	return vectorDocs, nil
}

// Close 释放底层资源
func (s *FullTextSearchService) Close() {
	if s.vectorStore != nil {
		s.vectorStore.Close()
	}
}

func truncateQueryRunes(text string, limit int) string {
	if limit <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func similarityFromDistance(distance float64) float64 {
	if distance <= 0 {
		return 1
	}
	return 1 / (1 + distance)
}

func buildSearchResultPreview(results []vectorstore.SearchResult, limit int) []map[string]any {
	if len(results) == 0 || limit <= 0 {
		return nil
	}
	if limit > len(results) {
		limit = len(results)
	}
	preview := make([]map[string]any, 0, limit)
	for i := 0; i < limit; i++ {
		item := results[i]
		preview = append(preview, map[string]any{
			"document_id": item.Record.ObjectID,
			"modality":    string(item.Record.Modality),
			"model":       item.Record.Model,
			"distance":    item.Distance,
			"file_name":   getStringFromMeta(item.Record.Metadata, "file_name"),
		})
	}
	return preview
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func assignString(meta map[string]interface{}, key string, target *string) {
	if target == nil || meta == nil {
		return
	}
	if value, ok := meta[key]; ok {
		switch v := value.(type) {
		case string:
			if v != "" {
				*target = v
			}
		case fmt.Stringer:
			str := v.String()
			if str != "" {
				*target = str
			}
		case []byte:
			str := string(v)
			if str != "" {
				*target = str
			}
		default:
			str := fmt.Sprintf("%v", v)
			if strings.TrimSpace(str) != "" {
				*target = str
			}
		}
	}
}

func getStringFromMeta(meta map[string]interface{}, key string) string {
	if meta == nil {
		return ""
	}
	if value, ok := meta[key]; ok {
		switch v := value.(type) {
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

func vectorDocumentToFullText(v VectorDocument) FullTextDocument {
	meta := v.Metadata
	fileName := v.FileName
	if fileName == "" {
		fileName = getStringFromMeta(meta, "file_name")
	}
	if fileName == "" {
		fileName = v.DocumentID
	}

	bucket := v.Bucket
	if bucket == "" {
		bucket = getStringFromMeta(meta, "bucket")
	}

	relativePath := v.RelativePath
	if relativePath == "" {
		relativePath = getStringFromMeta(meta, "relative_path")
	}

	contentPreview := v.ContentPreview
	if contentPreview == "" {
		contentPreview = getStringFromMeta(meta, "content_preview")
	}

	contentType := getStringFromMeta(meta, "content_type")

	doc := FullTextDocument{
		DocumentID:     v.DocumentID,
		AssetID:        v.AssetID,
		Score:          v.Score,
		ResourceID:     v.ResourceID,
		ResourceName:   v.ResourceName,
		ResourceType:   v.ResourceType,
		Bucket:         bucket,
		RelativePath:   relativePath,
		FileName:       fileName,
		DocumentType:   v.Modality,
		ContentPreview: contentPreview,
		ContentType:    contentType,
		Metadata:       meta,
		Highlights: map[string][]string{
			"vector_match": {fmt.Sprintf("semantic match (distance=%.4f)", v.Distance)},
		},
	}

	if doc.Title == "" {
		doc.Title = getStringFromMeta(meta, "title")
	}
	return doc
}

// mapMeilisearchHit 将 Meilisearch 搜索结果映射为 FullTextDocument
func mapMeilisearchHit(hit interface{}) FullTextDocument {
	hitMap, ok := hit.(map[string]interface{})
	if !ok {
		return FullTextDocument{}
	}

	doc := FullTextDocument{}

	// 基础字段
	if val, ok := hitMap["document_id"].(string); ok {
		doc.DocumentID = val
	}
	if val, ok := hitMap["asset_id"].(string); ok {
		doc.AssetID = val
	}
	if val, ok := hitMap["resource_id"].(float64); ok {
		doc.ResourceID = uint(val)
	}
	if val, ok := hitMap["resource_name"].(string); ok {
		doc.ResourceName = val
	}
	if val, ok := hitMap["resource_type"].(string); ok {
		doc.ResourceType = val
	}
	if val, ok := hitMap["bucket"].(string); ok {
		doc.Bucket = val
		doc.Schema = val // bucket 等价于 schema
	}
	if val, ok := hitMap["relative_path"].(string); ok {
		doc.RelativePath = val
		if doc.Bucket != "" {
			doc.ObjectKey = fmt.Sprintf("%s/%s", doc.Bucket, strings.TrimLeft(val, "/"))
		}
	}
	if val, ok := hitMap["file_name"].(string); ok {
		doc.FileName = val
	}
	if val, ok := hitMap["document_type"].(string); ok {
		doc.DocumentType = val
	}
	if val, ok := hitMap["title"].(string); ok {
		doc.Title = val
	}
	if val, ok := hitMap["author"].(string); ok {
		doc.Author = val
	}
	if val, ok := hitMap["content_preview"].(string); ok {
		doc.ContentPreview = val
	}
	if val, ok := hitMap["content_type"].(string); ok {
		doc.ContentType = val
	}
	if val, ok := hitMap["file_size"].(float64); ok {
		doc.FileSize = int64(val)
	}
	if val, ok := hitMap["word_count"].(float64); ok {
		doc.WordCount = int(val)
	}
	if val, ok := hitMap["page_count"].(float64); ok {
		doc.PageCount = int(val)
	}
	if val, ok := hitMap["last_modified"].(string); ok {
		doc.LastModified = normalizeTimeString(val)
	}
	if val, ok := hitMap["created_date"].(string); ok {
		doc.CreatedDate = normalizeTimeString(val)
	}
	if val, ok := hitMap["modified_date"].(string); ok {
		doc.ModifiedDate = normalizeTimeString(val)
	}

	// keywords 数组
	if val, ok := hitMap["keywords"].([]interface{}); ok {
		keywords := make([]string, 0, len(val))
		for _, kw := range val {
			if s, ok := kw.(string); ok {
				keywords = append(keywords, s)
			}
		}
		doc.Keywords = keywords
	}

	// metadata 对象
	if val, ok := hitMap["metadata"].(map[string]interface{}); ok {
		doc.Metadata = val
	}

	// 高亮结果 (_formatted 字段)
	if formatted, ok := hitMap["_formatted"].(map[string]interface{}); ok {
		highlights := make(map[string][]string)
		for key, val := range formatted {
			if strVal, ok := val.(string); ok && strings.Contains(strVal, "<mark>") {
				highlights[key] = []string{strVal}
			}
		}
		doc.Highlights = highlights
	}

	// 相关度分数
	if score, ok := hitMap["_rankingScore"].(float64); ok {
		doc.Score = score
	}

	return doc
}

// --- 内部辅助结构 ---

func normalizeTimeString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC().Format(time.RFC3339)
	}
	// 已经是其他格式，直接返回
	return value
}
