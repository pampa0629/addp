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

// BranchLeafScanService 扫描 root branch -> catalog leaf，并把 leaf 投影为 Meta item。
// 动态 schema 与图型引擎共享这一层级，但 leaf 事实仍由插件和 catalog model 决定。
type BranchLeafScanService struct {
	db             *gorm.DB
	log            *slog.Logger
	indexer        *search.Indexer
	repo           *metaRepo.ScanRepository // 数据访问层
	indexerService *IndexerService          // 索引服务
}

type branchLeafScanCatalog struct {
	model            plugin.CatalogModelSpec
	catalogProvider  plugin.CatalogProvider
	factsProvider    plugin.CatalogFactsProvider
	samplingProvider plugin.DynamicSchemaSamplingProvider
	connInfo         plugin.ConnectionInfo
	branchTerm       string
}

func NewBranchLeafScanService(db *gorm.DB, log *slog.Logger, indexer *search.Indexer, repo *metaRepo.ScanRepository, indexerService *IndexerService) *BranchLeafScanService {
	return &BranchLeafScanService{
		db:             db,
		log:            log,
		indexer:        indexer,
		repo:           repo,
		indexerService: indexerService,
	}
}

// ScanBranch 扫描 root branch 及其所有 catalog leaf。
// CatalogProvider 负责列出真实数据库、集合或 graph leaf；DynamicSchemaSamplingProvider 用于动态 schema 深度推断。
func (s *BranchLeafScanService) ScanBranch(
	ctx context.Context,
	enginePlugin plugin.EnginePlugin,
	resource *commonModels.Engine,
	tenantID uint,
	branchName string,
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
	model := catalogModelForPlugin(enginePlugin)
	if model == nil {
		return 0, 0, 0, fmt.Errorf("engine %s has no catalog model", resource.EngineType)
	}
	scanCatalog := branchLeafScanCatalog{
		model:            *model,
		catalogProvider:  catalogProvider,
		factsProvider:    catalogFactsProvider,
		samplingProvider: samplingProvider,
		connInfo:         connInfo,
		branchTerm:       firstBusinessBranchTermForPlugin(enginePlugin),
	}

	// 1. 创建/更新 root branch 节点
	rootNode, err := ensureCatalogRootNode(s.repo, tenantID, resource, enginePlugin)
	if err != nil {
		return 0, 0, 0, err
	}
	branchNode, err := s.repo.UpsertNode(tenantID, resource.ID, rootNode, scanCatalog.branchTerm, branchName, nil, nil)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to create catalog branch node: %w", err)
	}

	if err := s.repo.ResetNodeState(branchNode, "running"); err != nil {
		return 0, 0, 0, err
	}

	var totalObjects, totalFields int

	totalObjects, totalFields, err = s.scanCatalogLeaves(ctx, scanCatalog, resource, tenantID, branchNode, branchName, scanDepth, force)

	if err != nil {
		s.repo.FinalizeNodeState(branchNode, "pending", 0, 0, err.Error())
		return 0, 0, 0, err
	}

	// 3. 完成扫描
	var totalSize int64
	collectionItems, err := s.repo.GetItemsByNode(branchNode.ID)
	if err != nil {
		return 0, totalObjects, totalFields, err
	}
	for _, item := range collectionItems {
		if item.SizeBytes != nil {
			totalSize += *item.SizeBytes
		}
	}

	if err := s.repo.FinalizeNodeStateWithDepth(branchNode, "completed", totalObjects, totalSize, "", scanDepth); err != nil {
		return 0, totalObjects, totalFields, err
	}

	return 1, totalObjects, totalFields, nil
}

func (s *BranchLeafScanService) scanCatalogLeaves(
	ctx context.Context,
	scanCatalog branchLeafScanCatalog,
	resource *commonModels.Engine,
	tenantID uint,
	branchNode *models.MetaNode,
	branchName string,
	scanDepth string,
	force bool,
) (int, int, error) {
	parentPath := plugin.BranchCatalogPath(scanCatalog.model, resource.ID, scanCatalog.branchTerm, branchName)
	nodes, err := scanCatalog.catalogProvider.ListChildren(ctx, scanCatalog.connInfo, parentPath, plugin.ListOptions{})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list catalog leaves: %w", err)
	}

	s.log.Info("扫描到的 catalog branch leaf", "branch", branchName, "leaf_count", len(nodes))

	existingItems, err := s.repo.GetItemsByNode(branchNode.ID)
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
		itemType := branchLeafItemType(node)
		if itemType == "" {
			continue
		}
		count := catalogEntryRowCount(node, itemType)
		sizeBytes := catalogEntrySizeBytes(node)
		itemName := node.Name

		s.log.Info(fmt.Sprintf("处理第 %d/%d 个 catalog branch leaf", i+1, len(nodes)),
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

		if itemType == "collection" && strings.EqualFold(scanDepth, "deep") && scanCatalog.samplingProvider != nil {
			itemPath := plugin.BranchLeafCatalogPath(scanCatalog.model, resource.ID, scanCatalog.branchTerm, branchName, node.Term, node.Kind, itemName)
			catalogFacts, err := scanCatalog.samplingProvider.SampleDynamicSchema(ctx, scanCatalog.connInfo, itemPath, plugin.CatalogFactsOptions{
				IncludeSamples:    true,
				IncludeStatistics: true,
				IncludeIndexes:    true,
				SampleSize:        100,
			})
			if err != nil {
				s.log.Warn("动态 schema 采样失败", "branch", branchName, "collection", itemName, "error", err)
			} else {
				attrs = metaattr.BuildDynamicSchemaAttributes(dynamicSchemaAttributesInput(catalogFacts))
				if tableInfo := plugin.CatalogFactsTableInfo(catalogFacts); tableInfo != nil {
					totalFields += len(tableInfo.Fields)
				}
			}
		}
		var graphInfo *datatype.GraphInfo
		if itemType == "graph" {
			if scanCatalog.factsProvider == nil {
				s.log.Warn("图 catalog leaf 缺少 facts provider", "branch", branchName, "leaf", itemName)
				continue
			}
			catalogFacts, err := scanCatalog.factsProvider.DescribeCatalogFacts(ctx, scanCatalog.connInfo, node.Path, plugin.CatalogFactsOptions{
				IncludeStatistics: true,
				IncludeSamples:    strings.EqualFold(scanDepth, "deep"),
			})
			if err != nil {
				s.log.Warn("图结构扫描失败", "branch", branchName, "leaf", itemName, "error", err)
				continue
			}
			graphInfo = plugin.CatalogFactsGraphInfo(catalogFacts)
			if graphInfo == nil {
				s.log.Warn("图结构扫描未返回 GraphInfo", "branch", branchName, "leaf", itemName)
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
		metaattr.ApplyBranchLeafItemAttributes(attrs, itemType)

		fullName := fmt.Sprintf("%s.%s", branchName, itemName)
		rowCount := count

		_, err = s.repo.UpsertItemWithDepth(tenantID, resource.ID, branchNode, itemType, itemName, fullName, attrs, &rowCount, &sizeBytes, nil, scanDepth)
		if err != nil {
			s.log.Warn("保存 branch leaf 元数据失败", "branch", branchName, "item", itemName, "item_type", itemType, "error", err)
			continue
		}

		totalItems++
	}

	for itemType, scanned := range scannedByType {
		if len(scanned) == 0 {
			continue
		}
		s.softDeleteMissingItemsByType(tenantID, resource.ID, branchNode.ID, itemType, scanned)
	}
	for itemType := range itemTypes(existingItems) {
		if _, ok := scannedByType[itemType]; ok {
			continue
		}
		s.softDeleteMissingItemsByType(tenantID, resource.ID, branchNode.ID, itemType, map[string]bool{})
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

// softDeleteMissingItemsByType 按 item_type 软删除不存在的数据项
func (s *BranchLeafScanService) softDeleteMissingItemsByType(tenantID, engineID, branchNodeID uint, itemType string, scanned map[string]bool) {
	var items []models.MetaItem
	s.db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND deleted_at IS NULL",
		tenantID, engineID, branchNodeID, itemType).Find(&items)

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

func branchLeafItemType(node plugin.CatalogEntry) string {
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
