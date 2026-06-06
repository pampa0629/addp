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

	childNodesLite := make([]models.MetaNodeLite, len(childNodes))
	for i, node := range childNodes {
		childNodesLite[i] = metaquery.ToMetaNodeLite(node)
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
			return &result, nil
		}
	}

	trimmed := normalizeCatalogPath(catalogPath)
	if trimmed != "" {
		err := s.db.Where("tenant_id = ? AND engine_id = ? AND name = ? AND parent_node_id IS NULL AND deleted_at IS NULL", tenantID, engineID, trimmed).
			First(&node).Error
		if err == nil {
			result := metaquery.ToMetaNodeLite(node)
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

	return result, nil
}

func (s *MetadataQueryService) GetMetaNodeByID(tenantID, nodeID uint) (*models.MetaNodeLite, error) {
	var node models.MetaNode

	if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, nodeID).First(&node).Error; err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	result := metaquery.ToMetaNodeLite(node)
	return &result, nil
}
