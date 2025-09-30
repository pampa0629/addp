package repository

import (
	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

type ResourceRepository struct {
	db *gorm.DB
}

func NewResourceRepository(db *gorm.DB) *ResourceRepository {
	return &ResourceRepository{db: db}
}

func (r *ResourceRepository) Create(resource *models.Resource) error {
	return r.db.Create(resource).Error
}

func (r *ResourceRepository) GetByID(id uint) (*models.Resource, error) {
	var resource models.Resource
	err := r.db.First(&resource, id).Error
	if err != nil {
		return nil, err
	}
	return &resource, nil
}

func (r *ResourceRepository) List(offset, limit int, resourceType string) ([]models.Resource, error) {
	var resources []models.Resource
	query := r.db

	if resourceType != "" {
		query = query.Where("resource_type = ?", resourceType)
	}

	err := query.Offset(offset).Limit(limit).Find(&resources).Error
	return resources, err
}

func (r *ResourceRepository) Update(resource *models.Resource) error {
	return r.db.Save(resource).Error
}

func (r *ResourceRepository) Delete(id uint) error {
	return r.db.Delete(&models.Resource{}, id).Error
}