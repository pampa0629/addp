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
