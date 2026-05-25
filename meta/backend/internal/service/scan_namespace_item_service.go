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

// NamespaceItemScanService 扫描 namespace -> item 型 catalog。
// 文档型与图型引擎共享这一层级，但 item 事实仍由插件和 catalog model 决定。
type NamespaceItemScanService struct {
	db             *gorm.DB
	log            *slog.Logger
	indexer        *search.Indexer
	repo           *metaRepo.ScanRepository // 数据访问层
	indexerService *IndexerService          // 索引服务
}

func NewNamespaceItemScanService(db *gorm.DB, log *slog.Logger, indexer *search.Indexer, repo *metaRepo.ScanRepository, indexerService *IndexerService) *NamespaceItemScanService {
	return &NamespaceItemScanService{
		db:             db,
		log:            log,
		indexer:        indexer,
		repo:           repo,
		indexerService: indexerService,
	}
}

// ScanNamespace 扫描 namespace 及其所有 item。
// CatalogProvider 负责列出真实数据库、集合、标签和关系；DocumentMetadataSamplingProvider 用于文档 schema 深度推断。
func (s *NamespaceItemScanService) ScanNamespace(
	ctx context.Context,
	enginePlugin plugin.EnginePlugin,
	resource *commonModels.Engine,
	tenantID uint,
	namespaceName string,
	scanDepth string,
	force bool,
) (int, int, int, error) {

	connInfo := plugin.ConnectionInfo(resource.ConnectionInfo)
	catalogProvider, ok := enginePlugin.(plugin.CatalogProvider)
	if !ok {
		return 0, 0, 0, fmt.Errorf("engine %s does not implement CatalogProvider", resource.EngineType)
	}
	samplingProvider, _ := enginePlugin.(plugin.DocumentMetadataSamplingProvider)

	// 1. 创建/更新 namespace 节点
	namespaceNode, err := s.repo.UpsertNode(tenantID, resource.ID, nil, namespaceTermForPlugin(enginePlugin), namespaceName, nil, nil)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to create namespace node: %w", err)
	}

	if err := s.repo.ResetNodeState(namespaceNode, "running"); err != nil {
		return 0, 0, 0, err
	}

	var totalObjects, totalFields int

	totalObjects, totalFields, err = s.scanCatalogItems(ctx, enginePlugin, catalogProvider, samplingProvider, connInfo, resource, tenantID, namespaceNode, namespaceName, scanDepth, force)

	if err != nil {
		s.repo.FinalizeNodeState(namespaceNode, "pending", 0, 0, err.Error())
		return 0, 0, 0, err
	}

	// 3. 完成扫描
	var totalSize int64
	collectionItems, err := s.repo.GetItemsByNode(namespaceNode.ID)
	if err != nil {
		return 0, totalObjects, totalFields, err
	}
	for _, item := range collectionItems {
		if item.SizeBytes != nil {
			totalSize += *item.SizeBytes
		}
	}

	if err := s.repo.FinalizeNodeStateWithDepth(namespaceNode, "completed", totalObjects, totalSize, "", scanDepth); err != nil {
		return 0, totalObjects, totalFields, err
	}

	return 1, totalObjects, totalFields, nil
}

func (s *NamespaceItemScanService) scanCatalogItems(
	ctx context.Context,
	enginePlugin plugin.EnginePlugin,
	catalogProvider plugin.CatalogProvider,
	samplingProvider plugin.DocumentMetadataSamplingProvider,
	connInfo plugin.ConnectionInfo,
	resource *commonModels.Engine,
	tenantID uint,
	namespaceNode *models.MetaNode,
	namespaceName string,
	scanDepth string,
	force bool,
) (int, int, error) {
	nodes, err := catalogProvider.ListChildren(ctx, connInfo, plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: resource.ID,
		Segments: []plugin.CatalogSegment{{
			Term: namespaceTermForPlugin(enginePlugin),
			Kind: plugin.CatalogKindNamespace,
			Name: namespaceName,
		}},
	}, plugin.ListOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list catalog items: %w", err)
	}

	s.log.Info("扫描到的 namespace catalog item", "namespace", namespaceName, "item_count", len(nodes))

	existingItems, err := s.repo.GetItemsByNode(namespaceNode.ID)
	if err != nil {
		return 0, 0, err
	}
	existingItemMap := make(map[string]*models.MetaItem, len(existingItems))
	scannedByType := map[string]map[string]bool{}
	for _, item := range existingItems {
		existingItemMap[item.ItemType+"\x00"+item.Name] = item
	}
	totalItems := 0
	totalFields := 0

	for i, node := range nodes {
		itemType := namespaceItemType(node)
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

		s.log.Info(fmt.Sprintf("处理第 %d/%d 个 namespace catalog item", i+1, len(nodes)),
			"item_name", node.Name,
			"item_type", itemType,
			"count", count,
		)

		if scannedByType[itemType] == nil {
			scannedByType[itemType] = map[string]bool{}
		}
		scannedByType[itemType][node.Name] = true

		existingItem := existingItemMap[itemType+"\x00"+collInfo.Name]
		needsUpdate := force || scanchange.ShouldUpdateCollection(existingItem, collInfo) || existingItem == nil
		if strings.EqualFold(scanDepth, "deep") && existingItem != nil && existingItem.ScannedDepth != models.ScannedDepthDeep {
			needsUpdate = true
		}

		if existingItem != nil && !needsUpdate {
			totalItems++
			continue
		}

		var attrs models.JSONMap

		if itemType == "collection" && strings.EqualFold(scanDepth, "deep") && samplingProvider != nil {
			itemMetadata, err := samplingProvider.SampleDocumentMetadata(ctx, connInfo, itemCatalogPath(resource.ID, namespaceTermForPlugin(enginePlugin), namespaceName, node.Term, node.Kind, collInfo.Name), plugin.MetadataOptions{
				IncludeSamples:    true,
				IncludeStatistics: true,
				IncludeIndexes:    true,
				SampleSize:        100,
			})
			if err != nil {
				s.log.Warn("文档集合 Schema 采样失败", "namespace", namespaceName, "collection", collInfo.Name, "error", err)
			} else {
				attrs = metaattr.BuildDocumentCollectionAttributes(documentCollectionAttributesInput(itemMetadata))
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
		metaattr.ApplyNamespaceItemAttributes(attrs, itemType)

		fullName := fmt.Sprintf("%s.%s", namespaceName, collInfo.Name)
		rowCount := count

		_, err = s.repo.UpsertItemWithDepth(tenantID, resource.ID, namespaceNode, itemType, collInfo.Name, fullName, attrs, &rowCount, &sizeBytes, nil, scanDepth)
		if err != nil {
			s.log.Warn("保存 namespace item 元数据失败", "namespace", namespaceName, "item", collInfo.Name, "item_type", itemType, "error", err)
			continue
		}

		totalItems++
	}

	for itemType, scanned := range scannedByType {
		if len(scanned) == 0 {
			continue
		}
		s.softDeleteMissingItemsByType(tenantID, resource.ID, namespaceNode.ID, itemType, scanned)
	}
	for itemType := range itemTypes(existingItems) {
		if _, ok := scannedByType[itemType]; ok {
			continue
		}
		s.softDeleteMissingItemsByType(tenantID, resource.ID, namespaceNode.ID, itemType, map[string]bool{})
	}
	return totalItems, totalFields, nil
}

func documentCollectionAttributesInput(itemMetadata *plugin.ItemMetadata) metaattr.DocumentCollectionAttributesInput {
	if itemMetadata == nil {
		return metaattr.DocumentCollectionAttributesInput{}
	}
	indexes := make([]metaattr.IndexAttributesInput, 0, len(itemMetadata.Indexes))
	for _, index := range itemMetadata.Indexes {
		indexes = append(indexes, metaattr.IndexAttributesInput{
			Name:      index.Name,
			Fields:    append([]string(nil), index.Fields...),
			IsUnique:  index.IsUnique,
			IndexType: index.IndexType,
		})
	}
	return metaattr.DocumentCollectionAttributesInput{
		Fields:     itemMetadata.Fields,
		Indexes:    indexes,
		Stats:      itemMetadata.Stats,
		Attributes: itemMetadata.Attributes,
	}
}

func itemCatalogPath(engineID uint, namespaceTerm, namespace, itemTerm, itemKind, itemName string) plugin.CatalogPath {
	return plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
		Segments: []plugin.CatalogSegment{
			{Term: namespaceTerm, Kind: plugin.CatalogKindNamespace, Name: namespace},
			{Term: itemTerm, Kind: itemKind, Name: itemName},
		},
	}
}

// softDeleteMissingItemsByType 按 item_type 软删除不存在的数据项
func (s *NamespaceItemScanService) softDeleteMissingItemsByType(tenantID, engineID, namespaceNodeID uint, itemType string, scanned map[string]bool) {
	var items []models.MetaItem
	s.db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND deleted_at IS NULL",
		tenantID, engineID, namespaceNodeID, itemType).Find(&items)

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

func namespaceItemType(node plugin.CatalogNode) string {
	if !node.IsItem {
		return ""
	}
	if node.Term != "" {
		return node.Term
	}
	return node.Kind
}

func itemTypes(items []*models.MetaItem) map[string]struct{} {
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		result[item.ItemType] = struct{}{}
	}
	return result
}

func countStatKey(itemType string) string {
	if itemType == "relationship" {
		return "count"
	}
	return "document_count"
}
