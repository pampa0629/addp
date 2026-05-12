package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanchange"
	"github.com/addp/meta/internal/search"
	"gorm.io/gorm"
)

// NoSQLScanService NoSQL 数据库扫描服务
// 职责：扫描文档型数据库（MongoDB等）的Database、Collection、Field
type NoSQLScanService struct {
	db             *gorm.DB
	log            *slog.Logger
	indexer        *search.Indexer
	repo           *metaRepo.ScanRepository // 数据访问层
	indexerService *IndexerService          // 索引服务
}

// NewNoSQLScanService 创建 NoSQL 扫描服务
func NewNoSQLScanService(db *gorm.DB, log *slog.Logger, indexer *search.Indexer, repo *metaRepo.ScanRepository, indexerService *IndexerService) *NoSQLScanService {
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
	itemTerm := catalogItemTermForPlugin(enginePlugin, plugin.CatalogTermCollection)
	samplingProvider, _ := enginePlugin.(plugin.DocumentMetadataSamplingProvider)

	// 1. 创建/更新 Database 节点
	dbNode, err := s.repo.UpsertNode(tenantID, resource.ID, nil, namespaceTermForPlugin(enginePlugin), databaseName, nil, nil)
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
	collectionItems, err := s.repo.GetItemsByNodeAndType(tenantID, resource.ID, dbNode.ID, itemTerm)
	if err != nil {
		return 0, totalObjects, totalFields, err
	}
	for _, item := range collectionItems {
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

	existingCollectionMap := s.repo.GetItemsByNodeAndTypeMap(tenantID, resource.ID, dbNode.ID, "collection")
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
		needsUpdate := scanchange.ShouldUpdateCollection(existingItem, collInfo)

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
				attrs = metaattr.BuildDocumentCollectionAttributes(itemMetadata)
				totalFields += len(itemMetadata.Fields)
			}
		}

		if attrs == nil {
			attrs = models.JSONMap{}
		}
		if itemType == "collection" {
			metaattr.ApplyDocumentCollectionStatistics(attrs, count, sizeBytes)
		} else {
			metaattr.ApplyGraphItemAttributes(attrs, itemType, count, node.Attributes)
		}
		metaattr.ApplyNoSQLDataItemAttributes(attrs, itemType)

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

func noSQLItemType(node plugin.CatalogNode) string {
	if !node.IsItem {
		return ""
	}
	if node.Term != "" {
		return node.Term
	}
	return node.Kind
}

func countStatKey(itemType string) string {
	if itemType == "relationship" {
		return "count"
	}
	return "document_count"
}
