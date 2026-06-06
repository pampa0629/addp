package repository

import (
	"fmt"

	"github.com/addp/meta/internal/models"
)

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

// HardDeleteItemsByNodeExceptFullNames 硬删除节点下未出现在本轮扫描结果中的数据项。
func (r *ScanRepository) HardDeleteItemsByNodeExceptFullNames(nodeID uint, keepFullNames map[string]bool) error {
	query := r.db.Unscoped().Where("node_id = ?", nodeID)
	if len(keepFullNames) > 0 {
		names := make([]string, 0, len(keepFullNames))
		for name := range keepFullNames {
			names = append(names, name)
		}
		query = query.Where("full_name NOT IN ?", names)
	}
	return query.Delete(&models.MetaItem{}).Error
}

// HardDeleteChildNodesExceptFullNames 硬删除未出现在本轮扫描结果中的直接子节点及其子树。
func (r *ScanRepository) HardDeleteChildNodesExceptFullNames(parentNodeID uint, keepFullNames map[string]bool) error {
	var staleNodes []models.MetaNode
	query := r.db.Unscoped().Where("parent_node_id = ?", parentNodeID)
	if len(keepFullNames) > 0 {
		names := make([]string, 0, len(keepFullNames))
		for name := range keepFullNames {
			names = append(names, name)
		}
		query = query.Where("full_name NOT IN ?", names)
	}
	if err := query.Find(&staleNodes).Error; err != nil {
		return err
	}
	for i := range staleNodes {
		if err := r.HardDeleteDescendantNodes(&staleNodes[i]); err != nil {
			return err
		}
		if err := r.HardDeleteItemsByNode(staleNodes[i].ID); err != nil {
			return err
		}
		if err := r.db.Unscoped().Delete(&staleNodes[i]).Error; err != nil {
			return err
		}
	}
	return nil
}

// HardDeleteChildNodesByFullNames 硬删除与文件叶子路径冲突的直接子节点及其子树。
func (r *ScanRepository) HardDeleteChildNodesByFullNames(parentNodeID uint, fullNames map[string]bool) error {
	if len(fullNames) == 0 {
		return nil
	}
	names := make([]string, 0, len(fullNames))
	for name := range fullNames {
		names = append(names, name)
	}

	var staleNodes []models.MetaNode
	if err := r.db.Unscoped().
		Where("parent_node_id = ?", parentNodeID).
		Where("full_name IN ?", names).
		Find(&staleNodes).Error; err != nil {
		return err
	}
	for i := range staleNodes {
		if err := r.HardDeleteDescendantNodes(&staleNodes[i]); err != nil {
			return err
		}
		if err := r.HardDeleteItemsByNode(staleNodes[i].ID); err != nil {
			return err
		}
		if err := r.db.Unscoped().Delete(&staleNodes[i]).Error; err != nil {
			return err
		}
	}
	return nil
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
