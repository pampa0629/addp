package service

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/meta/internal/metapath"
	"github.com/addp/meta/internal/metaquery"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	"gorm.io/gorm"
)

// MetadataQueryService 元数据查询服务
// 提供Manager和Transfer模块的元数据查询接口
type MetadataQueryService struct {
	db            *gorm.DB
	repo          *metaRepo.ScanRepository
	engineService *EngineService
	log           *slog.Logger
}

// NewMetadataQueryService 创建元数据查询服务
func NewMetadataQueryService(db *gorm.DB, engineService *EngineService, log *slog.Logger) *MetadataQueryService {
	return &MetadataQueryService{
		db:            db,
		repo:          metaRepo.NewScanRepository(db),
		engineService: engineService,
		log:           log,
	}
}

// ============================================================================
// 通用元数据查询接口
// ============================================================================

func (s *MetadataQueryService) CountItems(tenantID uint) (int64, error) {
	var itemCount int64
	if err := s.db.Table("meta.meta_item").Where("tenant_id = ?", tenantID).Count(&itemCount).Error; err != nil {
		return 0, err
	}
	return itemCount, nil
}

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

// ============================================================================
// Manager 模块查询接口
// ============================================================================

func (s *MetadataQueryService) GetMetadataTree(tenantID, engineID uint) (*models.MetadataTreeResponse, error) {
	if err := s.ensureEngineCatalogRoot(tenantID, engineID); err != nil {
		return nil, err
	}

	// 查询顶层节点
	var topNodes []models.MetaNode
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id IS NULL AND full_name = '' AND deleted_at IS NULL", tenantID, engineID).
		Find(&topNodes).Error; err != nil {
		return nil, fmt.Errorf("failed to query top nodes: %w", err)
	}

	// 查询子节点
	var childNodes []models.MetaNode
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND parent_node_id IS NOT NULL AND deleted_at IS NULL", tenantID, engineID).
		Find(&childNodes).Error; err != nil {
		return nil, fmt.Errorf("failed to query child nodes: %w", err)
	}

	// 查询所有项（只返回 node_id 存在于 meta_node 中的 items，过滤孤立记录）
	var items []models.MetaItem
	if err := s.db.Where("tenant_id = ? AND engine_id = ? AND deleted_at IS NULL AND node_id IN (SELECT id FROM meta.meta_node WHERE tenant_id = ? AND engine_id = ? AND deleted_at IS NULL)",
		tenantID, engineID, tenantID, engineID).
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("failed to query items: %w", err)
	}

	// 转换为 Lite 模型
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

func (s *MetadataQueryService) ensureEngineCatalogRoot(tenantID, engineID uint) error {
	if s.engineService == nil {
		return fmt.Errorf("engine service is not available")
	}
	resource, err := s.engineService.GetResourceByID(engineID, tenantID, "")
	if err != nil {
		return fmt.Errorf("failed to get resource: %w", err)
	}
	enginePlugin, err := plugin.Get(resource.EngineType)
	if err != nil {
		return fmt.Errorf("unsupported engine type %s: %w", resource.EngineType, err)
	}
	if _, err := metaRepo.EnsureCatalogRootNode(s.repo, tenantID, resource, enginePlugin); err != nil {
		return fmt.Errorf("failed to ensure catalog root: %w", err)
	}
	if err := s.repo.HardDeleteInvalidEngineGraph(tenantID, engineID); err != nil {
		return fmt.Errorf("failed to reconcile metadata tree: %w", err)
	}
	return nil
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

func catalogPathCandidates(catalogPath string) []string {
	trimmed := normalizeCatalogPath(catalogPath)
	if trimmed == "" {
		return []string{""}
	}
	candidates := []string{trimmed}
	if strings.Contains(trimmed, "/") {
		candidates = append(candidates, strings.ReplaceAll(trimmed, "/", "."))
	}
	return candidates
}

func normalizeCatalogPath(catalogPath string) string {
	return metapath.SanitizeFSPath(catalogPath)
}

func (s *MetadataQueryService) GetNodeChildren(tenantID, nodeID uint) ([]models.MetaNodeLite, error) {
	var nodes []models.MetaNode

	// 先查询节点是否存在并验证租户
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

func (s *MetadataQueryService) GetNodeItems(tenantID, nodeID uint) ([]models.MetaItemLite, error) {
	var items []models.MetaItem

	// 先查询节点是否存在并验证租户
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

func (s *MetadataQueryService) GetItemSpatialMetadataByID(tenantID, itemID uint) (*models.SpatialMetadataResponse, error) {
	var item models.MetaItem
	if err := s.db.Where("tenant_id = ? AND id = ? AND deleted_at IS NULL", tenantID, itemID).First(&item).Error; err != nil {
		return nil, fmt.Errorf("metadata snapshot not found: %w", err)
	}
	return metaquery.SpatialMetadataFromItem(item)
}

func (s *MetadataQueryService) GetMetaNodeByID(tenantID, nodeID uint) (*models.MetaNodeLite, error) {
	var node models.MetaNode

	if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, nodeID).First(&node).Error; err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}

	result := metaquery.ToMetaNodeLite(node)
	return &result, nil
}

func (s *MetadataQueryService) GetItemByID(tenantID, itemID uint) (*models.MetaItemLite, error) {
	var item models.MetaItem

	if err := s.db.Where("tenant_id = ? AND id = ?", tenantID, itemID).First(&item).Error; err != nil {
		return nil, fmt.Errorf("item not found: %w", err)
	}

	result := metaquery.ToMetaItemLite(item)
	return &result, nil
}
