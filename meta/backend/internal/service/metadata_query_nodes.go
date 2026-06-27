package service

import (
	"fmt"

	"github.com/addp/meta/internal/metaquery"
	"github.com/addp/meta/internal/models"
)

func (s *MetadataQueryService) GetMetadataTree(tenantID, engineID uint) (*models.MetadataTreeResponse, error) {
	var topNodes []models.MetaNode
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id IS NULL AND full_name = '' AND deleted_at IS NULL", tenantID, engineID).
		Find(&topNodes).Error; err != nil {
		return nil, fmt.Errorf("failed to query top nodes: %w", err)
	}

	var childNodes []models.MetaNode
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id IS NOT NULL AND deleted_at IS NULL", tenantID, engineID).
		Find(&childNodes).Error; err != nil {
		return nil, fmt.Errorf("failed to query child nodes: %w", err)
	}

	var items []models.MetaItem
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND deleted_at IS NULL AND node_id IN (SELECT id FROM meta.meta_node WHERE tenant_id = ? AND engine_id = ? AND deleted_at IS NULL)",
		tenantID, engineID, tenantID, engineID).
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("failed to query items: %w", err)
	}

	topNodesLite := make([]models.MetaNodeLite, len(topNodes))
	for i, node := range topNodes {
		topNodesLite[i] = metaquery.ToMetaNodeLite(node)
	}
	if err := s.populateMetaNodeHasChildren(tenantID, topNodesLite); err != nil {
		return nil, err
	}

	childNodesLite := make([]models.MetaNodeLite, len(childNodes))
	for i, node := range childNodes {
		childNodesLite[i] = metaquery.ToMetaNodeLite(node)
	}
	if err := s.populateMetaNodeHasChildren(tenantID, childNodesLite); err != nil {
		return nil, err
	}

	itemsLite := make([]models.MetaItemLite, len(items))
	for i, item := range items {
		itemsLite[i] = metaquery.ToMetaItemLite(item)
	}

	return &models.MetadataTreeResponse{
		TopNodes:   topNodesLite,
		ChildNodes: childNodesLite,
		Items:      itemsLite,
	}, nil
}

func (s *MetadataQueryService) GetNodeByCatalogPath(tenantID, engineID uint, catalogPath string) (*models.MetaNodeLite, error) {
	var node models.MetaNode

	for _, candidate := range catalogPathCandidates(catalogPath) {
		err := s.db.Where("tenant_id = ? AND engine_id = ? AND full_name = ? AND deleted_at IS NULL", tenantID, engineID, candidate).
			First(&node).Error
		if err == nil {
			result := metaquery.ToMetaNodeLite(node)
			if err := s.populateMetaNodeHasChildren(tenantID, []models.MetaNodeLite{result}); err != nil {
				return nil, err
			}
			return &result, nil
		}
	}

	trimmed := normalizeCatalogPath(catalogPath)
	if trimmed != "" {
		err := s.db.Where("tenant_id = ? AND engine_id = ? AND name = ? AND parent_node_id IS NULL AND deleted_at IS NULL", tenantID, engineID, trimmed).
			First(&node).Error
		if err == nil {
			result := metaquery.ToMetaNodeLite(node)
			if err := s.populateMetaNodeHasChildren(tenantID, []models.MetaNodeLite{result}); err != nil {
				return nil, err
			}
			return &result, nil
		}
	}

	return nil, fmt.Errorf("node not found")
}

func (s *MetadataQueryService) GetNodeChildren(tenantID, nodeID uint) ([]models.MetaNodeLite, error) {
	var nodes []models.MetaNode

	var parentNode models.MetaNode
	if err := s.db.Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, nodeID).First(&parentNode).Error; err != nil {
		return nil, fmt.Errorf("parent node not found: %w", err)
	}

	if err := s.db.Where("tenant_id = ? AND parent_node_id = ? AND deleted_at IS NULL", tenantID, nodeID).
		Order("name").
		Find(&nodes).Error; err != nil {
		return nil, fmt.Errorf("failed to query child nodes: %w", err)
	}

	result := make([]models.MetaNodeLite, len(nodes))
	for i, node := range nodes {
		result[i] = metaquery.ToMetaNodeLite(node)
	}
	if err := s.populateMetaNodeHasChildren(tenantID, result); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *MetadataQueryService) GetMetaNodeByID(tenantID, nodeID uint) (*models.MetaNodeLite, error) {
	var node models.MetaNode

	if err := s.db.Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, nodeID).First(&node).Error; err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	result := metaquery.ToMetaNodeLite(node)
	if err := s.populateMetaNodeHasChildren(tenantID, []models.MetaNodeLite{result}); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *MetadataQueryService) GetNodeAncestors(tenantID, nodeID uint) ([]models.MetaNodeLite, error) {
	var target models.MetaNode
	if err := s.db.Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, nodeID).First(&target).Error; err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	nodes, err := s.nodeAncestorChain(tenantID, target)
	if err != nil {
		return nil, err
	}
	result := make([]models.MetaNodeLite, len(nodes))
	for i, node := range nodes {
		result[i] = metaquery.ToMetaNodeLite(node)
	}
	if err := s.populateMetaNodeHasChildren(tenantID, result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *MetadataQueryService) populateMetaNodeHasChildren(tenantID uint, nodes []models.MetaNodeLite) error {
	if len(nodes) == 0 {
		return nil
	}
	nodeIDs := make([]uint, 0, len(nodes))
	indexByID := make(map[uint]int, len(nodes))
	for i, node := range nodes {
		if node.ID == 0 {
			continue
		}
		nodeIDs = append(nodeIDs, node.ID)
		indexByID[node.ID] = i
		nodes[i].HasChildren = nodes[i].HasChildren || node.ItemCount > 0
	}
	if len(nodeIDs) == 0 {
		return nil
	}

	var childNodeRefs []struct {
		ParentNodeID uint
	}
	if err := s.db.Model(&models.MetaNode{}).
		Select("parent_node_id").
		Where("tenant_id = ? AND parent_node_id IN ? AND deleted_at IS NULL", tenantID, nodeIDs).
		Group("parent_node_id").
		Find(&childNodeRefs).Error; err != nil {
		return fmt.Errorf("failed to query child node refs: %w", err)
	}
	for _, ref := range childNodeRefs {
		if idx, ok := indexByID[ref.ParentNodeID]; ok {
			nodes[idx].HasChildren = true
		}
	}

	var itemRefs []struct {
		NodeID uint
	}
	if err := s.db.Model(&models.MetaItem{}).
		Select("node_id").
		Where("tenant_id = ? AND node_id IN ? AND deleted_at IS NULL", tenantID, nodeIDs).
		Group("node_id").
		Find(&itemRefs).Error; err != nil {
		return fmt.Errorf("failed to query child item refs: %w", err)
	}
	for _, ref := range itemRefs {
		if idx, ok := indexByID[ref.NodeID]; ok {
			nodes[idx].HasChildren = true
		}
	}
	return nil
}

func (s *MetadataQueryService) nodeAncestorChain(tenantID uint, target models.MetaNode) ([]models.MetaNode, error) {
	reversed := []models.MetaNode{target}
	seen := map[uint]bool{target.ID: true}
	current := target

	for current.ParentNodeID != nil {
		parentID := *current.ParentNodeID
		if seen[parentID] {
			return nil, fmt.Errorf("node ancestor cycle detected at node %d", parentID)
		}

		var parent models.MetaNode
		if err := s.db.Where("tenant_id = ? AND id = ? AND engine_id = ? AND deleted_at IS NULL", tenantID, parentID, target.EngineID).First(&parent).Error; err != nil {
			return nil, fmt.Errorf("node ancestor missing for node %d: %w", current.ID, err)
		}
		reversed = append(reversed, parent)
		seen[parent.ID] = true
		current = parent
	}

	chain := make([]models.MetaNode, len(reversed))
	for i := range reversed {
		chain[i] = reversed[len(reversed)-1-i]
	}
	return chain, nil
}
