package repository

import (
	"context"
	"fmt"

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
	query := r.db.Where("is_active = ?", true)

	if resourceType != "" {
		query = query.Where("resource_type = ?", resourceType)
	}

	err := query.Offset(offset).Limit(limit).Find(&resources).Error
	return resources, err
}

// ListByTenant 查询指定租户的资源列表（包含内置资源）
func (r *ResourceRepository) ListByTenant(tenantID uint, offset, limit int, resourceType string) ([]models.Resource, error) {
	var resources []models.Resource

	// 查询条件：
	// 1. 租户自己创建的资源（tenant_id = tenantID）
	// 2. 内置资源（is_builtin = true AND tenant_id IS NULL）
	query := r.db.Where(
		"(tenant_id = ? OR (is_builtin = ? AND tenant_id IS NULL)) AND is_active = ?",
		tenantID, true, true,
	)

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
// FindByUniqueIdentifier 根据 unique_identifier 查询资源（包括软删除的记录）
func (r *ResourceRepository) FindByUniqueIdentifier(ctx context.Context, identifier string) (*models.Resource, error) {
	var resource models.Resource
	err := r.db.WithContext(ctx).Unscoped().Where("unique_identifier = ?", identifier).First(&resource).Error
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
		// 特殊处理：has_compute_capability 过滤
		if key == "has_compute_capability" {
			if hasCompute, ok := value.(bool); ok && hasCompute {
				// PostgreSQL JSONB 查询：capabilities->'compute' 不为空
				query = query.Where("capabilities IS NOT NULL AND capabilities::jsonb ? 'compute'")
			}
			continue
		}

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

// FindByNameAndTenant 根据名称和租户查找资源
func (r *ResourceRepository) FindByNameAndTenant(name string, tenantID uint) (*models.Resource, error) {
	var resource models.Resource
	err := r.db.Where("name = ? AND tenant_id = ?", name, tenantID).First(&resource).Error
	if err != nil {
		return nil, err
	}
	return &resource, nil
}

// FindByConnection 根据连接信息查找 PostgreSQL 资源
func (r *ResourceRepository) FindByConnection(tenantID uint, host string, port int, database string) (*models.Resource, error) {
	var resource models.Resource

	// 使用 JSONB 查询
	err := r.db.Where("tenant_id = ? AND resource_type IN (?, ?)", tenantID, "postgresql", "postgres").
		Where("connection_info->>'host' = ?", host).
		Where("connection_info->>'port' = ?", fmt.Sprintf("%d", port)).
		Where("connection_info->>'database' = ?", database).
		First(&resource).Error

	if err != nil {
		return nil, err
	}
	return &resource, nil
}
