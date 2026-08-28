package service

import (
	"errors"
	"fmt"

	"github.com/addp/asset/internal/models"
	"gorm.io/gorm"
)

var ErrAssetCategoryVersionConflict = errors.New("AssetCategory version conflict")

type CategoryService struct {
	db *gorm.DB
}

func NewCategoryService(db *gorm.DB) *CategoryService {
	return &CategoryService{db: db}
}

// CreateAssetCategoryRequest 创建目录请求
type CreateAssetCategoryRequest struct {
	Name        string `json:"name" binding:"required"`
	ParentID    *int64 `json:"parent_id"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

// UpdateAssetCategoryRequest 更新目录请求
type UpdateAssetCategoryRequest struct {
	Version     int64   `json:"version" binding:"required,min=1"`
	Name        *string `json:"name"`
	Description *string `json:"description"`
	SortOrder   *int    `json:"sort_order"`
}

type DeleteAssetCategoryRequest struct {
	Version int64 `json:"version" binding:"required,min=1"`
}

// AssetCategoryWithCount 带资产数量的目录（扁平列表用）
type AssetCategoryWithCount struct {
	models.AssetCategory
	Count int64 `json:"count"`
}

// AssetCategoryTreeNode 带子节点的资产分类目录树节点。
type AssetCategoryTreeNode struct {
	AssetCategoryWithCount
	Children []AssetCategoryTreeNode `json:"children"`
}

// ListAll 返回租户所有目录（扁平列表，含每个目录直接归属的资产数量）
func (s *CategoryService) ListAll(tenantID uint) ([]AssetCategoryWithCount, error) {
	var results []AssetCategoryWithCount
	err := s.db.Table("asset.categories c").
		Select("c.*, COUNT(a.id) AS count").
		Joins("LEFT JOIN asset.assets a ON a.category_id = c.id AND a.tenant_id = c.tenant_id").
		Where("c.tenant_id = ?", tenantID).
		Group("c.id").
		Order("c.sort_order ASC, c.id ASC").
		Scan(&results).Error
	return results, err
}

// GetTree 返回目录树
func (s *CategoryService) GetTree(tenantID uint) ([]AssetCategoryTreeNode, error) {
	categories, err := s.ListAll(tenantID)
	if err != nil {
		return nil, err
	}
	return buildCategoryTree(categories, nil), nil
}

// GetPublishedTree 返回至少包含一个已上架资产的目录及其必要祖先。
func (s *CategoryService) GetPublishedTree(tenantID uint) ([]AssetCategoryTreeNode, error) {
	var categories []AssetCategoryWithCount
	if err := s.db.Table("asset.categories c").
		Select("c.*, COUNT(a.id) AS count").
		Joins("LEFT JOIN asset.assets a ON a.category_id = c.id AND a.tenant_id = c.tenant_id AND a.status = 'published'").
		Where("c.tenant_id = ?", tenantID).
		Group("c.id").
		Order("c.sort_order ASC, c.id ASC").
		Scan(&categories).Error; err != nil {
		return nil, err
	}

	tree := buildCategoryTree(categories, nil)
	return keepPublishedCategoryBranches(tree), nil
}

func keepPublishedCategoryBranches(nodes []AssetCategoryTreeNode) []AssetCategoryTreeNode {
	result := make([]AssetCategoryTreeNode, 0, len(nodes))
	for _, node := range nodes {
		node.Children = keepPublishedCategoryBranches(node.Children)
		if node.Count > 0 || len(node.Children) > 0 {
			result = append(result, node)
		}
	}
	return result
}

func buildCategoryTree(categories []AssetCategoryWithCount, parentID *int64) []AssetCategoryTreeNode {
	var nodes []AssetCategoryTreeNode
	for _, c := range categories {
		cCopy := c
		isRoot := parentID == nil && c.ParentID == nil
		isChild := parentID != nil && c.ParentID != nil && *parentID == *c.ParentID
		if isRoot || isChild {
			node := AssetCategoryTreeNode{
				AssetCategoryWithCount: cCopy,
				Children:               buildCategoryTree(categories, &cCopy.ID),
			}
			nodes = append(nodes, node)
		}
	}
	return nodes
}

// Get 根据 ID 获取目录
func (s *CategoryService) Get(tenantID uint, id int64) (*models.AssetCategory, error) {
	var category models.AssetCategory
	err := s.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&category).Error
	if err != nil {
		return nil, err
	}
	return &category, nil
}

// checkDuplicateName 检查同级目录下是否已存在同名目录（excludeID 为 0 时不排除任何记录）
func (s *CategoryService) checkDuplicateName(tenantID uint, parentID *int64, name string, excludeID int64) error {
	var count int64
	// IS NOT DISTINCT FROM 是 PostgreSQL 的 NULL-safe 相等比较
	query := s.db.Model(&models.AssetCategory{}).
		Where("tenant_id = ? AND name = ? AND parent_id IS NOT DISTINCT FROM ?", tenantID, name, parentID)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("同一目录下已存在名称为「%s」的目录", name)
	}
	return nil
}

// Create 创建目录
func (s *CategoryService) Create(tenantID uint, req *CreateAssetCategoryRequest) (*models.AssetCategory, error) {
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

	category := &models.AssetCategory{
		TenantID:    int64(tenantID),
		Name:        req.Name,
		ParentID:    req.ParentID,
		Description: req.Description,
		SortOrder:   req.SortOrder,
	}
	if err := s.db.Create(category).Error; err != nil {
		return nil, err
	}
	return category, nil
}

// Update 更新目录
func (s *CategoryService) Update(tenantID uint, id int64, req *UpdateAssetCategoryRequest) (*models.AssetCategory, error) {
	category, err := s.Get(tenantID, id)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		// 同级唯一名称校验（排除自身）
		if err := s.checkDuplicateName(tenantID, category.ParentID, *req.Name, id); err != nil {
			return nil, err
		}
	}
	updates := map[string]any{"version": gorm.Expr("version + 1")}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.SortOrder != nil {
		updates["sort_order"] = *req.SortOrder
	}
	result := s.db.Model(&models.AssetCategory{}).
		Where("id = ? AND tenant_id = ? AND version = ?", id, tenantID, req.Version).
		Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		var count int64
		if err := s.db.Model(&models.AssetCategory{}).Where("id = ? AND tenant_id = ?", id, tenantID).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, ErrAssetCategoryVersionConflict
	}
	if err := s.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(category).Error; err != nil {
		return nil, err
	}
	return category, nil
}

// Delete 删除目录（如果有子目录或有资产关联则拒绝）
func (s *CategoryService) Delete(tenantID uint, id, version int64) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		var category models.AssetCategory
		if err := tx.Where("id = ? AND tenant_id = ?", id, tenantID).First(&category).Error; err != nil {
			return err
		}
		if category.Version != version {
			return ErrAssetCategoryVersionConflict
		}

		var childCount int64
		if err := tx.Model(&models.AssetCategory{}).Where("parent_id = ? AND tenant_id = ?", id, tenantID).Count(&childCount).Error; err != nil {
			return err
		}
		if childCount > 0 {
			return fmt.Errorf("该分类下有子分类，无法删除")
		}

		var assetCount int64
		if err := tx.Model(&models.Asset{}).Where("category_id = ? AND tenant_id = ?", id, tenantID).Count(&assetCount).Error; err != nil {
			return err
		}
		if assetCount > 0 {
			return fmt.Errorf("该分类下有 %d 个资产，无法删除", assetCount)
		}

		result := tx.Where("id = ? AND tenant_id = ? AND version = ?", id, tenantID, version).Delete(&models.AssetCategory{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return nil
		}
		var count int64
		if err := tx.Model(&models.AssetCategory{}).Where("id = ? AND tenant_id = ?", id, tenantID).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return gorm.ErrRecordNotFound
		}
		return ErrAssetCategoryVersionConflict
	})
}
