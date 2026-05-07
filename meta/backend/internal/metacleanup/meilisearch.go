package metacleanup

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/search"
	"github.com/meilisearch/meilisearch-go"
)

type MeilisearchGarbageStats struct {
	TotalCount int
	ByType     map[string]int
	Samples    []models.MeilisearchRecordInfo
}

type MeilisearchCleaner struct {
	indexer *search.Indexer
	log     *slog.Logger
}

func NewMeilisearchCleaner(indexer *search.Indexer, log *slog.Logger) *MeilisearchCleaner {
	return &MeilisearchCleaner{indexer: indexer, log: log}
}

func (c *MeilisearchCleaner) Enabled() bool {
	return c != nil && c.indexer != nil && c.indexer.Enabled()
}

func (c *MeilisearchCleaner) ScanGarbage(ctx context.Context, tenantID uint, invalidEngineIDs []uint) (*MeilisearchGarbageStats, error) {
	stats := &MeilisearchGarbageStats{
		ByType:  make(map[string]int),
		Samples: []models.MeilisearchRecordInfo{},
	}
	if !c.Enabled() || len(invalidEngineIDs) == 0 {
		return stats, nil
	}

	filterStr := engineFilter(invalidEngineIDs, tenantID)
	searchReq := &meilisearch.SearchRequest{
		Limit:  1000,
		Filter: filterStr,
	}

	resp, err := c.indexer.Client().Index(c.indexer.AssetIndexName()).Search("", searchReq)
	if err != nil {
		if c.log != nil {
			c.log.Error("扫描 Meilisearch 索引失败", "error", err)
		}
		return stats, err
	}

	stats.TotalCount = int(resp.EstimatedTotalHits)
	for i, hit := range resp.Hits {
		hitMap, ok := hit.(map[string]interface{})
		if !ok {
			continue
		}
		assetType := stringValue(hitMap, "asset_type")
		stats.ByType[assetType]++

		if i < 10 {
			stats.Samples = append(stats.Samples, models.MeilisearchRecordInfo{
				AssetID:   stringValue(hitMap, "asset_id"),
				AssetType: assetType,
				EngineID:  uintValue(hitMap, "engine_id"),
				TenantID:  uintValue(hitMap, "tenant_id"),
				Name:      stringValue(hitMap, "name"),
				Reason:    "引擎已删除或禁用",
			})
		}
	}
	return stats, nil
}

func (c *MeilisearchCleaner) ExecuteCleanup(ctx context.Context, tenantID uint, invalidEngineIDs []uint) (int, error) {
	if !c.Enabled() || len(invalidEngineIDs) == 0 {
		return 0, nil
	}

	filterStr := engineFilter(invalidEngineIDs, tenantID)
	searchReq := &meilisearch.SearchRequest{
		Limit:  0,
		Filter: filterStr,
	}

	resp, err := c.indexer.Client().Index(c.indexer.AssetIndexName()).Search("", searchReq)
	if err != nil {
		return 0, fmt.Errorf("查询待删除记录失败: %w", err)
	}

	count := int(resp.EstimatedTotalHits)
	if count == 0 {
		return 0, nil
	}

	task, err := c.indexer.Client().Index(c.indexer.AssetIndexName()).DeleteDocumentsByFilter(filterStr)
	if err != nil {
		return 0, fmt.Errorf("删除索引记录失败: %w", err)
	}

	if c.log != nil {
		c.log.Info("Meilisearch 索引清理完成",
			"index", c.indexer.AssetIndexName(),
			"tenant_id", tenantID,
			"engine_ids", invalidEngineIDs,
			"deleted_count", count,
			"task_uid", task.TaskUID,
		)
	}

	return count, nil
}

func (c *MeilisearchCleaner) DeleteByEngine(ctx context.Context, tenantID uint, engineID uint) error {
	if !c.Enabled() {
		return nil
	}

	filterStr := fmt.Sprintf("engine_id = %d", engineID)
	if tenantID > 0 {
		filterStr = fmt.Sprintf("%s AND tenant_id = %d", filterStr, tenantID)
	}

	task, err := c.indexer.Client().Index(c.indexer.AssetIndexName()).DeleteDocumentsByFilter(filterStr)
	if err != nil {
		return err
	}
	if c.log != nil {
		c.log.Info("Meilisearch 索引已删除", "engine_id", engineID, "task_uid", task.TaskUID)
	}
	return nil
}

func engineFilter(engineIDs []uint, tenantID uint) string {
	engineFilters := make([]string, len(engineIDs))
	for i, id := range engineIDs {
		engineFilters[i] = fmt.Sprintf("engine_id = %d", id)
	}

	filterStr := fmt.Sprintf("(%s)", strings.Join(engineFilters, " OR "))
	if tenantID > 0 {
		filterStr = fmt.Sprintf("%s AND tenant_id = %d", filterStr, tenantID)
	}
	return filterStr
}

func stringValue(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if str, ok := v.(string); ok {
			return str
		}
	}
	return ""
}

func uintValue(m map[string]interface{}, key string) uint {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case float64:
			return uint(val)
		case int64:
			return uint(val)
		case int:
			return uint(val)
		case uint:
			return val
		}
	}
	return 0
}
