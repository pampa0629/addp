package service

import (
	"fmt"

	"github.com/addp/asset/internal/models"
	"gorm.io/gorm"
)

type CatalogService struct {
	db *gorm.DB
}

func NewCatalogService(db *gorm.DB) *CatalogService {
	return &CatalogService{db: db}
}

// CreateCatalogReq 创建目录请求
type CreateCatalogReq struct {
	Name        string `json:"name" binding:"required"`
	ParentID    *int64 `json:"parent_id"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

// UpdateCatalogReq 更新目录请求
type UpdateCatalogReq struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	SortOrder   *int    `json:"sort_order"`
}

// CatalogWithCount 带资产数量的目录（扁平列表用）
type CatalogWithCount struct {
	models.Catalog
	Count int64 `json:"count"`
}

// CatalogEntry 带子节点的目录树节点
type CatalogEntry struct {
	CatalogWithCount
	Children []CatalogEntry `json:"children"`
}

// ListAll 返回租户所有目录（扁平列表，含每个目录直接归属的资产数量）
func (s *CatalogService) ListAll(tenantID uint) ([]CatalogWithCount, error) {
	var results []CatalogWithCount
	err := s.db.Table("asset.catalogs c").
		Select("c.*, COUNT(a.id) AS count").
		Joins("LEFT JOIN asset.assets a ON a.catalog_id = c.id AND a.tenant_id = c.tenant_id").
		Where("c.tenant_id = ?", tenantID).
		Group("c.id").
		Order("c.sort_order ASC, c.id ASC").
		Scan(&results).Error
	return results, err
}

// GetTree 返回目录树
func (s *CatalogService) GetTree(tenantID uint) ([]CatalogEntry, error) {
	cats, err := s.ListAll(tenantID)
	if err != nil {
		return nil, err
	}
	return buildCatalogTree(cats, nil), nil
}

// GetPublishedTree 返回至少包含一个已上架资产的目录及其必要祖先。
func (s *CatalogService) GetPublishedTree(tenantID uint) ([]CatalogEntry, error) {
	var catalogs []CatalogWithCount
	if err := s.db.Table("asset.catalogs c").
		Select("c.*, COUNT(a.id) AS count").
		Joins("LEFT JOIN asset.assets a ON a.catalog_id = c.id AND a.tenant_id = c.tenant_id AND a.status = 'published'").
		Where("c.tenant_id = ?", tenantID).
		Group("c.id").
		Order("c.sort_order ASC, c.id ASC").
		Scan(&catalogs).Error; err != nil {
		return nil, err
	}

	tree := buildCatalogTree(catalogs, nil)
	return keepPublishedCatalogBranches(tree), nil
}

func keepPublishedCatalogBranches(nodes []CatalogEntry) []CatalogEntry {
	result := make([]CatalogEntry, 0, len(nodes))
	for _, node := range nodes {
		node.Children = keepPublishedCatalogBranches(node.Children)
		if node.Count > 0 || len(node.Children) > 0 {
			result = append(result, node)
		}
	}
	return result
}

func buildCatalogTree(cats []CatalogWithCount, parentID *int64) []CatalogEntry {
	var nodes []CatalogEntry
	for _, c := range cats {
		cCopy := c
		isRoot := parentID == nil && c.ParentID == nil
		isChild := parentID != nil && c.ParentID != nil && *parentID == *c.ParentID
		if isRoot || isChild {
			node := CatalogEntry{
				CatalogWithCount: cCopy,
				Children:         buildCatalogTree(cats, &cCopy.ID),
			}
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// Get 根据 ID 获取目录
func (s *CatalogService) Get(tenantID uint, id int64) (*models.Catalog, error) {
	var cat models.Catalog
	err := s.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&cat).Error
	if err != nil {
		return nil, err
	}
	return &cat, nil
}

// checkDuplicateName 检查同级目录下是否已存在同名目录（excludeID 为 0 时不排除任何记录）
func (s *CatalogService) checkDuplicateName(tenantID uint, parentID *int64, name string, excludeID int64) error {
	var count int64
	// IS NOT DISTINCT FROM 是 PostgreSQL 的 NULL-safe 相等比较
	query := s.db.Model(&models.Catalog{}).
		Where("tenant_id = ? AND name = ? AND parent_id IS NOT DISTINCT FROM ?", tenantID, name, parentID)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	query.Count(&count)
	if count > 0 {
		return fmt.Errorf("同一目录下已存在名称为「%s」的目录", name)
	}
	return nil
}

// Create 创建目录
func (s *CatalogService) Create(tenantID uint, req *CreateCatalogReq) (*models.Catalog, error) {
	// 校验父目录存在
	if req.ParentID != nil {
		if _, err := s.Get(tenantID, *req.ParentID); err != nil {
			return nil, fmt.Errorf("父目录不存在")
		}
	}

	// 同级唯一名称校验
	if err := s.checkDuplicateName(tenantID, req.ParentID, req.Name, 0); err != nil {
		return nil, err
	}

	cat := &models.Catalog{
		TenantID:    int64(tenantID),
		Name:        req.Name,
		ParentID:    req.ParentID,
		Description: req.Description,
		SortOrder:   req.SortOrder,
	}
	if err := s.db.Create(cat).Error; err != nil {
		return nil, err
	}
	return cat, nil
}

// Update 更新目录
func (s *CatalogService) Update(tenantID uint, id int64, req *UpdateCatalogReq) (*models.Catalog, error) {
	cat, err := s.Get(tenantID, id)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		// 同级唯一名称校验（排除自身）
		if err := s.checkDuplicateName(tenantID, cat.ParentID, *req.Name, id); err != nil {
			return nil, err
		}
		cat.Name = *req.Name
	}
	if req.Description != nil {
		cat.Description = *req.Description
	}
	if req.SortOrder != nil {
		cat.SortOrder = *req.SortOrder
	}

	if err := s.db.Save(cat).Error; err != nil {
		return nil, err
	}
	return cat, nil
}

// Delete 删除目录（如果有子目录或有资产关联则拒绝）
func (s *CatalogService) Delete(tenantID uint, id int64) error {
	cat, err := s.Get(tenantID, id)
	if err != nil {
		return err
	}

	// 检查是否有子目录
	var childCount int64
	s.db.Model(&models.Catalog{}).Where("parent_id = ? AND tenant_id = ?", id, tenantID).Count(&childCount)
	if childCount > 0 {
		return fmt.Errorf("该目录下有子目录，无法删除")
	}

	// 检查是否有资产引用
	var assetCount int64
	s.db.Model(&models.Asset{}).Where("catalog_id = ? AND tenant_id = ?", id, tenantID).Count(&assetCount)
	if assetCount > 0 {
		return fmt.Errorf("该目录下有 %d 个资产，无法删除", assetCount)
	}

	return s.db.Delete(cat).Error
}
