package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
)

type DimensionHierarchyRepository struct{ db *gorm.DB }

func NewDimensionHierarchyRepository(db *gorm.DB) *DimensionHierarchyRepository {
	return &DimensionHierarchyRepository{db: db}
}

func (r *DimensionHierarchyRepository) DB() *gorm.DB { return r.db }

func (r *DimensionHierarchyRepository) List(tableID, tenantID int64) ([]models.DimensionHierarchy, error) {
	var items []models.DimensionHierarchy
	err := r.db.Where("table_id = ? AND tenant_id = ?", tableID, tenantID).
		Preload("Levels", func(db *gorm.DB) *gorm.DB { return db.Order("level_num ASC, id ASC") }).
		Order("created_at ASC, id ASC").Find(&items).Error
	return items, commonrepo.WrapDBError(err)
}

func (r *DimensionHierarchyRepository) GetByID(id, tableID, tenantID int64) (*models.DimensionHierarchy, error) {
	var item models.DimensionHierarchy
	err := r.db.Where("id = ? AND table_id = ? AND tenant_id = ?", id, tableID, tenantID).
		Preload("Levels", func(db *gorm.DB) *gorm.DB { return db.Order("level_num ASC, id ASC") }).
		First(&item).Error
	return &item, commonrepo.WrapDBError(err)
}

func (r *DimensionHierarchyRepository) GetLevelByID(id, hierarchyID int64) (*models.DimensionHierarchyLevel, error) {
	var item models.DimensionHierarchyLevel
	err := r.db.Where("id = ? AND hierarchy_id = ?", id, hierarchyID).First(&item).Error
	return &item, commonrepo.WrapDBError(err)
}

func (r *DimensionHierarchyRepository) Create(item *models.DimensionHierarchy) error {
	return commonrepo.WrapDBError(r.db.Create(item).Error)
}

func (r *DimensionHierarchyRepository) Update(item *models.DimensionHierarchy) error {
	result := r.db.Model(&models.DimensionHierarchy{}).
		Where("id = ? AND table_id = ? AND tenant_id = ?", item.ID, item.TableID, item.TenantID).
		Updates(map[string]interface{}{"name": item.Name, "description": item.Description})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	return nil
}

func (r *DimensionHierarchyRepository) Delete(id, tableID, tenantID int64) error {
	result := r.db.Where("id = ? AND table_id = ? AND tenant_id = ?", id, tableID, tenantID).
		Delete(&models.DimensionHierarchy{})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	return nil
}

func (r *DimensionHierarchyRepository) CreateLevel(item *models.DimensionHierarchyLevel) error {
	return commonrepo.WrapDBError(r.db.Create(item).Error)
}

func (r *DimensionHierarchyRepository) UpdateLevel(item *models.DimensionHierarchyLevel) error {
	result := r.db.Model(&models.DimensionHierarchyLevel{}).
		Where("id = ? AND hierarchy_id = ?", item.ID, item.HierarchyID).
		Updates(map[string]interface{}{"field_id": item.FieldID, "level_num": item.LevelNum, "level_name": item.LevelName})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	return nil
}

func (r *DimensionHierarchyRepository) DeleteLevel(id, hierarchyID int64) error {
	result := r.db.Where("id = ? AND hierarchy_id = ?", id, hierarchyID).Delete(&models.DimensionHierarchyLevel{})
	if result.Error != nil {
		return commonrepo.WrapDBError(result.Error)
	}
	if result.RowsAffected != 1 {
		return commonrepo.WrapDBError(gorm.ErrRecordNotFound)
	}
	return nil
}
