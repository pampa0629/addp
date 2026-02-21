package repository

import (
	"github.com/addp/standard/internal/models"
	"gorm.io/gorm"
)

// MeasurementCategoryRepository 度量类别仓库
type MeasurementCategoryRepository struct {
	db *gorm.DB
}

func NewMeasurementCategoryRepository(db *gorm.DB) *MeasurementCategoryRepository {
	return &MeasurementCategoryRepository{db: db}
}

func (r *MeasurementCategoryRepository) List(tenantID int64) ([]models.MeasurementCategory, error) {
	var categories []models.MeasurementCategory
	err := r.db.Where("tenant_id = ?", tenantID).Order("sort_order ASC, id ASC").Find(&categories).Error
	return categories, err
}

func (r *MeasurementCategoryRepository) GetByID(id, tenantID int64) (*models.MeasurementCategory, error) {
	var cat models.MeasurementCategory
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&cat).Error
	return &cat, err
}

func (r *MeasurementCategoryRepository) Create(cat *models.MeasurementCategory) error {
	return r.db.Create(cat).Error
}

func (r *MeasurementCategoryRepository) Update(cat *models.MeasurementCategory) error {
	return r.db.Save(cat).Error
}

func (r *MeasurementCategoryRepository) Delete(id, tenantID int64) error {
	return r.db.Where("id = ? AND tenant_id = ? AND is_system = false", id, tenantID).Delete(&models.MeasurementCategory{}).Error
}

func (r *MeasurementCategoryRepository) ExistsByCode(code string, tenantID int64, excludeID int64) (bool, error) {
	var count int64
	query := r.db.Model(&models.MeasurementCategory{}).Where("code = ? AND tenant_id = ?", code, tenantID)
	if excludeID > 0 {
		query = query.Where("id != ?", excludeID)
	}
	err := query.Count(&count).Error
	return count > 0, err
}

// UnitRepository 计量单位仓库
type UnitRepository struct {
	db *gorm.DB
}

func NewUnitRepository(db *gorm.DB) *UnitRepository {
	return &UnitRepository{db: db}
}

func (r *UnitRepository) List(tenantID int64, categoryID *int64) ([]models.Unit, error) {
	query := r.db.Where("tenant_id = ?", tenantID)
	if categoryID != nil {
		query = query.Where("category_id = ?", *categoryID)
	}
	var units []models.Unit
	err := query.Preload("Category").Order("sort_order ASC, id ASC").Find(&units).Error
	return units, err
}

func (r *UnitRepository) GetByID(id, tenantID int64) (*models.Unit, error) {
	var unit models.Unit
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Preload("Category").First(&unit).Error
	return &unit, err
}

func (r *UnitRepository) Create(unit *models.Unit) error {
	return r.db.Create(unit).Error
}

func (r *UnitRepository) Update(unit *models.Unit) error {
	return r.db.Save(unit).Error
}

func (r *UnitRepository) Delete(id, tenantID int64) error {
	return r.db.Where("id = ? AND tenant_id = ? AND is_system = false", id, tenantID).Delete(&models.Unit{}).Error
}
