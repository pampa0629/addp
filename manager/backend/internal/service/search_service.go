package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	commonClient "github.com/addp/common/client"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/addp/common/dataprotection/projectionstore"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/logger"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/repository"
	"github.com/meilisearch/meilisearch-go"
	"log/slog"
)

var (
	// ErrSearchDisabled 在未配置搜索引擎时返回
	ErrSearchDisabled = errors.New("hybrid search is not configured")
	ErrSearchIsolated = errors.New("hybrid search outlet is isolated")
)

const (
	queryEmbeddingRuneLimit = 3500
	rrfRankConstant         = 60.0
)

// HybridSearchService 提供混合检索能力（全文检索 + 向量语义检索）
type HybridSearchService struct {
	client                meilisearch.ServiceManager
	contentIndex          string
	enabled               bool
	log                   *slog.Logger
	vectorRepo            *repository.EmbeddingRepository
	configurationProvider *EmbeddingConfigurationProvider
	bindingService        *InferenceScenarioBindingService
	inferenceClient       InferenceEmbeddingClient
	deliveries            *repository.ContentIndexDeliveryRepository
	protectionStore       *projectionstore.Store
	epoch                 string
	endpointID            string
	initialized           atomic.Bool
	ordinalReady          atomic.Bool
	searchSessions        *searchSessionRegistry
}

// SearchDocument 表示检索结果中的单个文档（混合检索的统一格式）
type SearchDocument struct {
	DocumentID     string                 `json:"document_id"`
	Locator        string                 `json:"locator,omitempty"`
	Score          float64                `json:"score"`                     // 归一化 RRF 融合分，取值范围 [0, 1]
	MatchMethods   []string               `json:"match_methods,omitempty"`   // 实际命中方式: ["keyword", "vector"]
	VectorDistance float64                `json:"vector_distance,omitempty"` // 向量距离（如果有向量匹配）
	EngineID       uint                   `json:"engine_id"`
	EngineName     string                 `json:"engine_name,omitempty"`
	EngineType     string                 `json:"engine_type,omitempty"`
	DataItemType   string                 `json:"data_item_type,omitempty"`
	FullName       string                 `json:"full_name,omitempty"`
	Bucket         string                 `json:"bucket,omitempty"`
	Schema         string                 `json:"schema,omitempty"`
	Path           string                 `json:"path,omitempty"` // 目录路径（以 / 结尾）
	Name           string                 `json:"name,omitempty"` // 文件名或表名
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
	FieldMatches   []SearchFieldMatch     `json:"field_matches,omitempty"` // 实际命中的已登记字段 | Matched registered fields
}

// SearchFieldMatch preserves the registered definition around a keyword match.
type SearchFieldMatch struct {
	Name       string            `json:"name"`
	DataType   string            `json:"data_type,omitempty"`
	Comment    string            `json:"comment,omitempty"`
	Highlights map[string]string `json:"highlights"` // name / data_type / comment 的实际高亮 | Actual highlights
}

// VectorDocument 表示向量检索结果
type VectorDocument struct {
	DocumentID     string                 `json:"document_id"`
	Locator        string                 `json:"locator,omitempty"`
	TenantID       uint                   `json:"tenant_id,omitempty"`
	EngineID       uint                   `json:"engine_id,omitempty"`
	Distance       float64                `json:"distance"`
	ModelProfileID string                 `json:"model_profile_id"`
	Title          string                 `json:"title,omitempty"`
	FileName       string                 `json:"file_name,omitempty"`
	EngineName     string                 `json:"engine_name,omitempty"`
	EngineType     string                 `json:"engine_type,omitempty"`
	Bucket         string                 `json:"bucket,omitempty"`
	Path           string                 `json:"path,omitempty"` // 目录路径（以 / 结尾）
	Name           string                 `json:"name,omitempty"` // 文件名
	ContentPreview string                 `json:"content_preview,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// NewHybridSearchService 构建混合检索服务（全文检索 + 向量检索）
func NewHybridSearchService(cfg *config.Config, vectorRepo *repository.EmbeddingRepository, configurationProvider *EmbeddingConfigurationProvider, bindingService *InferenceScenarioBindingService, inferenceClient InferenceEmbeddingClient, deliveries *repository.ContentIndexDeliveryRepository, protectionStore *projectionstore.Store) (*HybridSearchService, error) {
	if cfg == nil || deliveries == nil || protectionStore == nil {
		return nil, errors.New("content index requires durable delivery and protection stores")
	}
	svc := &HybridSearchService{
		contentIndex:          strings.TrimSpace(cfg.MeilisearchManagerContentIndex),
		searchSessions:        newSearchSessionRegistry(),
		enabled:               strings.TrimSpace(cfg.MeilisearchURL) != "",
		log:                   logger.With("component", "manager_hybrid_search"),
		vectorRepo:            vectorRepo,
		configurationProvider: configurationProvider,
		bindingService:        bindingService,
		inferenceClient:       inferenceClient,
		deliveries:            deliveries,
		protectionStore:       protectionStore,
		endpointID:            config.SearchEndpointID(cfg.MeilisearchURL),
	}
	epoch, err := deliveries.JoinConfiguredIndex(context.Background(), svc.contentIndex, svc.endpointID, svc.enabled)
	if errors.Is(err, repository.ErrContentIndexIsolated) {
		svc.log.Warn("本进程的搜索目标未获部署确认，暂停搜索", "index", svc.contentIndex)
		return svc, nil
	}
	if err != nil {
		return nil, err
	}
	svc.epoch = epoch

	if !svc.enabled {
		svc.log.Info("Meilisearch 未配置，混合检索功能已禁用")
		return svc, nil
	}

	// 创建 Meilisearch 客户端
	client := meilisearch.New(cfg.MeilisearchURL, meilisearch.WithAPIKey(cfg.MeilisearchMasterKey), meilisearch.WithCustomClient(&http.Client{Timeout: 5 * time.Second}), meilisearch.DisableRetries())
	svc.client = client

	// 初始化索引配置
	initCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := deliveries.InitializeIndex(initCtx, epoch, svc.initIndexes); err != nil {
		svc.log.Warn("搜索出口保持隔离，等待索引恢复")
		return svc, nil
	}
	svc.initialized.Store(true)
	if err := deliveries.ActivateIfSettled(context.Background(), epoch); err != nil && !errors.Is(err, repository.ErrContentIndexIsolated) {
		return nil, err
	}

	if err := svc.prepareSearchOrdinals(initCtx); err != nil {
		svc.log.Warn("检索序号准备尚未完成，搜索保持隔离")
	}

	svc.log.Info("混合检索配置已载入",
		"content_index", svc.contentIndex,
		"url", cfg.MeilisearchURL,
	)

	return svc, nil
}

// initIndexes 初始化 Meilisearch 索引配置
func (s *HybridSearchService) initIndexes(ctx context.Context) error {
	version, err := s.client.VersionWithContext(ctx)
	if err != nil {
		return err
	}
	// No fallback submission without correlation to older servers.
	if version == nil || !supportsContentTaskCorrelation(version.PkgVersion) {
		return errors.New("Meilisearch task correlation requires version 1.26 or later")
	}
	// Manager 创建并独占技术内容索引。
	existing, err := s.client.GetIndexWithContext(ctx, s.contentIndex)
	if err != nil {
		if !strings.Contains(err.Error(), "index_not_found") {
			return fmt.Errorf("failed to inspect Manager content index: %w", err)
		}
		task, createErr := s.client.CreateIndexWithContext(ctx, &meilisearch.IndexConfig{Uid: s.contentIndex, PrimaryKey: "id"})
		if createErr != nil {
			return fmt.Errorf("failed to create Manager content index: %w", createErr)
		}
		if err := s.waitMeilisearchTask(ctx, task.TaskUID); err != nil {
			return fmt.Errorf("failed to create Manager content index: %w", err)
		}
	} else if existing.PrimaryKey != "id" {
		return fmt.Errorf("Manager content index primary key is %q, expected id", existing.PrimaryKey)
	}

	// Searchable field order affects ranking. Filterable and sortable fields
	// are sets; Meilisearch may return them in a different order.
	idx := s.client.Index(s.contentIndex)
	searchable, err := idx.GetSearchableAttributesWithContext(ctx)
	if err != nil {
		return fmt.Errorf("read searchable attributes: %w", err)
	}
	filterable, err := idx.GetFilterableAttributesWithContext(ctx)
	if err != nil {
		return fmt.Errorf("read filterable attributes: %w", err)
	}
	sortable, err := idx.GetSortableAttributesWithContext(ctx)
	if err != nil {
		return fmt.Errorf("read sortable attributes: %w", err)
	}
	wantSearch := []string{"name", "title", "full_name", "content_preview", "description", "tags", "content", "fields.name", "fields.data_type", "fields.comment", "keywords", "author"}
	wantFilter := []string{"bucket", "data_item_type", "document_id", "document_type", "engine_id", "engine_type", "locator", "schema", "search_ordinal", "table_kind", "tenant_id"}
	wantSort := []string{"data_updated_at", "page_count", "row_count", "size_bytes", "word_count"}
	patch := &meilisearch.Settings{}
	if !slices.Equal(*searchable, wantSearch) {
		patch.SearchableAttributes = wantSearch
	}
	// The SDK can also return structured filter rules. Manager owns this
	// index and requires the plain attribute form for all of its filters.
	currentFilters := make([]string, 0, len(*filterable))
	for _, value := range *filterable {
		if name, ok := value.(string); ok {
			currentFilters = append(currentFilters, name)
		}
	}
	slices.Sort(currentFilters)
	if len(currentFilters) != len(*filterable) || !slices.Equal(currentFilters, wantFilter) {
		patch.FilterableAttributes = wantFilter
	}
	slices.Sort(*sortable)
	if !slices.Equal(*sortable, wantSort) {
		patch.SortableAttributes = wantSort
	}
	if patch.SearchableAttributes == nil && patch.FilterableAttributes == nil && patch.SortableAttributes == nil {
		return nil
	}
	task, err := idx.UpdateSettingsWithContext(ctx, patch)
	if err != nil {
		return fmt.Errorf("update Manager content index settings: %w", err)
	}
	if err := s.waitMeilisearchTask(ctx, task.TaskUID); err != nil {
		return fmt.Errorf("update Manager content index settings: %w", err)
	}
	s.log.Info("索引配置已更新", "index", s.contentIndex)
	return nil
}

func (s *HybridSearchService) waitMeilisearchTask(ctx context.Context, taskID int64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}
	completed, err := s.client.WaitForTaskWithContext(ctx, taskID, 50*time.Millisecond)
	if err != nil {
		return err
	}
	if completed == nil || completed.Status != meilisearch.TaskStatusSucceeded {
		status := meilisearch.TaskStatusUnknown
		if completed != nil {
			status = completed.Status
		}
		return fmt.Errorf("Meilisearch task %d finished with status %s", taskID, status)
	}
	return nil
}

// Enabled 返回混合检索是否可用
func (s *HybridSearchService) Enabled() bool {
	return s != nil && s.enabled && s.client != nil && s.contentIndex != ""
}

func mergeVectorSearchFacts(target *SearchDocument, vector SearchDocument) {
	target.VectorDistance = vector.VectorDistance
	if target.Locator == "" {
		target.Locator = vector.Locator
	}
	if target.EngineID == 0 {
		target.EngineID = vector.EngineID
	}
	if target.EngineName == "" {
		target.EngineName = vector.EngineName
	}
	if target.EngineType == "" {
		target.EngineType = vector.EngineType
	}
	if target.Bucket == "" {
		target.Bucket = vector.Bucket
	}
	if target.Path == "" {
		target.Path = vector.Path
	}
	if target.Name == "" {
		target.Name = vector.Name
	}
	if target.FileName == "" {
		target.FileName = vector.FileName
	}
}

func (s *HybridSearchService) Close() {
	if s.searchSessions != nil {
		s.searchSessions.closeAll()
	}
}

type ContentDocumentDeleteScope = commonClient.ManagerContentDeleteScope

func (s *HybridSearchService) DeleteContentDocuments(ctx context.Context, tenantID uint, scope ContentDocumentDeleteScope) error {
	if !s.Enabled() {
		return ErrSearchDisabled
	}
	if tenantID == 0 || scope.Validate() != nil {
		return errors.New("invalid Manager content document deletion scope")
	}
	filters := []string{fmt.Sprintf("tenant_id = %d", tenantID), fmt.Sprintf("engine_id = %d", scope.EngineID)}
	for field, value := range map[string]string{
		"data_item_type": scope.DataItemType, "schema": scope.Schema, "bucket": scope.Bucket,
	} {
		if value = strings.TrimSpace(value); value != "" {
			filters = append(filters, fmt.Sprintf("%s = '%s'", field, strings.ReplaceAll(value, "'", "\\'")))
		}
	}
	if value := strings.TrimSpace(scope.PathPrefix); value != "" {
		filters = append(filters, fmt.Sprintf("path ^= '%s'", strings.ReplaceAll(value, "'", "\\'")))
	}
	if len(scope.DocumentIDs) > 0 {
		ids := slices.Clone(scope.DocumentIDs)
		slices.Sort(ids)
		encoded, err := json.Marshal(ids)
		if err != nil {
			return err
		}
		filters = append(filters, "document_id IN "+string(encoded))
	}
	filter := strings.Join(filters, " AND ")
	op, err := s.deliveries.QueueDelete(ctx, s.epoch, int64(tenantID), filter, scope)
	if err != nil {
		return err
	}
	return s.waitContentDelivery(ctx, op)
}

func buildSearchFilter(tenantID, engineID *uint) string {
	filters := make([]string, 0, 2)
	if tenantID != nil && *tenantID > 0 {
		filters = append(filters, fmt.Sprintf("tenant_id = %d", *tenantID))
	}
	if engineID != nil && *engineID > 0 {
		filters = append(filters, fmt.Sprintf("engine_id = %d", *engineID))
	}
	return strings.Join(filters, " AND ")
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
	assignStringValue(meta[key], target)
}

func assignStringFromMetaAttributes(meta map[string]interface{}, section, key string, target *string) {
	if target == nil || meta == nil {
		return
	}
	if value := commonJSON.String(meta, section, key); value != "" {
		*target = value
	}
}

func assignStringValue(value interface{}, target *string) {
	if target == nil || value == nil {
		return
	}
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

func getStringFromMeta(meta map[string]interface{}, key string) string {
	if meta == nil {
		return ""
	}
	for _, section := range searchAttributeSectionsForKey(key) {
		if value := commonJSON.String(meta, section, key); value != "" {
			return value
		}
	}
	return ""
}

func searchAttributeSectionsForKey(key string) []string {
	switch key {
	case "bucket", "path", "name", "content_type", "file_name":
		return []string{"storage"}
	case "format", "document_type", "type":
		return []string{"item"}
	case "title", "page_count", "word_count":
		return []string{"type_info.document"}
	case "keywords", "created_date", "modified_date":
		return []string{"capabilities.extraction"}
	default:
		return nil
	}
}

func vectorDocumentToSearchDocument(v VectorDocument) SearchDocument {
	meta := v.Metadata

	bucket := v.Bucket
	if bucket == "" {
		bucket = getStringFromMeta(meta, "bucket")
	}

	// 从向量文档中获取路径和名称信息
	path := v.Path
	if path == "" {
		path = getStringFromMeta(meta, "path")
	}

	name := v.Name
	if name == "" {
		name = getStringFromMeta(meta, "name")
	}

	// 构建 fileName（优先使用标准 storage.name，然后是向量记录的 FileName 字段）
	fileName := name
	if fileName == "" {
		fileName = v.FileName
	}
	if fileName == "" {
		fileName = v.DocumentID
	}

	contentPreview := v.ContentPreview
	if contentPreview == "" {
		contentPreview = getStringFromMeta(meta, "content_preview")
	}

	contentType := getStringFromMeta(meta, "content_type")
	documentType := getStringFromMeta(meta, "document_type")

	doc := SearchDocument{
		DocumentID:     v.DocumentID,
		MatchMethods:   []string{"vector"}, // 标记为向量匹配
		VectorDistance: v.Distance,         // 保存向量距离
		EngineID:       v.EngineID,
		EngineName:     v.EngineName,
		EngineType:     v.EngineType,
		DataItemType:   "object",
		Bucket:         bucket,
		Path:           path,
		Name:           name,
		FileName:       fileName,
		DocumentType:   documentType,
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
	doc.Locator = normalizeSearchResultLocator(v.Locator)
	if doc.Locator == "" {
		doc.Locator = searchResultLocatorFromMetadata(meta)
	}
	return doc
}

// mapMeilisearchHit 将 Meilisearch 搜索结果映射为 SearchDocument
func mapMeilisearchHit(hit interface{}) SearchDocument {
	hitMap, ok := hit.(map[string]interface{})
	if !ok {
		return SearchDocument{}
	}

	doc := SearchDocument{}

	// 基础字段
	if val, ok := hitMap["document_id"].(string); ok {
		doc.DocumentID = val
	}
	if val, ok := hitMap["locator"].(string); ok {
		doc.Locator = normalizeSearchResultLocator(val)
	}
	if val, ok := hitMap["engine_id"].(float64); ok {
		doc.EngineID = uint(val)
	}
	if val, ok := hitMap["engine_name"].(string); ok {
		doc.EngineName = val
	}
	if val, ok := hitMap["engine_type"].(string); ok {
		doc.EngineType = val
	}
	if val, ok := hitMap["data_item_type"].(string); ok {
		doc.DataItemType = val
	}
	if val, ok := hitMap["schema"].(string); ok {
		doc.Schema = val
	}
	if val, ok := hitMap["bucket"].(string); ok {
		doc.Bucket = val
		if doc.Schema == "" {
			doc.Schema = val
		}
	}
	// 读取目录路径（以 / 结尾）
	if val, ok := hitMap["path"].(string); ok {
		doc.Path = val
	}
	// 内容投影使用 name 字段存储文件名。
	if val, ok := hitMap["name"].(string); ok {
		doc.Name = val
		doc.FileName = val
	}
	if val, ok := hitMap["full_name"].(string); ok {
		doc.FullName = val
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
	if val, ok := hitMap["size_bytes"].(float64); ok {
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
		doc.FieldMatches = matchedSearchFields(hitMap["fields"], formatted["fields"])
	}

	return doc
}

func searchResultLocatorFromMetadata(meta map[string]interface{}) string {
	if len(meta) == 0 {
		return ""
	}
	if locator := normalizeSearchResultLocator(meta["locator"]); locator != "" {
		return locator
	}
	if resource, ok := meta["resource"].(map[string]interface{}); ok {
		return normalizeSearchResultLocator(resource["locator"])
	}
	return ""
}

func normalizeSearchResultLocator(raw interface{}) string {
	locator, ok := raw.(string)
	if !ok {
		return ""
	}
	locator = strings.TrimSpace(locator)
	if locator == "" {
		return ""
	}
	parsed, err := resourcetree.ParseURI(locator)
	if err != nil || parsed == nil || parsed.ItemID == nil || *parsed.ItemID == 0 {
		return ""
	}
	return locator
}

func locatorLastSegment(locator string) string {
	parsed, err := resourcetree.ParseURI(locator)
	if err != nil || parsed == nil {
		return ""
	}
	return parsed.LastSegment()
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
