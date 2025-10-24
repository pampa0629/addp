package repository

import (
	"fmt"

	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
)

// LocalResourceRepository 管理本地存储引擎配置
type LocalResourceRepository struct {
	db *gorm.DB
}

// NewLocalResourceRepository 创建仓储实例
func NewLocalResourceRepository(db *gorm.DB) *LocalResourceRepository {
	return &LocalResourceRepository{db: db}
}

// List 返回指定租户下的本地存储引擎列表
func (r *LocalResourceRepository) List(tenantID uint, resourceType string) ([]models.LocalResource, error) {
	var resources []models.LocalResource
	query := r.db.Model(&models.LocalResource{}).Where("tenant_id = ?", tenantID)
	if resourceType != "" {
		query = query.Where("resource_type = ?", resourceType)
	}
	if err := query.Order("updated_at DESC").Find(&resources).Error; err != nil {
		return nil, fmt.Errorf("failed to list local resources: %w", err)
	}
	return resources, nil
}

// GetByID 获取单个资源
func (r *LocalResourceRepository) GetByID(id, tenantID uint) (*models.LocalResource, error) {
	var resource models.LocalResource
	if err := r.db.First(&resource, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
		return nil, err
	}
	return &resource, nil
}

// Create 新增资源
func (r *LocalResourceRepository) Create(resource *models.LocalResource) error {
	if err := r.db.Create(resource).Error; err != nil {
		return fmt.Errorf("failed to create local resource: %w", err)
	}
	return nil
}

// Update 更新资源
func (r *LocalResourceRepository) Update(resource *models.LocalResource) error {
	if err := r.db.Save(resource).Error; err != nil {
		return fmt.Errorf("failed to update local resource: %w", err)
	}
	return nil
}

// Delete 删除资源
func (r *LocalResourceRepository) Delete(id, tenantID uint) error {
	if err := r.db.Delete(&models.LocalResource{}, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
		return fmt.Errorf("failed to delete local resource: %w", err)
	}
	return nil
}
