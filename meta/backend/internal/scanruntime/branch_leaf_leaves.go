package scanruntime

import (
	"context"
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanchange"
)

func (s *BranchLeafRuntime) scanCatalogLeaves(
	ctx context.Context,
	scanCatalog branchLeafScanCatalog,
	resource *commonModels.Engine,
	tenantID uint,
	branchNode *models.MetaNode,
	branchName string,
	scanDepth string,
	force bool,
) (int, int, error) {
	parentPath := plugin.EngineCatalogBranchPath(scanCatalog.model, resource.ID, scanCatalog.branchTerm, branchName)
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
		itemType := catalogLeafItemType(node)
		if itemType == "" {
			continue
		}
		estimatedCount := catalogEntryEstimatedRowCount(node, itemType)
		sizeBytes := engineCatalogEntrySizeBytes(node)
		itemName := node.Name

		s.log.Info(fmt.Sprintf("处理第 %d/%d 个 catalog branch leaf", i+1, len(nodes)),
			"item_name", itemName,
			"item_type", itemType,
			"estimated_count", estimatedCount,
		)

		if scannedByType[itemType] == nil {
			scannedByType[itemType] = map[string]bool{}
		}
		scannedByType[itemType][itemName] = true

		existingItem := existingItemMap[itemType+"\x00"+itemName]
		needsUpdate := force || scanchange.ShouldUpdateDynamicSchemaItem(existingItem, estimatedCount, sizeBytes) || existingItem == nil
		if strings.EqualFold(scanDepth, "deep") && existingItem != nil && existingItem.ScannedDepth != models.ScannedDepthDeep {
			needsUpdate = true
		}

		if existingItem != nil && !needsUpdate {
			totalItems++
			continue
		}

		var attrs models.JSONMap
		var rowCount *int64
		estimatedRowCount := estimatedCount

		if itemType == "collection" && strings.EqualFold(scanDepth, "deep") && scanCatalog.samplingProvider != nil {
			itemPath := plugin.EngineCatalogBranchLeafPath(scanCatalog.model, resource.ID, scanCatalog.branchTerm, branchName, node.Term, node.Kind, itemName)
			catalogFacts, err := scanCatalog.samplingProvider.SampleDynamicSchema(ctx, scanCatalog.connInfo, itemPath, plugin.EngineCatalogFactsOptions{
				IncludeSamples:    true,
				IncludeStatistics: true,
				IncludeIndexes:    true,
				SampleSize:        100,
			})
			if err != nil {
				s.log.Warn("动态 schema 采样失败", "branch", branchName, "collection", itemName, "error", err)
			} else {
				attrs = metaattr.BuildDynamicSchemaAttributes(dynamicSchemaAttributesInput(catalogFacts))
				if tableInfo := plugin.EngineCatalogFactsTableInfo(catalogFacts); tableInfo != nil {
					totalFields += len(tableInfo.Fields)
					rowCount = tableInfo.RowCount
					if tableInfo.EstimatedRowCount != nil {
						estimatedRowCount = tableInfo.EstimatedRowCount
					}
				}
			}
		}
		var graphInfo *datatype.GraphInfo
		if itemType == "graph" {
			if scanCatalog.factsProvider == nil {
				s.log.Warn("图 catalog leaf 缺少 facts provider", "branch", branchName, "leaf", itemName)
				continue
			}
			catalogFacts, err := scanCatalog.factsProvider.DescribeEngineCatalogFacts(ctx, scanCatalog.connInfo, node.Path, plugin.EngineCatalogFactsOptions{
				IncludeStatistics: true,
				IncludeSamples:    strings.EqualFold(scanDepth, "deep"),
			})
			if err != nil {
				s.log.Warn("图结构扫描失败", "branch", branchName, "leaf", itemName, "error", err)
				continue
			}
			graphInfo = plugin.EngineCatalogFactsGraphInfo(catalogFacts)
			if graphInfo == nil {
				s.log.Warn("图结构扫描未返回 GraphInfo", "branch", branchName, "leaf", itemName)
				continue
			}
			count := derefGraphNodeCount(graphInfo)
			rowCount = &count
			sizeBytes = 0
		}

		if attrs == nil {
			attrs = models.JSONMap{}
		}
		if itemType == "collection" {
			metaattr.ApplyDynamicSchemaStatistics(attrs, rowCount, estimatedRowCount, sizeBytes)
		} else if itemType == "graph" {
			metaattr.ApplyGraphItemAttributes(attrs, graphInfo)
		} else {
			continue
		}
		metaattr.ApplyBranchLeafItemAttributes(attrs, itemType)

		fullName := fmt.Sprintf("%s.%s", branchName, itemName)
		if rowCount == nil && existingItem != nil {
			rowCount = existingItem.RowCount
		}

		_, err = s.repo.UpsertItemWithDepth(tenantID, resource.ID, branchNode, itemType, itemName, fullName, attrs, rowCount, &sizeBytes, nil, scanDepth)
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
