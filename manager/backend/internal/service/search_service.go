package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/embedding"
	"github.com/addp/common/logger"
	"github.com/addp/common/vectorstore"
	"github.com/addp/manager/internal/config"
	"github.com/elastic/go-elasticsearch/v8"
	"log/slog"
)

var (
	// ErrFullTextSearchDisabled 在未配置 ES 时返回
	ErrFullTextSearchDisabled = errors.New("full text search is not configured")

	highlightPreTags  = []string{"<mark>"}
	highlightPostTags = []string{"</mark>"}
)

const (
	queryEmbeddingRuneLimit = 3500
)

// FullTextSearchService 提供全文检索能力
type FullTextSearchService struct {
	client            *elasticsearch.Client
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
		documentIndex: strings.TrimSpace(cfg.ElasticsearchDocumentIndex),
		assetIndex:    strings.TrimSpace(cfg.ElasticsearchAssetIndex),
		enabled:       strings.TrimSpace(cfg.ElasticsearchURL) != "",
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
		svc.log.Info("Elasticsearch 未配置，全文检索功能已禁用")
		return svc, nil
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
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 - 由配置项控制
		}
	}

	client, err := elasticsearch.NewClient(esCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create elasticsearch client: %w", err)
	}
	svc.client = client

	svc.log.Info("全文检索服务已启用",
		"document_index", svc.documentIndex,
		"asset_index", svc.assetIndex,
		"url", cfg.ElasticsearchURL,
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

	from := (page - 1) * pageSize

	boolFilters := make([]interface{}, 0, 2)
	if tenantID != nil {
		boolFilters = append(boolFilters, map[string]interface{}{
			"term": map[string]interface{}{
				"tenant_id": *tenantID,
			},
		})
	}

	body := map[string]interface{}{
		"from": from,
		"size": pageSize,
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": []interface{}{
					map[string]interface{}{
						"multi_match": map[string]interface{}{
							"query":  query,
							"fields": []string{"title^3", "file_name^2", "content", "content_preview^2", "metadata.description", "metadata.tags"},
						},
					},
				},
				"filter": boolFilters,
			},
		},
		"highlight": map[string]interface{}{
			"pre_tags":  highlightPreTags,
			"post_tags": highlightPostTags,
			"fields": map[string]interface{}{
				"content":          map[string]interface{}{"fragment_size": 120, "number_of_fragments": 3},
				"content_preview":  map[string]interface{}{"fragment_size": 120, "number_of_fragments": 3},
				"title":            map[string]interface{}{"fragment_size": 80, "number_of_fragments": 1},
				"file_name":        map[string]interface{}{"fragment_size": 80, "number_of_fragments": 1},
				"metadata.tags":    map[string]interface{}{"fragment_size": 40, "number_of_fragments": 1},
				"metadata.summary": map[string]interface{}{"fragment_size": 100, "number_of_fragments": 1},
			},
		},
		"_source": []string{
			"document_id",
			"asset_id",
			"tenant_id",
			"resource_id",
			"resource_name",
			"resource_type",
			"bucket",
			"relative_path",
			"file_name",
			"document_type",
			"title",
			"author",
			"keywords",
			"content_preview",
			"content_type",
			"file_size",
			"word_count",
			"page_count",
			"last_modified",
			"created_date",
			"modified_date",
			"metadata",
		},
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to encode search body: %w", err)
	}

	res, err := s.client.Search(
		s.client.Search.WithContext(ctx),
		s.client.Search.WithIndex(s.documentIndex),
		s.client.Search.WithBody(bytes.NewReader(bodyBytes)),
		s.client.Search.WithTrackTotalHits(true),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to execute search: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("search request failed: %s", res.String())
	}

	var response esSearchResponse
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	result := &FullTextSearchResult{
		Total:    response.Hits.Total.Value,
		Page:     page,
		PageSize: pageSize,
		Hits:     make([]FullTextDocument, 0, len(response.Hits.Hits)),
	}

	for _, hit := range response.Hits.Hits {
		doc := mapDocumentHit(hit)

		if tenantID != nil && doc.ResourceID == 0 {
			// 若 ES 索引中缺失 resource_id，则尝试跳过避免无效结果
			continue
		}

		result.Hits = append(result.Hits, doc)
	}

	if s.vectorStore != nil && s.textEmbedder != nil {
		vectorHits, err := s.vectorSearch(ctx, tenantID, query)
		if err != nil {
			s.log.Warn("向量检索失败，已忽略", "error", err)
		} else {
			result.VectorHits = vectorHits
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

// --- 内部辅助结构 ---

type esSearchResponse struct {
	Hits struct {
		Total struct {
			Value int `json:"value"`
		} `json:"total"`
		Hits []struct {
			Index     string              `json:"_index"`
			ID        string              `json:"_id"`
			Score     float64             `json:"_score"`
			Source    documentSource      `json:"_source"`
			Highlight map[string][]string `json:"highlight"`
		} `json:"hits"`
	} `json:"hits"`
}

type documentSource struct {
	DocumentID     string                 `json:"document_id"`
	AssetID        string                 `json:"asset_id"`
	TenantID       interface{}            `json:"tenant_id"`
	ResourceID     interface{}            `json:"resource_id"`
	ResourceName   string                 `json:"resource_name"`
	ResourceType   string                 `json:"resource_type"`
	Bucket         string                 `json:"bucket"`
	RelativePath   string                 `json:"relative_path"`
	FileName       string                 `json:"file_name"`
	DocumentType   string                 `json:"document_type"`
	Title          string                 `json:"title"`
	Author         string                 `json:"author"`
	Keywords       []string               `json:"keywords"`
	ContentPreview string                 `json:"content_preview"`
	ContentType    string                 `json:"content_type"`
	FileSize       interface{}            `json:"file_size"`
	WordCount      interface{}            `json:"word_count"`
	PageCount      interface{}            `json:"page_count"`
	LastModified   string                 `json:"last_modified"`
	CreatedDate    string                 `json:"created_date"`
	ModifiedDate   string                 `json:"modified_date"`
	Metadata       map[string]interface{} `json:"metadata"`
}

func mapDocumentHit(hit struct {
	Index     string              `json:"_index"`
	ID        string              `json:"_id"`
	Score     float64             `json:"_score"`
	Source    documentSource      `json:"_source"`
	Highlight map[string][]string `json:"highlight"`
}) FullTextDocument {
	source := hit.Source

	resourceID := toUint(source.ResourceID)
	bucket := strings.TrimSpace(source.Bucket)
	relativePath := strings.TrimSpace(source.RelativePath)
	objectKey := ""
	if bucket != "" && relativePath != "" {
		objectKey = strings.TrimLeft(relativePath, "/")
		objectKey = fmt.Sprintf("%s/%s", bucket, objectKey)
	}

	result := FullTextDocument{
		DocumentID:     source.DocumentID,
		AssetID:        source.AssetID,
		Score:          hit.Score,
		ResourceID:     resourceID,
		ResourceName:   source.ResourceName,
		ResourceType:   source.ResourceType,
		Bucket:         bucket,
		Schema:         bucket, // 对象存储中 bucket 等价于 schema
		RelativePath:   relativePath,
		ObjectKey:      objectKey,
		FileName:       source.FileName,
		DocumentType:   source.DocumentType,
		Title:          source.Title,
		Author:         source.Author,
		Keywords:       source.Keywords,
		ContentPreview: source.ContentPreview,
		ContentType:    source.ContentType,
		FileSize:       toInt64(source.FileSize),
		WordCount:      int(toInt64(source.WordCount)),
		PageCount:      int(toInt64(source.PageCount)),
		LastModified:   normalizeTimeString(source.LastModified),
		CreatedDate:    normalizeTimeString(source.CreatedDate),
		ModifiedDate:   normalizeTimeString(source.ModifiedDate),
		Metadata:       source.Metadata,
		Highlights:     hit.Highlight,
	}

	return result
}

func toUint(value interface{}) uint {
	switch v := value.(type) {
	case nil:
		return 0
	case float64:
		if v < 0 {
			return 0
		}
		return uint(v)
	case json.Number:
		if i64, err := v.Int64(); err == nil && i64 > 0 {
			return uint(i64)
		}
	case string:
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			if i64, err := strconv.ParseInt(trimmed, 10, 64); err == nil && i64 > 0 {
				return uint(i64)
			}
		}
	}
	return 0
}

func toInt64(value interface{}) int64 {
	switch v := value.(type) {
	case nil:
		return 0
	case float64:
		return int64(v)
	case json.Number:
		i64, _ := v.Int64()
		return i64
	case string:
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			if i64, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
				return i64
			}
		}
	}
	return 0
}

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
