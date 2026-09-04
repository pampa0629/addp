package scanruntime

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/scanflow"
)

// softDeleteMissingItemsByType 按 item_type 软删除不存在的数据项。
func (s *BranchLeafRuntime) softDeleteMissingItemsByType(tenantID, engineID, branchNodeID uint, itemType string, scanned map[string]bool) error {
	var items []models.MetaItem
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND node_id = ? AND item_type = ? AND deleted_at IS NULL",
		tenantID, engineID, branchNodeID, itemType).Find(&items).Error; err != nil {
		return err
	}
	failures := &scanflow.FailedTargetCollector{}

	for _, item := range items {
		if !scanned[item.Name] {
			s.log.Info("软删除不存在的数据项",
				"tenant_id", tenantID,
				"engine_id", engineID,
				"item_type", itemType,
				"name", item.Name,
			)
			if err := s.db.Delete(&item).Error; err != nil {
				failures.Add(item.FullName, err)
			}
		}
	}
	return failures.Err()
}

func catalogLeafItemType(node plugin.EngineCatalogEntry) string {
	if node.Role != plugin.EngineCatalogRoleLeaf {
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

func catalogEntryEstimatedRowCount(node plugin.EngineCatalogEntry, itemType string) *int64 {
	if itemType == "collection" && node.Table != nil && node.Table.EstimatedRowCount != nil {
		estimatedRowCount := *node.Table.EstimatedRowCount
		return &estimatedRowCount
	}
	return nil
}

func engineCatalogEntrySizeBytes(node plugin.EngineCatalogEntry) int64 {
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
