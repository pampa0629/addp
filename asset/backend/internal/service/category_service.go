package service

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/addp/asset/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrAssetCategoryVersionConflict = errors.New("AssetCategory version conflict")
	ErrAssetCategoryParentNotFound  = errors.New("AssetCategory parent not found")
	ErrAssetCategoryInvalidParent   = errors.New("AssetCategory parent would create a cycle")
	ErrAssetCategoryDuplicateName   = errors.New("AssetCategory sibling name conflict")
)

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
	Version     int64  `json:"version" binding:"required,min=1"`
	Name        string `json:"name" binding:"required"`
	ParentID    *int64 `json:"parent_id" validate:"required" extensions:"x-nullable"`
	Description string `json:"description" validate:"required"`
	SortOrder   int    `json:"sort_order" binding:"min=0" validate:"required"`
	complete    bool
}

func (r *UpdateAssetCategoryRequest) UnmarshalJSON(data []byte) error {
	type requestAlias UpdateAssetCategoryRequest
	var decoded requestAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*r = UpdateAssetCategoryRequest(decoded)
	_, hasParent := fields["parent_id"]
	_, hasDescription := fields["description"]
	_, hasSortOrder := fields["sort_order"]
	r.complete = hasParent && hasDescription && hasSortOrder
	return nil
}

func (r *UpdateAssetCategoryRequest) IsComplete() bool {
	return r.complete
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
// Count 表示当前目录整棵子树中的已上架资产数，与消费端按子树浏览的语义一致。
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

	tree := keepPublishedCategoryBranches(buildCategoryTree(categories, nil))
	rollUpPublishedAssetCounts(tree)
	return tree, nil
}

// SubtreeIDs 返回指定目录及其全部后代目录 ID。不存在或跨租户的根目录返回 not found。
func (s *CategoryService) SubtreeIDs(tenantID uint, rootID int64) ([]int64, error) {
	var ids []int64
	if err := s.db.Raw(`WITH RECURSIVE category_tree AS (
		SELECT id
		FROM asset.categories
		WHERE tenant_id = ? AND id = ?
		UNION
		SELECT child.id
		FROM asset.categories child
		JOIN category_tree parent ON child.parent_id = parent.id
		WHERE child.tenant_id = ?
	)
	SELECT id FROM category_tree ORDER BY id`, tenantID, rootID, tenantID).Scan(&ids).Error; err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return ids, nil
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

func rollUpPublishedAssetCounts(nodes []AssetCategoryTreeNode) int64 {
	var total int64
	for i := range nodes {
		nodes[i].Count += rollUpPublishedAssetCounts(nodes[i].Children)
		total += nodes[i].Count
	}
	return total
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

// Create 创建目录
func (s *CategoryService) Create(tenantID uint, req *CreateAssetCategoryRequest) (*models.AssetCategory, error) {
	var category models.AssetCategory
	err := s.db.Transaction(func(tx *gorm.DB) error {
		categories, err := lockTenantCategoryGraph(tx, tenantID)
		if err != nil {
			return err
		}
		if req.ParentID != nil {
			if _, found := categoryByID(categories, *req.ParentID); !found {
				return ErrAssetCategoryParentNotFound
			}
		}
		if categoryNameExists(categories, req.ParentID, req.Name, 0) {
			return ErrAssetCategoryDuplicateName
		}
		category = models.AssetCategory{
			TenantID:    int64(tenantID),
			Name:        req.Name,
			ParentID:    req.ParentID,
			Description: req.Description,
			SortOrder:   req.SortOrder,
		}
		return tx.Create(&category).Error
	})
	if err != nil {
		return nil, err
	}
	return &category, nil
}

// Update 完整更新目录节点，包括其父子位置。
func (s *CategoryService) Update(tenantID uint, id int64, req *UpdateAssetCategoryRequest) (*models.AssetCategory, error) {
	var updated models.AssetCategory
	err := s.db.Transaction(func(tx *gorm.DB) error {
		categories, err := lockTenantCategoryGraph(tx, tenantID)
		if err != nil {
			return err
		}
		category, found := categoryByID(categories, id)
		if !found {
			return gorm.ErrRecordNotFound
		}
		if category.Version != req.Version {
			return ErrAssetCategoryVersionConflict
		}
		if err := validateCategoryParent(categories, id, req.ParentID); err != nil {
			return err
		}
		if categoryNameExists(categories, req.ParentID, req.Name, id) {
			return ErrAssetCategoryDuplicateName
		}
		var parentValue any
		if req.ParentID != nil {
			parentValue = *req.ParentID
		}

		result := tx.Model(&models.AssetCategory{}).
			Where("id = ? AND tenant_id = ? AND version = ?", id, tenantID, req.Version).
			Updates(map[string]any{
				"name":        req.Name,
				"parent_id":   parentValue,
				"description": req.Description,
				"sort_order":  req.SortOrder,
				"version":     gorm.Expr("version + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrAssetCategoryVersionConflict
		}
		return tx.Where("id = ? AND tenant_id = ?", id, tenantID).First(&updated).Error
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func lockTenantCategoryGraph(tx *gorm.DB, tenantID uint) ([]models.AssetCategory, error) {
	query := tx.Where("tenant_id = ?", tenantID).Order("id ASC")
	if tx.Dialector.Name() == "postgres" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var categories []models.AssetCategory
	if err := query.Find(&categories).Error; err != nil {
		return nil, err
	}
	return categories, nil
}

func categoryByID(categories []models.AssetCategory, id int64) (models.AssetCategory, bool) {
	for _, category := range categories {
		if category.ID == id {
			return category, true
		}
	}
	return models.AssetCategory{}, false
}

func validateCategoryParent(categories []models.AssetCategory, categoryID int64, parentID *int64) error {
	if parentID == nil {
		return nil
	}
	byID := make(map[int64]models.AssetCategory, len(categories))
	for _, category := range categories {
		byID[category.ID] = category
	}
	visited := map[int64]struct{}{categoryID: {}}
	currentID := *parentID
	for {
		if _, seen := visited[currentID]; seen {
			return ErrAssetCategoryInvalidParent
		}
		visited[currentID] = struct{}{}
		parent, found := byID[currentID]
		if !found {
			return ErrAssetCategoryParentNotFound
		}
		if parent.ParentID == nil {
			return nil
		}
		currentID = *parent.ParentID
	}
}

func categoryNameExists(categories []models.AssetCategory, parentID *int64, name string, excludeID int64) bool {
	for _, category := range categories {
		if category.ID != excludeID && category.Name == name && equalOptionalInt64(category.ParentID, parentID) {
			return true
		}
	}
	return false
}

func equalOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// Delete 删除目录（如果有子目录或有资产关联则拒绝）
func (s *CategoryService) Delete(tenantID uint, id, version int64) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		categories, err := lockTenantCategoryGraph(tx, tenantID)
		if err != nil {
			return err
		}
		category, found := categoryByID(categories, id)
		if !found {
			return gorm.ErrRecordNotFound
		}
		if category.Version != version {
			return ErrAssetCategoryVersionConflict
		}

		if categoryHasChildren(categories, id) {
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

func categoryHasChildren(categories []models.AssetCategory, categoryID int64) bool {
	for _, category := range categories {
		if category.ParentID != nil && *category.ParentID == categoryID {
			return true
		}
	}
	return false
}
