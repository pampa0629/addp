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

// ScanDatabase 扫描 NoSQL 数据库及其所有对象
//
// 根据插件类型分流：
//   - DocumentDBPlugin：扫描 Collection
//   - GraphDBPlugin：扫描 NodeLabel + RelationshipType
//
// 返回：(database数量, 对象数量, 字段数量, error)
func (s *NoSQLScanService) ScanDatabase(
	ctx context.Context,
	nosqlPlugin plugin.NoSQLPlugin,
	resource *commonModels.Engine,
	tenantID uint,
	databaseName string,
	scanDepth string,
) (int, int, int, error) {

	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)

	// 1. 创建/更新 Database 节点
	dbNode, err := s.repo.UpsertNode(tenantID, resource.ID, nil, "database", databaseName, nil, nil)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to create database node: %w", err)
	}

	if err := s.repo.ResetNodeState(dbNode, "running"); err != nil {
		return 0, 0, 0, err
	}

	var totalObjects, totalFields int

	// 2. 按插件类型分流
	switch p := nosqlPlugin.(type) {
	case plugin.DocumentDBPlugin:
		totalObjects, totalFields, err = s.scanCollections(ctx, p, connInfo, resource, tenantID, dbNode, databaseName, scanDepth)
	case plugin.GraphDBPlugin:
		totalObjects, err = s.scanNodeLabels(ctx, p, connInfo, resource, tenantID, dbNode, databaseName)
		if err == nil {
			s.scanRelationshipTypes(ctx, p, connInfo, resource, tenantID, dbNode, databaseName)
		}
	default:
		s.repo.FinalizeNodeState(dbNode, "pending", 0, 0, "unsupported NoSQL plugin type")
		return 0, 0, 0, fmt.Errorf("unsupported NoSQL plugin type for engine %s", resource.EngineType)
	}

	if err != nil {
		s.repo.FinalizeNodeState(dbNode, "pending", 0, 0, err.Error())
		return 0, 0, 0, err
	}

	// 3. 完成扫描
	var totalSize int64
	for _, item := range getCollectionItems(s.db, tenantID, resource.ID, dbNode.ID) {
		if item.SizeBytes != nil {
			totalSize += *item.SizeBytes
		}
	}

	if err := s.repo.FinalizeNodeState(dbNode, "completed", totalObjects, totalSize, ""); err != nil {
		return 0, totalObjects, totalFields, err
	}

	return 1, totalObjects, totalFields, nil
}

// scanCollections 扫描文档型数据库的集合（DocumentDBPlugin 专用）
func (s *NoSQLScanService) scanCollections(
	ctx context.Context,
	docPlugin plugin.DocumentDBPlugin,
	connInfo plugin.ConnectionInfo,
	resource *commonModels.Engine,
	tenantID uint,
	dbNode *models.MetaNode,
	databaseName string,
	scanDepth string,
) (int, int, error) {
	isDeepScan := strings.EqualFold(scanDepth, "deep")

	collections, err := docPlugin.ListCollections(ctx, connInfo, databaseName)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list collections: %w", err)
	}

	s.log.Info("扫描到的集合", "database", databaseName, "collections_count", len(collections))

	existingCollectionMap := getExistingCollectionMap(s.db, tenantID, resource.ID, dbNode.ID)
	totalCollections := 0
	totalFields := 0
	scannedCollections := make(map[string]bool)

	for i, collInfo := range collections {
		s.log.Info(fmt.Sprintf("处理第 %d/%d 个集合", i+1, len(collections)),
			"collection_name", collInfo.Name,
			"document_count", collInfo.DocumentCount,
		)

		scannedCollections[collInfo.Name] = true

		existingItem := existingCollectionMap[collInfo.Name]
		needsUpdate := shouldUpdateCollection(existingItem, collInfo)

		if !isDeepScan && existingItem != nil && !needsUpdate {
			totalCollections++
			continue
		}

		var tableInfo *format.TableInfo
		var attrs models.JSONMap

		if isDeepScan {
			parser, err := format.GetDocCollectionParser(resource.EngineType)
			if err != nil {
				s.log.Warn("未找到 Parser，跳过 Schema 推断", "engine_type", resource.EngineType, "error", err)
			} else {
				client, err := docPlugin.CreateClient(ctx, connInfo)
				if err != nil {
					s.log.Warn("创建客户端失败，跳过 Schema 推断", "database", databaseName, "collection", collInfo.Name, "error", err)
				} else {
					defer docPlugin.CloseClient(ctx, client)
					options := format.DefaultParseOptions()
					options.SampleSize = 100
					tableInfo, err = parser.ParseTableInfo(ctx, client, databaseName, collInfo.Name, options)
					if err != nil {
						s.log.Warn("解析 TableInfo 失败", "database", databaseName, "collection", collInfo.Name, "error", err)
					} else {
						attrs = buildDocCollectionAttributes(tableInfo)
						totalFields += len(tableInfo.Fields)
					}
				}
			}
		}

		if attrs == nil {
			attrs = models.JSONMap{}
		}
		attrs["document_count"] = collInfo.DocumentCount
		attrs["size_bytes"] = collInfo.SizeBytes

		fullName := fmt.Sprintf("%s.%s", databaseName, collInfo.Name)
		docCount := collInfo.DocumentCount
		sizeBytes := collInfo.SizeBytes

		_, err = s.repo.UpsertItem(tenantID, resource.ID, dbNode, "collection", collInfo.Name, fullName, attrs, &docCount, &sizeBytes, nil)
		if err != nil {
			s.log.Warn("保存集合元数据失败", "database", databaseName, "collection", collInfo.Name, "error", err)
			continue
		}

		totalCollections++
	}

	s.softDeleteMissingCollections(tenantID, resource.ID, dbNode.ID, scannedCollections)
	return totalCollections, totalFields, nil
}

// scanNodeLabels 扫描图数据库的节点标签（GraphDBPlugin 专用）
func (s *NoSQLScanService) scanNodeLabels(
	ctx context.Context,
	graphPlugin plugin.GraphDBPlugin,
	connInfo plugin.ConnectionInfo,
	resource *commonModels.Engine,
	tenantID uint,
	dbNode *models.MetaNode,
	databaseName string,
) (int, error) {
	labels, err := graphPlugin.ListNodeLabels(ctx, connInfo, databaseName)
	if err != nil {
		return 0, fmt.Errorf("failed to list node labels: %w", err)
	}

	s.log.Info("扫描到的节点标签", "database", databaseName, "labels_count", len(labels))

	scannedLabels := make(map[string]bool)
	total := 0

	for _, label := range labels {
		scannedLabels[label.Name] = true

		attrs := models.JSONMap{
			"document_count": label.Count,
		}
		fullName := fmt.Sprintf("%s.%s", databaseName, label.Name)
		count := label.Count

		_, err := s.repo.UpsertItem(tenantID, resource.ID, dbNode, "label", label.Name, fullName, attrs, &count, nil, nil)
		if err != nil {
			s.log.Warn("保存节点标签元数据失败", "database", databaseName, "label", label.Name, "error", err)
			continue
		}
		total++
	}

	s.softDeleteMissingItemsByType(tenantID, resource.ID, dbNode.ID, "label", scannedLabels)
	return total, nil
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

// softDeleteMissingCollections 软删除不存在的集合（MongoDB）
func (s *NoSQLScanService) softDeleteMissingCollections(tenantID, engineID, dbNodeID uint, scannedCollections map[string]bool) {
	s.softDeleteMissingItemsByType(tenantID, engineID, dbNodeID, "collection", scannedCollections)
}

// softDeleteMissingItemsByType 按 item_type 软删除不存在的数据项
func (s *NoSQLScanService) softDeleteMissingItemsByType(tenantID, engineID, dbNodeID uint, itemType string, scanned map[string]bool) {
	var items []models.MetaItem
	s.db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND deleted_at IS NULL",
		tenantID, engineID, dbNodeID, itemType).Find(&items)

	for _, item := range items {
		if !scanned[item.Name] {
			s.log.Info("软删除不存在的数据项",
				"tenant_id", tenantID,
				"engine_id", engineID,
				"item_type", itemType,
				"name", item.Name,
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

// scanRelationshipTypes 扫描图数据库的关系类型并持久化为 MetaItem（item_type="relationship"）
func (s *NoSQLScanService) scanRelationshipTypes(
	ctx context.Context,
	graphPlugin plugin.GraphDBPlugin,
	connInfo plugin.ConnectionInfo,
	resource *commonModels.Engine,
	tenantID uint,
	dbNode *models.MetaNode,
	databaseName string,
) {
	relTypes, err := graphPlugin.ListRelationshipTypes(ctx, connInfo, databaseName)
	if err != nil {
		s.log.Warn("扫描关系类型失败", "database", databaseName, "error", err)
		return
	}

	s.log.Info("扫描到的关系类型", "database", databaseName, "count", len(relTypes))

	scannedRelTypes := make(map[string]bool)
	for _, rel := range relTypes {
		scannedRelTypes[rel.Name] = true

		attrs := models.JSONMap{
			"count":       rel.Count,
			"from_labels": rel.FromLabels,
			"to_labels":   rel.ToLabels,
		}

		fullName := fmt.Sprintf("%s.%s", databaseName, rel.Name)
		count := rel.Count
		_, err := s.repo.UpsertItem(tenantID, resource.ID, dbNode, "relationship", rel.Name, fullName, attrs, &count, nil, nil)
		if err != nil {
			s.log.Warn("保存关系类型元数据失败", "database", databaseName, "rel_type", rel.Name, "error", err)
		}
	}

	// 软删除不再存在的关系类型
	var existingRels []models.MetaItem
	s.db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND deleted_at IS NULL",
		tenantID, resource.ID, dbNode.ID, "relationship").Find(&existingRels)
	for _, item := range existingRels {
		if !scannedRelTypes[item.Name] {
			s.db.Delete(&item)
		}
	}
}
