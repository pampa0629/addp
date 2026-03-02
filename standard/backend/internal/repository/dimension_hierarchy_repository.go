package repository

import (
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

// DimensionHierarchyRepository 维度层级仓库
type DimensionHierarchyRepository struct {
	db *gorm.DB
}

func NewDimensionHierarchyRepository(db *gorm.DB) *DimensionHierarchyRepository {
	return &DimensionHierarchyRepository{db: db}
}

func (r *DimensionHierarchyRepository) List(tenantID int64) ([]models.DimensionHierarchy, error) {
	var list []models.DimensionHierarchy
	err := r.db.Where("tenant_id = ?", tenantID).
		Order("created_at DESC").
		Find(&list).Error
	return list, err
}

func (r *DimensionHierarchyRepository) GetByID(id, tenantID int64) (*models.DimensionHierarchy, error) {
	var h models.DimensionHierarchy
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).
		Preload("Levels", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC, level_num ASC")
		}).
		First(&h).Error
	return &h, err
}

func (r *DimensionHierarchyRepository) Create(h *models.DimensionHierarchy) error {
	return r.db.Create(h).Error
}

func (r *DimensionHierarchyRepository) Update(h *models.DimensionHierarchy) error {
	return r.db.Save(h).Error
}

func (r *DimensionHierarchyRepository) Delete(id, tenantID int64) error {
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.DimensionHierarchy{}).Error
}

func (r *DimensionHierarchyRepository) ExistsByCode(code string, tenantID int64, excludeID int64) (bool, error) {
	var count int64
	query := r.db.Model(&models.DimensionHierarchy{}).Where("code = ? AND tenant_id = ?", code, tenantID)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	return count > 0, err
}

// --- 层级管理 ---

func (r *DimensionHierarchyRepository) GetLevels(hierarchyID int64) ([]models.DimensionHierarchyLevel, error) {
	var levels []models.DimensionHierarchyLevel
	err := r.db.Where("hierarchy_id = ?", hierarchyID).
		Order("sort_order ASC, level_num ASC").
		Find(&levels).Error
	return levels, err
}

func (r *DimensionHierarchyRepository) CreateLevel(level *models.DimensionHierarchyLevel) error {
	return r.db.Create(level).Error
}

func (r *DimensionHierarchyRepository) GetLevelByID(levelID, hierarchyID int64) (*models.DimensionHierarchyLevel, error) {
	var level models.DimensionHierarchyLevel
	err := r.db.Where("id = ? AND hierarchy_id = ?", levelID, hierarchyID).First(&level).Error
	return &level, err
}

func (r *DimensionHierarchyRepository) UpdateLevel(level *models.DimensionHierarchyLevel) error {
	return r.db.Save(level).Error
}

func (r *DimensionHierarchyRepository) DeleteLevel(levelID, hierarchyID int64) error {
	return r.db.Where("id = ? AND hierarchy_id = ?", levelID, hierarchyID).
		Delete(&models.DimensionHierarchyLevel{}).Error
}
