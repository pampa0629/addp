package repository

import (
	"context"
	"fmt"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/format"
	commonjson "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

// DeleteClaimedSingleItems removes only independent components claimed by a persisted item.
// Call in the same transaction as persistence; fullNames is an exact visible claim set.
func (r *ScanRepository) DeleteClaimedSingleItems(item *models.MetaItem, fullNames []string) ([]models.MetaItem, error) {
	if len(fullNames) == 0 {
		return nil, nil
	}
	var candidates []models.MetaItem
	if err := r.db.Unscoped().Where("tenant_id = ? AND engine_id = ? AND item_type = ? AND id <> ?", item.TenantID, item.EngineID, item.ItemType, item.ID).
		Where("full_name IN ?", fullNames).Find(&candidates).Error; err != nil {
		return nil, err
	}
	removed := []models.MetaItem{}
	for _, candidate := range candidates {
		layout := dataitem.DescriptorFromAttributes(candidate.Attributes).Layout
		if layout != "" && layout != format.LayoutSingle {
			continue
		}
		removed = append(removed, candidate)
	}
	// Keep each statement bounded while preserving the caller's transaction and row triggers.
	const deleteBatchSize = 256
	for start := 0; start < len(removed); start += deleteBatchSize {
		end := min(len(removed), start+deleteBatchSize)
		ids := make([]uint, 0, end-start)
		for _, item := range removed[start:end] {
			ids = append(ids, item.ID)
		}
		if err := r.db.Unscoped().Where("tenant_id = ? AND engine_id = ? AND id IN ?", item.TenantID, item.EngineID, ids).Delete(&models.MetaItem{}).Error; err != nil {
			return nil, err
		}
	}
	return removed, nil
}

// ReconcileWholeScope removes obsolete independent resources inside an identified
// whole scope. The caller persists the item in the same transaction. Membership
// follows tenant/engine-bound parent edges, never cached node.path or LIKE names.
func (r *ScanRepository) ReconcileWholeScope(item *models.MetaItem) ([]models.MetaItem, error) {
	if item == nil || commonjson.String(item.Attributes, "item", "layout") != "whole" {
		return nil, fmt.Errorf("whole scope item required")
	}
	var root models.MetaNode
	if err := r.db.Where("id = ? AND tenant_id = ? AND engine_id = ?", item.NodeID, item.TenantID, item.EngineID).First(&root).Error; err != nil {
		return nil, err
	}
	if root.Name != item.Name || root.FullName != item.FullName ||
		!((item.ItemType == "object" && root.NodeType == "prefix") || (item.ItemType == "file" && root.NodeType == "dir")) {
		return nil, fmt.Errorf("whole scope owner mismatch")
	}
	const descendants = `WITH RECURSIVE scope(id) AS (
		SELECT id FROM meta.meta_node WHERE id = ? AND tenant_id = ? AND engine_id = ?
		UNION SELECT n.id FROM meta.meta_node n JOIN scope s ON n.parent_node_id = s.id
		WHERE n.tenant_id = ? AND n.engine_id = ?
	) SELECT id FROM scope`
	args := []interface{}{root.ID, item.TenantID, item.EngineID, item.TenantID, item.EngineID}
	query := "node_id IN (" + descendants + ") AND tenant_id = ? AND engine_id = ? AND id <> ?"
	itemArgs := append(append([]interface{}{}, args...), item.TenantID, item.EngineID, item.ID)
	var removed []models.MetaItem
	if err := r.db.Unscoped().Where(query, itemArgs...).Find(&removed).Error; err != nil {
		return nil, err
	}
	if err := r.db.Unscoped().Where(query, itemArgs...).Delete(&models.MetaItem{}).Error; err != nil {
		return nil, err
	}
	nodeArgs := append(append([]interface{}{}, args...), root.ID)
	if err := r.db.Unscoped().Where("id IN ("+descendants+") AND id <> ?", nodeArgs...).Delete(&models.MetaNode{}).Error; err != nil {
		return nil, err
	}
	return removed, nil
}

// HardDeleteItemsByNode 硬删除节点下的所有数据项
func (r *ScanRepository) HardDeleteItemsByNode(nodeID uint) error {
	return r.db.Unscoped().Where("node_id = ?", nodeID).Delete(&models.MetaItem{}).Error
}

// HardDeleteDescendantNodes 硬删除子孙节点及其下的所有数据项
func (r *ScanRepository) HardDeleteDescendantNodes(node *models.MetaNode) error {
	if node.Path == "" {
		return nil
	}
	prefix := fmt.Sprintf("%s/%%", node.Path)

	// 先找出所有子孙节点 ID
	var descendantIDs []uint
	if err := r.db.Unscoped().Model(&models.MetaNode{}).
		Where("path LIKE ?", prefix).
		Where("id <> ?", node.ID).
		Pluck("id", &descendantIDs).Error; err != nil {
		return err
	}

	if len(descendantIDs) == 0 {
		return nil
	}

	// 级联删除这些节点下的所有 items
	if err := r.db.Unscoped().
		Where("node_id IN ?", descendantIDs).
		Delete(&models.MetaItem{}).Error; err != nil {
		return err
	}

	// 删除子孙节点
	return r.db.Unscoped().
		Where("id IN ?", descendantIDs).
		Delete(&models.MetaNode{}).Error
}

// SoftDeleteItemsByNodeExceptFullNames 标记目录扫描中缺失的数据项，保留血缘身份。
func (r *ScanRepository) SoftDeleteItemsByNodeExceptFullNames(nodeID uint, keepFullNames map[string]bool) error {
	query := r.db.Where("node_id = ?", nodeID)
	if len(keepFullNames) > 0 {
		names := make([]string, 0, len(keepFullNames))
		for name := range keepFullNames {
			names = append(names, name)
		}
		query = query.Where("full_name NOT IN ?", names)
	}
	return query.Delete(&models.MetaItem{}).Error
}

// HardDeleteItemsByNodeFullNames 硬删除当前节点下指定 full_name 的数据项。
func (r *ScanRepository) HardDeleteItemsByNodeFullNames(nodeID uint, fullNames map[string]bool) error {
	if len(fullNames) == 0 {
		return nil
	}
	names := make([]string, 0, len(fullNames))
	for name := range fullNames {
		names = append(names, name)
	}
	return r.db.Unscoped().
		Where("node_id = ?", nodeID).
		Where("full_name IN ?", names).
		Delete(&models.MetaItem{}).Error
}

// SoftDeleteChildNodesExceptFullNames marks missing direct children and their subtrees.
func (r *ScanRepository) SoftDeleteChildNodesExceptFullNames(ctx context.Context, parent *models.MetaNode, keepFullNames map[string]bool) error {
	return r.softDeleteChildSubtrees(ctx, parent, "full_name NOT IN ?", keepFullNames)
}

// SoftDeleteChildNodesByFullNames marks subtrees excluded by the current scan.
func (r *ScanRepository) SoftDeleteChildNodesByFullNames(ctx context.Context, parent *models.MetaNode, fullNames map[string]bool) error {
	if len(fullNames) == 0 {
		return nil
	}
	return r.softDeleteChildSubtrees(ctx, parent, "full_name IN ?", fullNames)
}

func (r *ScanRepository) softDeleteChildSubtrees(ctx context.Context, parent *models.MetaNode, nameFilter string, fullNames map[string]bool) error {
	names := make([]string, 0, len(fullNames))
	for name := range fullNames {
		names = append(names, name)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		roots := tx.Model(&models.MetaNode{}).Select("id").
			Where("tenant_id = ? AND engine_id = ? AND parent_node_id = ?", parent.TenantID, parent.EngineID, parent.ID)
		if len(names) > 0 {
			roots = roots.Where(nameFilter, names)
		}
		const subtree = `WITH RECURSIVE scope(id) AS (
			?
			UNION
			SELECT child.id FROM meta.meta_node child JOIN scope ON child.parent_node_id = scope.id
			WHERE child.tenant_id = ? AND child.engine_id = ?
		) SELECT id FROM scope`
		if err := tx.Where("tenant_id = ? AND engine_id = ?", parent.TenantID, parent.EngineID).
			Where("node_id IN ("+subtree+")", roots, parent.TenantID, parent.EngineID).
			Delete(&models.MetaItem{}).Error; err != nil {
			return err
		}
		return tx.Where("id IN ("+subtree+")", roots, parent.TenantID, parent.EngineID).
			Delete(&models.MetaNode{}).Error
	})
}

// HardDeleteInvalidEngineGraph 硬删除当前引擎下不满足元数据树不变量的记录。
func (r *ScanRepository) HardDeleteInvalidEngineGraph(tenantID, engineID uint) error {
	var invalidNodes []models.MetaNode
	if err := r.db.Raw(`
		SELECT n.*
		FROM meta.meta_node n
		WHERE n.tenant_id = ?
		  AND n.engine_id = ?
		  AND n.deleted_at IS NULL
		  AND n.parent_node_id IS NOT NULL
		  AND NOT EXISTS (
		      SELECT 1
		      FROM meta.meta_node p
		      WHERE p.id = n.parent_node_id
		        AND p.deleted_at IS NULL
		  )
	`, tenantID, engineID).Scan(&invalidNodes).Error; err != nil {
		return err
	}

	var conflictNodes []models.MetaNode
	if err := r.db.Raw(`
		SELECT n.*
		FROM meta.meta_node n
		WHERE n.tenant_id = ?
		  AND n.engine_id = ?
		  AND n.deleted_at IS NULL
		  AND n.full_name <> ''
		  AND EXISTS (
		      SELECT 1
		      FROM meta.meta_item i
		      WHERE i.tenant_id = n.tenant_id
		        AND i.engine_id = n.engine_id
		        AND i.full_name = n.full_name
		        AND i.deleted_at IS NULL
		        AND NOT (i.node_id = n.id AND i.name = n.name
		            AND COALESCE(i.attributes -> 'item' ->> 'layout', '') = 'whole'
		            AND ((i.item_type = 'object' AND n.node_type = 'prefix')
		              OR (i.item_type = 'file' AND n.node_type = 'dir')))
		  )
	`, tenantID, engineID).Scan(&conflictNodes).Error; err != nil {
		return err
	}

	nodesByID := make(map[uint]models.MetaNode, len(invalidNodes)+len(conflictNodes))
	for _, node := range invalidNodes {
		nodesByID[node.ID] = node
	}
	for _, node := range conflictNodes {
		nodesByID[node.ID] = node
	}
	for _, node := range nodesByID {
		if err := r.HardDeleteDescendantNodes(&node); err != nil {
			return err
		}
		if err := r.HardDeleteItemsByNode(node.ID); err != nil {
			return err
		}
		if err := r.db.Unscoped().Delete(&node).Error; err != nil {
			return err
		}
	}

	return r.db.Unscoped().Exec(`
		DELETE FROM meta.meta_item
		WHERE tenant_id = ?
		  AND engine_id = ?
		  AND deleted_at IS NULL
		  AND NOT EXISTS (
		      SELECT 1
		      FROM meta.meta_node n
		      WHERE n.id = meta.meta_item.node_id
		        AND n.deleted_at IS NULL
		  )
	`, tenantID, engineID).Error
}
