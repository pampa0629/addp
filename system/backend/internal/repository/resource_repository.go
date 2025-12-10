package repository

import (
	"context"

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

// ListByTenant 查询指定租户的资源列表
func (r *ResourceRepository) ListByTenant(tenantID uint, offset, limit int, resourceType string) ([]models.Resource, error) {
	var resources []models.Resource
	query := r.db.Where("tenant_id = ?", tenantID)

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
// FindByUniqueIdentifier 根据 unique_identifier 查询资源
func (r *ResourceRepository) FindByUniqueIdentifier(ctx context.Context, identifier string) (*models.Resource, error) {
	var resource models.Resource
	err := r.db.WithContext(ctx).Where("unique_identifier = ?", identifier).First(&resource).Error
	if err != nil {
		return nil, err
	}
	return &resource, nil
}

// FindByFilters 根据过滤条件查询资源列表
func (r *ResourceRepository) FindByFilters(ctx context.Context, filters map[string]interface{}) ([]*models.Resource, error) {
	var resources []*models.Resource
	query := r.db.WithContext(ctx)

	for key, value := range filters {
		query = query.Where(key+" = ?", value)
	}

	err := query.Find(&resources).Error
	if err != nil {
		return nil, err
	}

	return resources, nil
}

// UpdateByID 根据 ID 更新资源（支持部分更新）
func (r *ResourceRepository) UpdateByID(ctx context.Context, id uint, updates map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&models.Resource{}).Where("id = ?", id).Updates(updates).Error
}

// CreateWithContext 创建资源（带 context）
func (r *ResourceRepository) CreateWithContext(ctx context.Context, resource *models.Resource) error {
	return r.db.WithContext(ctx).Create(resource).Error
}
