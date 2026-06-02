package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"github.com/addp/meta/internal/scanchange"
	"github.com/addp/meta/internal/search"
	"gorm.io/gorm"
)

// NamespaceItemScanService 扫描 namespace -> catalog leaf，并把 leaf 投影为 Meta item。
// 文档型与图型引擎共享这一层级，但 leaf 事实仍由插件和 catalog model 决定。
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

// ScanNamespace 扫描 namespace 及其所有 catalog leaf。
// CatalogProvider 负责列出真实数据库、集合或 graph leaf；DynamicSchemaSamplingProvider 用于动态 schema 深度推断。
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
	samplingProvider, _ := enginePlugin.(plugin.DynamicSchemaSamplingProvider)
	catalogFactsProvider, _ := enginePlugin.(plugin.CatalogFactsProvider)

	// 1. 创建/更新 namespace 节点
	rootNode, err := ensureCatalogRootNode(s.repo, tenantID, resource, enginePlugin)
	if err != nil {
		return 0, 0, 0, err
	}
	namespaceNode, err := s.repo.UpsertNode(tenantID, resource.ID, rootNode, namespaceTermForPlugin(enginePlugin), namespaceName, nil, nil)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to create namespace node: %w", err)
	}

	if err := s.repo.ResetNodeState(namespaceNode, "running"); err != nil {
		return 0, 0, 0, err
	}

	var totalObjects, totalFields int

	totalObjects, totalFields, err = s.scanCatalogLeaves(ctx, enginePlugin, catalogProvider, catalogFactsProvider, samplingProvider, connInfo, resource, tenantID, namespaceNode, namespaceName, scanDepth, force)

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

func (s *NamespaceItemScanService) scanCatalogLeaves(
	ctx context.Context,
	enginePlugin plugin.EnginePlugin,
	catalogProvider plugin.CatalogProvider,
	catalogFactsProvider plugin.CatalogFactsProvider,
	samplingProvider plugin.DynamicSchemaSamplingProvider,
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
			Term: namespaceRootTermForPlugin(enginePlugin),
			Kind: namespaceRootTermForPlugin(enginePlugin),
		}, {
			Term: namespaceTermForPlugin(enginePlugin),
			Kind: plugin.CatalogKindNamespace,
			Name: namespaceName,
		}},
	}, plugin.ListOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list catalog leaves: %w", err)
	}

	s.log.Info("扫描到的 namespace catalog leaf", "namespace", namespaceName, "leaf_count", len(nodes))

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
		itemType := namespaceLeafItemType(node)
		if itemType == "" {
			continue
		}
		count := catalogEntryRowCount(node, itemType)
		sizeBytes := catalogEntrySizeBytes(node)
		itemName := node.Name

		s.log.Info(fmt.Sprintf("处理第 %d/%d 个 namespace catalog leaf", i+1, len(nodes)),
			"item_name", itemName,
			"item_type", itemType,
			"count", count,
		)

		if scannedByType[itemType] == nil {
			scannedByType[itemType] = map[string]bool{}
		}
		scannedByType[itemType][itemName] = true

		existingItem := existingItemMap[itemType+"\x00"+itemName]
		needsUpdate := force || scanchange.ShouldUpdateDynamicSchemaItem(existingItem, count, sizeBytes) || existingItem == nil
		if strings.EqualFold(scanDepth, "deep") && existingItem != nil && existingItem.ScannedDepth != models.ScannedDepthDeep {
			needsUpdate = true
		}

		if existingItem != nil && !needsUpdate {
			totalItems++
			continue
		}

		var attrs models.JSONMap

		if itemType == "collection" && strings.EqualFold(scanDepth, "deep") && samplingProvider != nil {
			catalogFacts, err := samplingProvider.SampleDynamicSchema(ctx, connInfo, itemCatalogPath(resource.ID, namespaceTermForPlugin(enginePlugin), namespaceName, node.Term, node.Kind, itemName), plugin.CatalogFactsOptions{
				IncludeSamples:    true,
				IncludeStatistics: true,
				IncludeIndexes:    true,
				SampleSize:        100,
			})
			if err != nil {
				s.log.Warn("动态 schema 采样失败", "namespace", namespaceName, "collection", itemName, "error", err)
			} else {
				attrs = metaattr.BuildDynamicSchemaAttributes(dynamicSchemaAttributesInput(catalogFacts))
				if tableInfo := plugin.CatalogFactsTableInfo(catalogFacts); tableInfo != nil {
					totalFields += len(tableInfo.Fields)
				}
			}
		}
		var graphInfo *datatype.GraphInfo
		if itemType == "graph" {
			if catalogFactsProvider == nil {
				s.log.Warn("图 catalog leaf 缺少 facts provider", "namespace", namespaceName, "leaf", itemName)
				continue
			}
			catalogFacts, err := catalogFactsProvider.DescribeCatalogFacts(ctx, connInfo, node.Path, plugin.CatalogFactsOptions{
				IncludeStatistics: true,
				IncludeSamples:    strings.EqualFold(scanDepth, "deep"),
			})
			if err != nil {
				s.log.Warn("图结构扫描失败", "namespace", namespaceName, "leaf", itemName, "error", err)
				continue
			}
			graphInfo = plugin.CatalogFactsGraphInfo(catalogFacts)
			if graphInfo == nil {
				s.log.Warn("图结构扫描未返回 GraphInfo", "namespace", namespaceName, "leaf", itemName)
				continue
			}
			count = derefGraphNodeCount(graphInfo)
			sizeBytes = 0
		}

		if attrs == nil {
			attrs = models.JSONMap{}
		}
		if itemType == "collection" {
			metaattr.ApplyDynamicSchemaStatistics(attrs, count, sizeBytes)
		} else if itemType == "graph" {
			metaattr.ApplyGraphItemAttributes(attrs, graphInfo)
		} else {
			continue
		}
		metaattr.ApplyNamespaceItemAttributes(attrs, itemType)

		fullName := fmt.Sprintf("%s.%s", namespaceName, itemName)
		rowCount := count

		_, err = s.repo.UpsertItemWithDepth(tenantID, resource.ID, namespaceNode, itemType, itemName, fullName, attrs, &rowCount, &sizeBytes, nil, scanDepth)
		if err != nil {
			s.log.Warn("保存 namespace item 元数据失败", "namespace", namespaceName, "item", itemName, "item_type", itemType, "error", err)
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

func dynamicSchemaAttributesInput(catalogFacts *plugin.CatalogFacts) metaattr.DynamicSchemaAttributesInput {
	if catalogFacts == nil {
		return metaattr.DynamicSchemaAttributesInput{}
	}
	indexes := make([]metaattr.IndexAttributesInput, 0, len(catalogFacts.Indexes))
	for _, index := range catalogFacts.Indexes {
		indexes = append(indexes, metaattr.IndexAttributesInput{
			Name:      index.Name,
			Fields:    append([]string(nil), index.Fields...),
			IsUnique:  index.IsUnique,
			IndexType: index.IndexType,
		})
	}
	return metaattr.DynamicSchemaAttributesInput{
		Fields:     dynamicSchemaFields(catalogFacts),
		Indexes:    indexes,
		Attributes: dynamicSchemaAttributes(catalogFacts),
	}
}

func dynamicSchemaAttributes(catalogFacts *plugin.CatalogFacts) map[string]interface{} {
	tableInfo := plugin.CatalogFactsTableInfo(catalogFacts)
	if tableInfo == nil {
		return nil
	}
	attrs := map[string]interface{}{
		"is_sampled": true,
	}
	if tableInfo.Native != nil {
		if v, ok := tableInfo.Native["schema_type"]; ok {
			attrs["schema_type"] = v
		}
		if v, ok := tableInfo.Native["sample_size"]; ok {
			attrs["sample_size"] = v
		}
		if v, ok := tableInfo.Native["index_count"]; ok {
			attrs["index_count"] = v
		}
		if v, ok := tableInfo.Native["avg_record_size"]; ok {
			attrs["avg_record_size"] = v
		}
		if v, ok := tableInfo.Native["database"]; ok {
			attrs["database"] = v
		}
		if v, ok := tableInfo.Native["collection"]; ok {
			attrs["collection"] = v
		}
	}
	if tableInfo.RowCount != nil {
		attrs["total_documents"] = *tableInfo.RowCount
	}
	return attrs
}

func dynamicSchemaFields(catalogFacts *plugin.CatalogFacts) []datatype.FieldInfo {
	tableInfo := plugin.CatalogFactsTableInfo(catalogFacts)
	if tableInfo == nil || len(tableInfo.Fields) == 0 {
		return nil
	}
	return append([]datatype.FieldInfo(nil), tableInfo.Fields...)
}

func itemCatalogPath(engineID uint, namespaceTerm, namespace, itemTerm, itemKind, itemName string) plugin.CatalogPath {
	return plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
		Segments: []plugin.CatalogSegment{
			{Term: plugin.CatalogTermServer, Kind: plugin.CatalogTermServer},
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

func namespaceLeafItemType(node plugin.CatalogEntry) string {
	if node.Role != plugin.CatalogRoleLeaf {
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

func catalogEntryRowCount(node plugin.CatalogEntry, itemType string) int64 {
	if itemType == "collection" && node.Table != nil && node.Table.RowCount != nil {
		return *node.Table.RowCount
	}
	return 0
}

func catalogEntrySizeBytes(node plugin.CatalogEntry) int64 {
	if node.Table != nil && node.Table.SizeBytes != nil {
		return *node.Table.SizeBytes
	}
	if node.Storage != nil && node.Storage.SizeBytes != nil {
		return *node.Storage.SizeBytes
	}
	return 0
}

func derefGraphNodeCount(info *datatype.GraphInfo) int64 {
	if info == nil || info.NodeCount == nil {
		return 0
	}
	return *info.NodeCount
}
