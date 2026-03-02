package repository

import (
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

type TenantRepository struct {
	db *gorm.DB
}

func NewTenantRepository(db *gorm.DB) *TenantRepository {
	return &TenantRepository{db: db}
}

func (r *TenantRepository) Create(tenant *models.Tenant) error {
	return r.db.Create(tenant).Error
}

func (r *TenantRepository) GetByID(id uint) (*models.Tenant, error) {
	var tenant models.Tenant
	err := r.db.First(&tenant, id).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &tenant, nil
}

func (r *TenantRepository) GetByName(name string) (*models.Tenant, error) {
	var tenant models.Tenant
	err := r.db.Where("name = ?", name).First(&tenant).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &tenant, nil
}

func (r *TenantRepository) List(offset, limit int) ([]models.Tenant, int64, error) {
	var tenants []models.Tenant
	var total int64

	// 先获取总数
	if err := r.db.Model(&models.Tenant{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 再获取分页数据
	err := r.db.Offset(offset).Limit(limit).Order("created_at DESC").Find(&tenants).Error
	return tenants, total, err
}

func (r *TenantRepository) Update(tenant *models.Tenant) error {
	return r.db.Save(tenant).Error
}

func (r *TenantRepository) Delete(id uint) error {
	return r.db.Delete(&models.Tenant{}, id).Error
}
