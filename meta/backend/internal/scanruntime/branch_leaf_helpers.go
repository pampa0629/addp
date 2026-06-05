package scanruntime

import (
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/models"
)

// softDeleteMissingItemsByType 按 item_type 软删除不存在的数据项。
func (s *BranchLeafRuntime) softDeleteMissingItemsByType(tenantID, engineID, branchNodeID uint, itemType string, scanned map[string]bool) {
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
