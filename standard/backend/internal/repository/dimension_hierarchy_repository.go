package repository

import (
	commonrepo "github.com/addp/common/repository"
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
		Preload("Levels", func(db *gorm.DB) *gorm.DB {
			return db.Order("sort_order ASC, level_num ASC")
		}).
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
	return &h, commonrepo.WrapDBError(err)
}

func (r *DimensionHierarchyRepository) Create(h *models.DimensionHierarchy) error {
	return wrapDBError(r.db.Create(h).Error)
}

func (r *DimensionHierarchyRepository) Update(h *models.DimensionHierarchy, expectedVersion int64) error {
	if err := updateVersioned(r.db, h, h.ID, h.TenantID, expectedVersion, map[string]interface{}{
		"domain_id": h.DomainID, "name": h.Name, "description": h.Description, "updated_by": h.UpdatedBy,
	}); err != nil {
		return err
	}
	h.Version = expectedVersion + 1
	return nil
}

func (r *DimensionHierarchyRepository) Delete(id, tenantID int64) error {
	return deleteInTransaction(r.db, &models.DimensionHierarchy{}, "id = ? AND tenant_id = ?", id, tenantID)
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

func (r *DimensionHierarchyRepository) GetLevels(hierarchyID, tenantID int64) ([]models.DimensionHierarchyLevel, error) {
	var levels []models.DimensionHierarchyLevel
	err := r.db.Model(&models.DimensionHierarchyLevel{}).
		Joins("JOIN standard.dimension_hierarchies dh ON dh.id = standard.dimension_hierarchy_levels.hierarchy_id").
		Where("standard.dimension_hierarchy_levels.hierarchy_id = ? AND dh.tenant_id = ?", hierarchyID, tenantID).
		Order("sort_order ASC, level_num ASC").
		Find(&levels).Error
	return levels, err
}

func (r *DimensionHierarchyRepository) CreateLevel(level *models.DimensionHierarchyLevel, tenantID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := updateVersioned(tx, &models.DimensionHierarchy{}, level.HierarchyID, tenantID, expectedVersion, map[string]interface{}{}); err != nil {
			return err
		}
		return tx.Create(level).Error
	}))
}

func (r *DimensionHierarchyRepository) GetLevelByID(levelID, hierarchyID, tenantID int64) (*models.DimensionHierarchyLevel, error) {
	var level models.DimensionHierarchyLevel
	err := r.db.Model(&models.DimensionHierarchyLevel{}).
		Joins("JOIN standard.dimension_hierarchies dh ON dh.id = standard.dimension_hierarchy_levels.hierarchy_id").
		Where("standard.dimension_hierarchy_levels.id = ? AND standard.dimension_hierarchy_levels.hierarchy_id = ? AND dh.tenant_id = ?", levelID, hierarchyID, tenantID).
		First(&level).Error
	return &level, commonrepo.WrapDBError(err)
}

func (r *DimensionHierarchyRepository) UpdateLevel(level *models.DimensionHierarchyLevel, tenantID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := updateVersioned(tx, &models.DimensionHierarchy{}, level.HierarchyID, tenantID, expectedVersion, map[string]interface{}{}); err != nil {
			return err
		}
		return requireAffectedRow(tx.Model(&models.DimensionHierarchyLevel{}).Where("id = ? AND hierarchy_id = ?", level.ID, level.HierarchyID).Updates(map[string]interface{}{
			"level_num": level.LevelNum, "name": level.Name, "element_id": level.ElementID,
			"description": level.Description, "sort_order": level.SortOrder,
		}))
	}))
}

func (r *DimensionHierarchyRepository) DeleteLevel(levelID, hierarchyID, tenantID, expectedVersion int64) error {
	return wrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		if err := updateVersioned(tx, &models.DimensionHierarchy{}, hierarchyID, tenantID, expectedVersion, map[string]interface{}{}); err != nil {
			return err
		}
		return requireAffectedRow(tx.Where("id = ? AND hierarchy_id = ?", levelID, hierarchyID).Delete(&models.DimensionHierarchyLevel{}))
	}))
}

func (r *DimensionHierarchyRepository) ExistsLevelNum(hierarchyID, tenantID int64, levelNum int, excludeID int64) (bool, error) {
	var count int64
	query := r.db.Model(&models.DimensionHierarchyLevel{}).
		Joins("JOIN standard.dimension_hierarchies dh ON dh.id = standard.dimension_hierarchy_levels.hierarchy_id").
		Where("standard.dimension_hierarchy_levels.hierarchy_id = ? AND dh.tenant_id = ? AND standard.dimension_hierarchy_levels.level_num = ?", hierarchyID, tenantID, levelNum)
	if excludeID > 0 {
		query = query.Where("standard.dimension_hierarchy_levels.id != ?", excludeID)
	}
	err := query.Count(&count).Error
	return count > 0, err
}
