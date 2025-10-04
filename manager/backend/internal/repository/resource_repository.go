package repository

import (
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

type ResourceRepository struct {
	db *gorm.DB
}

func NewResourceRepository(db *gorm.DB) *ResourceRepository {
	return &ResourceRepository{db: db}
}

// List 获取资源列表
func (r *ResourceRepository) List(page, pageSize int, resourceType string) ([]models.Resource, int64, error) {
	var resources []models.Resource
	var total int64

	query := r.db.Model(&models.Resource{}).Where("is_active = ?", true)

	if resourceType != "" {
		query = query.Where("resource_type = ?", resourceType)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := query.Offset(offset).Limit(pageSize).Find(&resources).Error; err != nil {
		return nil, 0, err
	}

	return resources, total, nil
}

// GetByID 根据ID获取资源
func (r *ResourceRepository) GetByID(id uint) (*models.Resource, error) {
	var resource models.Resource
	if err := r.db.First(&resource, id).Error; err != nil {
		return nil, err
	}
	return &resource, nil
}
