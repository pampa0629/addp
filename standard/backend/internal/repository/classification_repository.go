package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

// ClassificationRepository 数据分类仓库
type ClassificationRepository struct {
	db *gorm.DB
}

func NewClassificationRepository(db *gorm.DB) *ClassificationRepository {
	return &ClassificationRepository{db: db}
}

func (r *ClassificationRepository) List(tenantID int64) ([]models.Classification, error) {
	var list []models.Classification
	err := r.db.Where("tenant_id = ?", tenantID).Order("sort_order ASC, id ASC").Find(&list).Error
	return list, err
}

func (r *ClassificationRepository) GetByID(id, tenantID int64) (*models.Classification, error) {
	var c models.Classification
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&c).Error
	return &c, commonrepo.WrapDBError(err)
}

func (r *ClassificationRepository) Create(c *models.Classification) error {
	return wrapDBError(r.db.Create(c).Error)
}

func (r *ClassificationRepository) Update(c *models.Classification, expectedVersion int64) error {
	if err := updateVersioned(r.db, c, c.ID, c.TenantID, expectedVersion, map[string]interface{}{
		"name": c.Name, "description": c.Description, "parent_id": c.ParentID,
		"sort_order": c.SortOrder, "updated_by": c.UpdatedBy,
	}); err != nil {
		return err
	}
	c.Version = expectedVersion + 1
	return nil
}

func (r *ClassificationRepository) Delete(id, tenantID int64) error {
	return deleteInTransaction(r.db, &models.Classification{}, "id = ? AND tenant_id = ?", id, tenantID)
}

// GradingLevelRepository 数据分级仓库
type GradingLevelRepository struct {
	db *gorm.DB
}

func NewGradingLevelRepository(db *gorm.DB) *GradingLevelRepository {
	return &GradingLevelRepository{db: db}
}

func (r *GradingLevelRepository) List(tenantID int64) ([]models.GradingLevel, error) {
	var levels []models.GradingLevel
	err := r.db.Where("tenant_id = ?", tenantID).Order("sort_order ASC").Find(&levels).Error
	return levels, err
}

func (r *GradingLevelRepository) Update(id, tenantID int64, req *models.UpdateGradingLevelRequest) error {
	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	updates["description"] = req.Description
	if req.Color != "" {
		updates["color"] = req.Color
	}
	return updateVersioned(r.db, &models.GradingLevel{}, id, tenantID, req.Version, updates)
}

// EnsureDefaults 确保租户有默认的 L1-L4 分级数据
func (r *GradingLevelRepository) EnsureDefaults(tenantID int64) error {
	defaults := []models.GradingLevel{
		{TenantID: tenantID, Level: "L1", Name: "公开", Description: "可对外发布，无敏感性", Color: "#67C23A", SortOrder: 1},
		{TenantID: tenantID, Level: "L2", Name: "内部", Description: "限内部员工访问，不得对外公开", Color: "#409EFF", SortOrder: 2},
		{TenantID: tenantID, Level: "L3", Name: "机密", Description: "严格受控，需定向授权方可访问", Color: "#E6A23C", SortOrder: 3},
		{TenantID: tenantID, Level: "L4", Name: "绝密", Description: "最高保护级别，极少数人可访问", Color: "#F56C6C", SortOrder: 4},
	}
	for _, d := range defaults {
		var count int64
		r.db.Model(&models.GradingLevel{}).Where("tenant_id = ? AND level = ?", tenantID, d.Level).Count(&count)
		if count == 0 {
			if err := r.db.Create(&d).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
