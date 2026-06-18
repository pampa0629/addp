package service

import (
	"fmt"

	"github.com/addp/common/datatype"
	"github.com/addp/meta/internal/metaquery"
	"github.com/addp/meta/internal/models"
)

func (s *MetadataQueryService) ListItemsByEngine(engineID, tenantID uint) ([]models.MetaItemLite, error) {
	var items []models.MetaItem

	var nodeIDs []uint
	err := s.db.Model(&models.MetaNode{}).
		Where("tenant_id = ? AND engine_id = ? AND deleted_at IS NULL", tenantID, engineID).
		Pluck("id", &nodeIDs).Error
	if err != nil {
		return nil, err
	}

	if len(nodeIDs) == 0 {
		return []models.MetaItemLite{}, nil
	}

	err = s.db.Select("*").
		Where("tenant_id = ? AND node_id IN (?) AND deleted_at IS NULL", tenantID, nodeIDs).
		Order("name").
		Find(&items).Error
	if err != nil {
		return nil, err
	}

	result := make([]models.MetaItemLite, len(items))
	for i, item := range items {
		result[i] = metaquery.ToMetaItemLite(item)
	}
	return result, nil
}

func (s *MetadataQueryService) ListItemsByBranch(engineID, tenantID uint, branch string) ([]models.MetaItemLite, error) {
	var node models.MetaNode
	err := s.db.Where("tenant_id = ? AND engine_id = ? AND name = ? AND parent_node_id IS NULL AND deleted_at IS NULL",
		tenantID, engineID, branch).
		First(&node).Error
	if err != nil {
		return nil, err
	}

	var items []models.MetaItem
	err = s.db.Select("*").
		Where("tenant_id = ? AND node_id = ? AND deleted_at IS NULL", tenantID, node.ID).
		Order("name").
		Find(&items).Error
	if err != nil {
		return nil, err
	}

	result := make([]models.MetaItemLite, len(items))
	for i, item := range items {
		result[i] = metaquery.ToMetaItemLite(item)
	}
	return result, nil
}

func (s *MetadataQueryService) GetItemFieldDetailsByID(tenantID, itemID uint) ([]datatype.FieldInfo, error) {
	var item models.MetaItem
	if err := s.db.Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, itemID).First(&item).Error; err != nil {
		return nil, fmt.Errorf("metadata snapshot not found: %w", err)
	}
	return metaquery.FieldsFromMetaItem(item)
}

func (s *MetadataQueryService) GetItemByCatalogPath(tenantID, engineID uint, catalogPath string) (*models.MetaItemLite, error) {
	var item models.MetaItem

	var lastErr error
	for _, candidate := range catalogPathCandidates(catalogPath) {
		err := s.db.Where("tenant_id = ? AND engine_id = ? AND deleted_at IS NULL", tenantID, engineID).
			Where("full_name = ?", candidate).
			First(&item).Error
		if err == nil {
			result := metaquery.ToMetaItemLite(item)
			return &result, nil
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, fmt.Errorf("item not found: %w", lastErr)
	}
	return nil, fmt.Errorf("item not found")
}

func (s *MetadataQueryService) GetNodeItems(tenantID, nodeID uint) ([]models.MetaItemLite, error) {
	var items []models.MetaItem

	var parentNode models.MetaNode
	if err := s.db.Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, nodeID).First(&parentNode).Error; err != nil {
		return nil, fmt.Errorf("parent node not found: %w", err)
	}

	if err := s.db.Where("tenant_id = ? AND node_id = ? AND deleted_at IS NULL", tenantID, nodeID).
		Order("name").
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("failed to query node items: %w", err)
	}

	result := make([]models.MetaItemLite, len(items))
	for i, item := range items {
		result[i] = metaquery.ToMetaItemLite(item)
	}

	return result, nil
}

func (s *MetadataQueryService) GetItemByID(tenantID, itemID uint) (*models.MetaItemLite, error) {
	var item models.MetaItem

	if err := s.db.Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, itemID).First(&item).Error; err != nil {
		return nil, fmt.Errorf("item not found: %w", err)
	}

	result := metaquery.ToMetaItemLite(item)
	return &result, nil
}

func (s *MetadataQueryService) GetItemAncestors(tenantID, itemID uint) (*models.MetaItemAncestorsResponse, error) {
	var item models.MetaItem
	if err := s.db.Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, itemID).First(&item).Error; err != nil {
		return nil, fmt.Errorf("item not found: %w", err)
	}

	var parent models.MetaNode
	if err := s.db.Where("tenant_id = ? AND id = ? AND engine_id = ? AND deleted_at IS NULL", tenantID, item.NodeID, item.EngineID).First(&parent).Error; err != nil {
		return nil, fmt.Errorf("item parent node not found: %w", err)
	}

	nodes, err := s.nodeAncestorChain(tenantID, parent)
	if err != nil {
		return nil, err
	}
	ancestors := make([]models.MetaNodeLite, len(nodes))
	for i, node := range nodes {
		ancestors[i] = metaquery.ToMetaNodeLite(node)
	}

	return &models.MetaItemAncestorsResponse{
		Item:      metaquery.ToMetaItemLite(item),
		Ancestors: ancestors,
	}, nil
}
