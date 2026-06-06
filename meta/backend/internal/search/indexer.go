package search

import (
	"fmt"
	"strings"
	"sync"

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
		"name",            // 文件名/表名 - 最高权重
		"title",           // 文档标题
		"full_name",       // 完整路径名
		"path",            // 目录路径（用于路径搜索）
		"content_preview", // 内容预览（中等权重）
		"description",     // 描述
		"tags",            // 标签
		"content",         // 全文内容（权重较低但范围广）
		"fields.name",     // 表字段名
		"fields.comment",  // 字段注释
		"keywords",        // 关键词
		"author",          // 作者
	})
	if err != nil {
		return fmt.Errorf("failed to update asset searchable attributes: %w", err)
	}

	// 设置可过滤字段
	_, err = assetIndex.UpdateFilterableAttributes(&[]string{
		"tenant_id",
		"engine_id",
		"engine_type",
		"asset_type", // 可过滤表/对象
		"locator",
		"schema",
		"bucket",
		"table_kind",
		"document_type", // 可过滤文档类型
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
