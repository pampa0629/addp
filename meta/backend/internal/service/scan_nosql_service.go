package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/search"
	"gorm.io/gorm"
)

// NoSQLScanService NoSQL 数据库扫描服务
// 职责：扫描文档型数据库（MongoDB等）的Database、Collection、Field
type NoSQLScanService struct {
	db             *gorm.DB
	log            *slog.Logger
	indexer        *search.Indexer
	repo           *ScanRepository // 数据访问层
	indexerService *IndexerService // 索引服务
}

// NewNoSQLScanService 创建 NoSQL 扫描服务
func NewNoSQLScanService(db *gorm.DB, log *slog.Logger, indexer *search.Indexer, repo *ScanRepository, indexerService *IndexerService) *NoSQLScanService {
	return &NoSQLScanService{
		db:             db,
		log:            log,
		indexer:        indexer,
		repo:           repo,
		indexerService: indexerService,
	}
}

// ScanDatabase 扫描 NoSQL 数据库及其所有集合
//
// 职责划分：
// 1. Database节点管理：创建/更新Database节点，管理扫描状态
// 2. Collection迭代处理：扫描所有集合，判断是否需要更新
// 3. 字段扫描：深度扫描时通过 Parser 推断 Schema
// 4. 搜索索引：将集合资产信息同步到Meilisearch
// 5. 软删除处理：清理已删除的集合
//
// 参数：
//   - ctx: 上下文
//   - nosqlPlugin: NoSQL插件
//   - resource: 数据源引擎配置
//   - tenantID: 租户ID
//   - databaseName: 数据库名称
//   - scanDepth: 扫描深度 ("quick"快速扫描 | "deep"深度扫描)
//
// 返回：(database数量, 集合数量, 字段数量, error)
func (s *NoSQLScanService) ScanDatabase(
	ctx context.Context,
	nosqlPlugin plugin.NoSQLPlugin,
	resource *commonModels.Engine,
	tenantID uint,
	databaseName string,
	scanDepth string,
) (int, int, int, error) {

	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	isDeepScan := strings.EqualFold(scanDepth, "deep")

	// 1. 创建/更新 Database 节点 (NodeType = "database")
	dbNode, err := s.repo.UpsertNode(tenantID, resource.ID, nil, "database", databaseName, "", nil)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to create database node: %w", err)
	}

	if err := s.repo.ResetNodeState(dbNode, "扫描中"); err != nil {
		return 0, 0, 0, err
	}

	// 2. 列出所有集合
	collections, err := nosqlPlugin.ListCollections(ctx, connInfo, databaseName)
	if err != nil {
		s.repo.FinalizeNodeState(dbNode, "未扫描", 0, 0, err.Error())
		return 0, 0, 0, fmt.Errorf("failed to list collections: %w", err)
	}

	s.log.Info("扫描到的集合",
		"database", databaseName,
		"collections_count", len(collections),
	)

	// 查询已存在的集合
	existingCollectionMap := getExistingCollectionMap(s.db, tenantID, resource.ID, dbNode.ID)

	totalCollections := 0
	totalFields := 0
	scannedCollections := make(map[string]bool)

	// 3. 扫描每个集合
	for i, collInfo := range collections {
		s.log.Info(fmt.Sprintf("处理第 %d/%d 个集合", i+1, len(collections)),
			"collection_name", collInfo.Name,
			"document_count", collInfo.DocumentCount,
		)

		scannedCollections[collInfo.Name] = true

		// 检查是否需要更新
		existingItem := existingCollectionMap[collInfo.Name]
		needsUpdate := shouldUpdateCollection(existingItem, collInfo)

		// 浅度扫描且集合未变化，跳过
		if !isDeepScan && existingItem != nil && !needsUpdate {
			s.log.Debug("集合未变化，跳过更新",
				"database", databaseName,
				"collection", collInfo.Name,
			)
			totalCollections++
			continue
		}

		// 深度扫描: 使用 Parser 推断 Schema
		var tableInfo *format.TableInfo
		var attrs models.JSONMap

		if isDeepScan {
			// 获取 Parser
			parser, err := format.GetDocCollectionParser(resource.EngineType)
			if err != nil {
				s.log.Warn("未找到 Parser，跳过 Schema 推断",
					"engine_type", resource.EngineType,
					"error", err,
				)
			} else {
				// 创建客户端
				client, err := nosqlPlugin.CreateClient(ctx, connInfo)
				if err != nil {
					s.log.Warn("创建客户端失败，跳过 Schema 推断",
						"database", databaseName,
						"collection", collInfo.Name,
						"error", err,
					)
				} else {
					defer nosqlPlugin.CloseClient(ctx, client)

					// 解析 TableInfo
					options := format.DefaultParseOptions()
					options.SampleSize = 100
					tableInfo, err = parser.ParseTableInfo(ctx, client, databaseName, collInfo.Name, options)
					if err != nil {
						s.log.Warn("解析 TableInfo 失败",
							"database", databaseName,
							"collection", collInfo.Name,
							"error", err,
						)
					} else {
						// 构建 Attributes
						attrs = buildDocCollectionAttributes(tableInfo)
						totalFields += len(tableInfo.Fields)
					}
				}
			}
		}

		// 如果未深度扫描或 Schema 推断失败，使用基本信息
		if attrs == nil {
			attrs = models.JSONMap{}
		}

		// 存储统计信息
		attrs["document_count"] = collInfo.DocumentCount
		attrs["size_bytes"] = collInfo.SizeBytes

		// 4. 持久化集合元数据 (ItemType = "collection")
		fullName := fmt.Sprintf("%s.%s", databaseName, collInfo.Name)
		docCount := collInfo.DocumentCount
		sizeBytes := collInfo.SizeBytes

		_, err = s.repo.UpsertItem(tenantID, resource.ID, dbNode, "collection", collInfo.Name, fullName, attrs, &docCount, &sizeBytes, nil)
		if err != nil {
			s.log.Warn("保存集合元数据失败",
				"database", databaseName,
				"collection", collInfo.Name,
				"error", err,
			)
			continue
		}

		totalCollections++
	}

	// 5. 软删除不存在的集合
	s.softDeleteMissingCollections(tenantID, resource.ID, dbNode.ID, scannedCollections)

	// 6. 完成扫描
	var totalSize int64
	for _, item := range getCollectionItems(s.db, tenantID, resource.ID, dbNode.ID) {
		if item.SizeBytes != nil {
			totalSize += *item.SizeBytes
		}
	}

	if err := s.repo.FinalizeNodeState(dbNode, "已扫描", totalCollections, totalSize, ""); err != nil {
		return 0, totalCollections, totalFields, err
	}

	return 1, totalCollections, totalFields, nil
}

// buildDocCollectionAttributes 构建文档集合的属性
func buildDocCollectionAttributes(tableInfo *format.TableInfo) models.JSONMap {
	attrs := models.JSONMap{}

	// 提取 DocCollectionInfo
	if docInfo := tableInfo.GetDocCollectionInfo(); docInfo != nil {
		attrs["is_sampled"] = docInfo.IsSampled
		attrs["sample_size"] = docInfo.SampleSize
		attrs["schema_type"] = docInfo.SchemaType
		attrs["total_documents"] = docInfo.TotalDocuments

		// 索引信息
		if len(docInfo.Indexes) > 0 {
			indexes := make([]map[string]interface{}, 0, len(docInfo.Indexes))
			for _, idx := range docInfo.Indexes {
				indexes = append(indexes, map[string]interface{}{
					"name":       idx.Name,
					"fields":     idx.Fields,
					"is_unique":  idx.IsUnique,
					"index_type": idx.IndexType,
				})
			}
			attrs["indexes"] = indexes
		}
	}

	// 存储字段信息
	if len(tableInfo.Fields) > 0 {
		attrs["fields"] = buildDocFieldAttributes(tableInfo.Fields)
	}

	return attrs
}

// buildDocFieldAttributes 构建文档字段属性列表
func buildDocFieldAttributes(fields []format.FieldInfo) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(fields))
	for _, field := range fields {
		fieldAttr := map[string]interface{}{
			"name":          field.Name,
			"type":          string(field.Type),
			"original_type": field.OriginalType,
			"nullable":      field.Nullable,
			"is_primary_key": field.IsPrimaryKey,
		}

		// 添加 OccurrenceRate（如果非零）
		if field.OccurrenceRate > 0 {
			fieldAttr["occurrence_rate"] = field.OccurrenceRate
		}

		result = append(result, fieldAttr)
	}
	return result
}

// shouldUpdateCollection 判断集合是否需要更新
func shouldUpdateCollection(existingItem *models.MetaItem, newInfo plugin.CollectionInfo) bool {
	if existingItem == nil {
		return true
	}

	// 如果文档数量变化，需要更新
	if existingItem.RowCount != nil && *existingItem.RowCount != newInfo.DocumentCount {
		return true
	}

	// 如果大小变化超过 10%，需要更新
	if existingItem.SizeBytes != nil && newInfo.SizeBytes > 0 {
		oldSize := *existingItem.SizeBytes
		change := float64(abs64(newInfo.SizeBytes-oldSize)) / float64(oldSize)
		if change > 0.1 {
			return true
		}
	}

	return false
}

// abs64 返回 int64 的绝对值
func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// softDeleteMissingCollections 软删除不存在的集合
func (s *NoSQLScanService) softDeleteMissingCollections(tenantID, engineID, dbNodeID uint, scannedCollections map[string]bool) {
	var items []models.MetaItem
	s.db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND deleted_at IS NULL",
		tenantID, engineID, dbNodeID, "collection").Find(&items)

	for _, item := range items {
		if !scannedCollections[item.Name] {
			s.log.Info("软删除不存在的集合",
				"tenant_id", tenantID,
				"engine_id", engineID,
				"collection", item.Name,
			)
			s.db.Delete(&item)
		}
	}
}

// getExistingCollectionMap 获取已存在的集合映射
func getExistingCollectionMap(db *gorm.DB, tenantID, engineID, dbNodeID uint) map[string]*models.MetaItem {
	var items []models.MetaItem
	db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND deleted_at IS NULL",
		tenantID, engineID, dbNodeID, "collection").Find(&items)

	result := make(map[string]*models.MetaItem)
	for i := range items {
		result[items[i].Name] = &items[i]
	}
	return result
}

// getCollectionItems 获取集合项列表
func getCollectionItems(db *gorm.DB, tenantID, engineID, dbNodeID uint) []models.MetaItem {
	var items []models.MetaItem
	db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND deleted_at IS NULL",
		tenantID, engineID, dbNodeID, "collection").Find(&items)
	return items
}
