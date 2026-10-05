package search

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/meilisearch/meilisearch-go"
)

// AssetIndexDoc 资产索引文档结构
type AssetIndexDoc struct {
	ID           int64    `json:"id"` // Meilisearch 主键
	TenantID     int64    `json:"tenant_id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Tags         []string `json:"tags"`
	TypeCode     string   `json:"type_code"`
	TypeName     string   `json:"type_name"`
	CategoryID   *int64   `json:"category_id"`
	CategoryName string   `json:"category_name"`
	Status       string   `json:"status"`       // "published" | "offline"
	PublishedAt  string   `json:"published_at"` // RFC3339，用于排序
}

// Indexer 封装 asset 模块的 Meilisearch 操作
type Indexer struct {
	client  meilisearch.ServiceManager
	index   string
	enabled bool
}

// NewIndexer 创建索引器。若未配置 Meilisearch URL，返回禁用状态的索引器。
func NewIndexer(msURL, msAPIKey, indexName string) (*Indexer, error) {
	idx := &Indexer{
		index:   strings.TrimSpace(indexName),
		enabled: strings.TrimSpace(msURL) != "",
	}
	if idx.index == "" {
		idx.index = "asset_published"
	}

	if !idx.enabled {
		log.Println("ℹ️  Meilisearch 未配置，资产索引功能已禁用")
		return idx, nil
	}

	idx.client = meilisearch.New(msURL, meilisearch.WithAPIKey(msAPIKey), meilisearch.WithCustomClient(&http.Client{Timeout: 10 * time.Second}), meilisearch.DisableRetries())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := idx.ensureIndex(ctx); err != nil {
		return nil, fmt.Errorf("初始化 Meilisearch 索引失败: %w", err)
	}

	log.Printf("✅ Meilisearch 资产索引器已启用，index: %s", idx.index)
	return idx, nil
}

// Enabled 返回索引器是否启用
func (i *Indexer) Enabled() bool {
	return i != nil && i.enabled && i.client != nil
}

// ensureIndex 确保索引存在并配置正确
func (i *Indexer) ensureIndex(ctx context.Context) error {
	existing, err := i.client.GetIndexWithContext(ctx, i.index)
	if err != nil {
		var apiErr *meilisearch.Error
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound || apiErr.MeilisearchApiError.Code != "index_not_found" {
			return fmt.Errorf("读取索引失败: %w", err)
		}
		task, err := i.client.CreateIndexWithContext(ctx, &meilisearch.IndexConfig{Uid: i.index, PrimaryKey: "id"})
		if err != nil {
			return fmt.Errorf("创建索引失败: %w", err)
		}
		if err := i.waitTask(ctx, task); err != nil {
			return fmt.Errorf("创建索引失败: %w", err)
		}
	} else if existing == nil || existing.PrimaryKey != "id" {
		return fmt.Errorf("资产索引 %s 主键必须为 id", i.index)
	}

	assetIdx := i.client.Index(i.index)
	settings := []func() (*meilisearch.TaskInfo, error){
		func() (*meilisearch.TaskInfo, error) {
			return assetIdx.UpdateSearchableAttributesWithContext(ctx, &[]string{"name", "description", "tags", "type_name", "category_name"})
		},
		func() (*meilisearch.TaskInfo, error) {
			return assetIdx.UpdateFilterableAttributesWithContext(ctx, &[]interface{}{"tenant_id", "status", "type_code", "category_id"})
		},
		func() (*meilisearch.TaskInfo, error) {
			return assetIdx.UpdateSortableAttributesWithContext(ctx, &[]string{"published_at"})
		},
	}
	for _, update := range settings {
		task, err := update()
		if err != nil {
			return fmt.Errorf("配置索引属性失败: %w", err)
		}
		if err := i.waitTask(ctx, task); err != nil {
			return fmt.Errorf("配置索引属性失败: %w", err)
		}
	}
	return nil
}

func (i *Indexer) waitTask(ctx context.Context, receipt *meilisearch.TaskInfo) error {
	if receipt == nil || receipt.TaskUID < 0 {
		return fmt.Errorf("资产索引任务缺少有效回执")
	}
	task, err := i.client.WaitForTaskWithContext(ctx, receipt.TaskUID, 50*time.Millisecond)
	if err != nil {
		return err
	}
	if task == nil || task.UID != receipt.TaskUID || task.Status != meilisearch.TaskStatusSucceeded {
		return fmt.Errorf("资产索引任务 %d 未成功完成", receipt.TaskUID)
	}
	return nil
}

// UpsertAsset 写入/更新单个资产（上架时调用）
func (i *Indexer) UpsertAsset(doc *AssetIndexDoc) {
	if !i.Enabled() || doc == nil {
		return
	}
	if doc.Tags == nil {
		doc.Tags = []string{}
	}
	idx := i.client.Index(i.index)
	if _, err := idx.AddDocuments([]AssetIndexDoc{*doc}, nil); err != nil {
		log.Printf("⚠️  写入资产索引失败 (id=%d): %v", doc.ID, err)
	}
}

// UpsertAssets 批量写入资产（批量上架时调用）
func (i *Indexer) UpsertAssets(docs []AssetIndexDoc) {
	if !i.Enabled() || len(docs) == 0 {
		return
	}
	for idx := range docs {
		if docs[idx].Tags == nil {
			docs[idx].Tags = []string{}
		}
	}
	index := i.client.Index(i.index)
	if _, err := index.AddDocuments(docs, nil); err != nil {
		log.Printf("⚠️  批量写入资产索引失败 (count=%d): %v", len(docs), err)
	}
}

// ReplaceAssets rebuilds the derived search projection from authoritative Asset records.
func (i *Indexer) ReplaceAssets(docs []AssetIndexDoc) error {
	if !i.Enabled() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	index := i.client.Index(i.index)
	task, err := index.DeleteAllDocumentsWithContext(ctx, nil)
	if err != nil {
		return fmt.Errorf("清空资产索引失败: %w", err)
	}
	if err := i.waitTask(ctx, task); err != nil {
		return fmt.Errorf("清空资产索引失败: %w", err)
	}
	if len(docs) == 0 {
		return nil
	}
	for idx := range docs {
		if docs[idx].Tags == nil {
			docs[idx].Tags = []string{}
		}
	}
	task, err = index.AddDocumentsWithContext(ctx, docs, nil)
	if err != nil {
		return fmt.Errorf("重建资产索引失败: %w", err)
	}
	return i.waitTask(ctx, task)
}

// UpdateStatus 仅更新资产的 status 字段（下架时调用，文档保留在索引中）
func (i *Indexer) UpdateStatus(id int64, status string) {
	if !i.Enabled() {
		return
	}
	// Meilisearch 的 AddDocuments 在主键匹配时执行 upsert（合并更新）
	doc := map[string]interface{}{
		"id":     id,
		"status": status,
	}
	idx := i.client.Index(i.index)
	if _, err := idx.UpdateDocuments([]map[string]interface{}{doc}, nil); err != nil {
		log.Printf("⚠️  更新资产状态索引失败 (id=%d, status=%s): %v", id, status, err)
	}
}

// UpdateStatusBatch 批量更新资产 status（批量下架时调用）
func (i *Indexer) UpdateStatusBatch(ids []int64, status string) {
	if !i.Enabled() || len(ids) == 0 {
		return
	}
	docs := make([]map[string]interface{}, len(ids))
	for j, id := range ids {
		docs[j] = map[string]interface{}{
			"id":     id,
			"status": status,
		}
	}
	idx := i.client.Index(i.index)
	if _, err := idx.UpdateDocuments(docs, nil); err != nil {
		log.Printf("⚠️  批量更新资产状态索引失败 (count=%d, status=%s): %v", len(ids), status, err)
	}
}

// DeleteAsset removes the projection after the authoritative Asset is deleted.
func (i *Indexer) DeleteAsset(id int64) {
	if !i.Enabled() || id <= 0 {
		return
	}
	if _, err := i.client.Index(i.index).DeleteDocument(strconv.FormatInt(id, 10), nil); err != nil {
		log.Printf("⚠️  删除资产索引失败 (id=%d): %v", id, err)
	}
}

// SearchResult Meilisearch 搜索结果
type SearchResult struct {
	IDs   []int64
	Total int64
}

// Search 搜索已上架资产，返回匹配的资产 ID 列表（按相关度排序）
func (i *Indexer) Search(tenantID int64, keyword string, typeCode string, categoryIDs []int64, limit, offset int64) (*SearchResult, error) {
	if !i.Enabled() {
		return nil, nil
	}

	// 固定过滤：仅返回 published 状态 + 当前租户
	filters := buildAssetSearchFilters(tenantID, typeCode, categoryIDs)
	filterStr := strings.Join(filters, " AND ")

	req := &meilisearch.SearchRequest{
		Filter: filterStr,
		Limit:  limit,
		Offset: offset,
	}

	idx := i.client.Index(i.index)
	resp, err := idx.Search(keyword, req)
	if err != nil {
		return nil, fmt.Errorf("Meilisearch 搜索失败: %w", err)
	}

	result := &SearchResult{
		IDs:   make([]int64, 0, len(resp.Hits)),
		Total: resp.EstimatedTotalHits,
	}
	var hits []struct {
		ID int64 `json:"id"`
	}
	if err := resp.Hits.DecodeInto(&hits); err != nil {
		return nil, err
	}
	for _, hit := range hits {
		result.IDs = append(result.IDs, hit.ID)
	}

	return result, nil
}

func buildAssetSearchFilters(tenantID int64, typeCode string, categoryIDs []int64) []string {
	filters := []string{
		fmt.Sprintf("tenant_id = %d", tenantID),
		"status = \"published\"",
	}
	if typeCode != "" {
		filters = append(filters, fmt.Sprintf("type_code = \"%s\"", typeCode))
	}
	if categoryIDs == nil {
		return filters
	}
	if len(categoryIDs) == 1 && categoryIDs[0] == -1 {
		return append(filters, "category_id IS NULL")
	}
	categoryFilters := make([]string, 0, len(categoryIDs))
	for _, categoryID := range categoryIDs {
		categoryFilters = append(categoryFilters, fmt.Sprintf("category_id = %d", categoryID))
	}
	return append(filters, "("+strings.Join(categoryFilters, " OR ")+")")
}

// MeilisearchPublishedAt 将 *time.Time 格式化为索引用字符串
func MeilisearchPublishedAt(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
