package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
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

// ScanDatabase 扫描 NoSQL 数据库及其所有对象。
// CatalogProvider 负责列出真实数据库、集合、标签和关系；DocumentMetadataSamplingProvider 用于文档 schema 深度推断。
func (s *NoSQLScanService) ScanDatabase(
	ctx context.Context,
	enginePlugin plugin.EnginePlugin,
	resource *commonModels.Engine,
	tenantID uint,
	databaseName string,
	scanDepth string,
) (int, int, int, error) {

	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	catalogProvider, ok := enginePlugin.(plugin.CatalogProvider)
	if !ok {
		return 0, 0, 0, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}
	samplingProvider, _ := enginePlugin.(plugin.DocumentMetadataSamplingProvider)

	// 1. 创建/更新 Database 节点
	dbNode, err := s.repo.UpsertNode(tenantID, resource.ID, nil, "database", databaseName, nil, nil)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to create database node: %w", err)
	}

	if err := s.repo.ResetNodeState(dbNode, "running"); err != nil {
		return 0, 0, 0, err
	}

	var totalObjects, totalFields int

	totalObjects, totalFields, err = s.scanCatalogItems(ctx, catalogProvider, samplingProvider, connInfo, resource, tenantID, dbNode, databaseName, scanDepth)

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

func (s *NoSQLScanService) scanCatalogItems(
	ctx context.Context,
	catalogProvider plugin.CatalogProvider,
	samplingProvider plugin.DocumentMetadataSamplingProvider,
	connInfo plugin.ConnectionInfo,
	resource *commonModels.Engine,
	tenantID uint,
	dbNode *models.MetaNode,
	databaseName string,
	scanDepth string,
) (int, int, error) {
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: resource.ID,
		Segments: []plugin.CatalogSegment{{
			Term: plugin.CatalogTermDatabase,
			Kind: plugin.CatalogKindNamespace,
			Name: databaseName,
		}},
	}, plugin.ListOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list catalog items: %w", err)
	}

	s.log.Info("扫描到的 NoSQL catalog item", "database", databaseName, "item_count", len(nodes))

	existingCollectionMap := getExistingCollectionMap(s.db, tenantID, resource.ID, dbNode.ID)
	scannedByType := map[string]map[string]bool{
		"collection":   {},
		"label":        {},
		"relationship": {},
	}
	totalItems := 0
	totalFields := 0

	for i, node := range nodes {
		itemType := noSQLItemType(node)
		if itemType == "" {
			continue
		}
		count, _ := int64Stat(node.Stats, countStatKey(itemType))
		sizeBytes, _ := int64Stat(node.Stats, "size_bytes")
		collInfo := plugin.CollectionInfo{
			Name:          node.Name,
			DocumentCount: count,
			SizeBytes:     sizeBytes,
		}

		s.log.Info(fmt.Sprintf("处理第 %d/%d 个 NoSQL catalog item", i+1, len(nodes)),
			"item_name", node.Name,
			"item_type", itemType,
			"count", count,
		)

		scannedByType[itemType][node.Name] = true

		existingItem := existingCollectionMap[collInfo.Name]
		needsUpdate := shouldUpdateCollection(existingItem, collInfo)

		if !strings.EqualFold(scanDepth, "deep") && existingItem != nil && !needsUpdate {
			totalItems++
			continue
		}

		var attrs models.JSONMap

		if itemType == "collection" && strings.EqualFold(scanDepth, "deep") && samplingProvider != nil {
			itemMetadata, err := samplingProvider.SampleDocumentMetadata(ctx, connInfo, documentCollectionCatalogPath(resource.ID, databaseName, collInfo.Name), plugin.MetadataOptions{
				IncludeSamples:    true,
				IncludeStatistics: true,
				IncludeIndexes:    true,
				SampleSize:        100,
			})
			if err != nil {
				s.log.Warn("文档集合 Schema 采样失败", "database", databaseName, "collection", collInfo.Name, "error", err)
			} else {
				attrs = buildDocCollectionAttributesFromMetadata(itemMetadata)
				totalFields += len(itemMetadata.Fields)
			}
		}

		if attrs == nil {
			attrs = models.JSONMap{}
		}
		for k, v := range node.Attributes {
			attrs[k] = v
		}
		if itemType == "relationship" {
			attrs["count"] = count
		} else {
			attrs["document_count"] = count
		}
		attrs["size_bytes"] = sizeBytes
		applyNoSQLDataItemAttributes(attrs, itemType, resource.EngineType)

		fullName := fmt.Sprintf("%s.%s", databaseName, collInfo.Name)
		rowCount := count

		_, err = s.repo.UpsertItem(tenantID, resource.ID, dbNode, itemType, collInfo.Name, fullName, attrs, &rowCount, &sizeBytes, nil)
		if err != nil {
			s.log.Warn("保存 NoSQL item 元数据失败", "database", databaseName, "item", collInfo.Name, "item_type", itemType, "error", err)
			continue
		}

		totalItems++
	}

	for itemType, scanned := range scannedByType {
		if len(scanned) == 0 {
			continue
		}
		s.softDeleteMissingItemsByType(tenantID, resource.ID, dbNode.ID, itemType, scanned)
	}
	if strings.EqualFold(resource.EngineType, "neo4j") {
		s.softDeleteLegacyGraphTableItems(tenantID, resource.ID, dbNode.ID)
	}
	return totalItems, totalFields, nil
}

func documentCollectionCatalogPath(engineID uint, database, collection string) plugin.CatalogPath {
	return plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
		Segments: []plugin.CatalogSegment{
			{Term: plugin.CatalogTermDatabase, Kind: plugin.CatalogKindNamespace, Name: database},
			{Term: plugin.CatalogTermCollection, Kind: plugin.CatalogKindCollection, Name: collection},
		},
	}
}

// buildDocCollectionAttributesFromMetadata 构建文档集合的属性
func buildDocCollectionAttributesFromMetadata(itemMetadata *plugin.ItemMetadata) models.JSONMap {
	attrs := models.JSONMap{}

	if itemMetadata == nil {
		return attrs
	}

	for key, value := range itemMetadata.Attributes {
		attrs[key] = value
	}
	schemaAttrs := map[string]interface{}{}
	if len(itemMetadata.Indexes) > 0 {
		indexes := make([]map[string]interface{}, 0, len(itemMetadata.Indexes))
		for _, idx := range itemMetadata.Indexes {
			indexes = append(indexes, map[string]interface{}{
				"name":       idx.Name,
				"fields":     idx.Fields,
				"is_unique":  idx.IsUnique,
				"index_type": idx.IndexType,
			})
		}
		attrs["indexes"] = indexes
		schemaAttrs["indexes"] = indexes
	}
	if len(itemMetadata.Fields) > 0 {
		fields := buildDocFieldAttributes(itemMetadata.Fields)
		attrs["fields"] = fields
		schemaAttrs["fields"] = fields
	}
	if len(schemaAttrs) > 0 {
		attrs["schema"] = schemaAttrs
	}

	return attrs
}

// buildDocFieldAttributes 构建文档字段属性列表
func buildDocFieldAttributes(fields []plugin.FieldInfo) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(fields))
	for _, field := range fields {
		fieldAttr := map[string]interface{}{
			"name":           field.Name,
			"type":           field.Type,
			"original_type":  field.NativeType,
			"nullable":       field.Nullable,
			"is_primary_key": field.PrimaryKey,
		}

		if field.Attributes != nil {
			if occurrenceRate, ok := field.Attributes["occurrence_rate"]; ok {
				fieldAttr["occurrence_rate"] = occurrenceRate
			}
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

func (s *NoSQLScanService) softDeleteLegacyGraphTableItems(tenantID, engineID, dbNodeID uint) {
	var items []models.MetaItem
	s.db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND deleted_at IS NULL",
		tenantID, engineID, dbNodeID, "table").Find(&items)

	for _, item := range items {
		s.log.Info("软删除旧图数据库 table 数据项",
			"tenant_id", tenantID,
			"engine_id", engineID,
			"item_id", item.ID,
			"name", item.Name,
		)
		s.db.Delete(&item)
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

func noSQLItemType(node plugin.CatalogNode) string {
	switch node.Kind {
	case plugin.CatalogKindCollection:
		return "collection"
	case plugin.CatalogKindLabel:
		return "label"
	case plugin.CatalogKindRelationship:
		return "relationship"
	default:
		return ""
	}
}

func countStatKey(itemType string) string {
	if itemType == "relationship" {
		return "count"
	}
	return "document_count"
}

func applyNoSQLDataItemAttributes(attrs models.JSONMap, itemType, engineType string) {
	setItemAttribute(attrs, "format", engineType)
	switch itemType {
	case "collection":
		setItemAttribute(attrs, "data_family", string(dataitem.DataFamilyTabular))
	case "label", "relationship":
		setItemAttribute(attrs, "data_family", string(dataitem.DataFamilyGraph))
	default:
		setItemAttribute(attrs, "data_family", string(dataitem.DataFamilyUnknown))
	}
}
