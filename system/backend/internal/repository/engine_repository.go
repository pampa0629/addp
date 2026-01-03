package repository

import (
	"context"
	"fmt"

	"github.com/addp/system/internal/models"
	"gorm.io/gorm"
)

type EngineRepository struct {
	db *gorm.DB
}

func NewEngineRepository(db *gorm.DB) *EngineRepository {
	return &EngineRepository{db: db}
}

func (r *EngineRepository) Create(engine *models.Engine) error {
	return r.db.Create(engine).Error
}

func (r *EngineRepository) GetByID(id uint) (*models.Engine, error) {
	var engine models.Engine
	err := r.db.First(&engine, id).Error
	if err != nil {
		return nil, err
	}
	return &engine, nil
}

func (r *EngineRepository) List(offset, limit int, engineType string) ([]models.Engine, error) {
	var engines []models.Engine
	query := r.db.Where("is_active = ?", true)

	if engineType != "" {
		query = query.Where("engine_type = ?", engineType)
	}

	err := query.Offset(offset).Limit(limit).Find(&engines).Error
	return engines, err
}

// ListByTenant 查询指定租户的资源列表（包含内置资源）
func (r *EngineRepository) ListByTenant(tenantID uint, offset, limit int, engineType string) ([]models.Engine, error) {
	var engines []models.Engine

	// 查询条件：
	// 1. 租户自己创建的资源（tenant_id = tenantID）
	// 2. 内置资源（is_builtin = true AND tenant_id IS NULL）
	query := r.db.Where(
		"(tenant_id = ? OR (is_builtin = ? AND tenant_id IS NULL)) AND is_active = ?",
		tenantID, true, true,
	)

	if engineType != "" {
		query = query.Where("engine_type = ?", engineType)
	}

	err := query.Offset(offset).Limit(limit).Find(&engines).Error
	return engines, err
}

func (r *EngineRepository) Update(engine *models.Engine) error {
	return r.db.Save(engine).Error
}

func (r *EngineRepository) Delete(id uint) error {
	return r.db.Delete(&models.Engine{}, id).Error
}
// FindByUniqueIdentifier 根据 unique_identifier 查询资源（包括软删除的记录）
func (r *EngineRepository) FindByUniqueIdentifier(ctx context.Context, identifier string) (*models.Engine, error) {
	var engine models.Engine
	err := r.db.WithContext(ctx).Unscoped().Where("unique_identifier = ?", identifier).First(&engine).Error
	if err != nil {
		return nil, err
	}
	return &engine, nil
}

// FindByEngineTypeAndBuiltin 根据 engine_type 和 is_builtin 查找引擎
func (r *EngineRepository) FindByEngineTypeAndBuiltin(ctx context.Context, engineType string) (*models.Engine, error) {
	var engine models.Engine
	err := r.db.WithContext(ctx).Where("engine_type = ? AND is_builtin = ?", engineType, true).First(&engine).Error
	if err != nil {
		return nil, err
	}
	return &engine, nil
}

// GetByEngineTypeAndTenant 根据 engine_type 和 tenant_id 查询引擎
// 用于工作流引擎自注册时查找是否已存在记录
func (r *EngineRepository) GetByEngineTypeAndTenant(engineType string, tenantID *uint) (*models.Engine, error) {
	var engine models.Engine
	query := r.db.Where("engine_type = ?", engineType)

	if tenantID == nil {
		query = query.Where("tenant_id IS NULL")
	} else {
		query = query.Where("tenant_id = ?", *tenantID)
	}

	if err := query.First(&engine).Error; err != nil {
		return nil, err
	}

	return &engine, nil
}

// FindByFilters 根据过滤条件查询引擎列表
func (r *EngineRepository) FindByFilters(ctx context.Context, filters map[string]interface{}) ([]*models.Engine, error) {
	var engines []*models.Engine
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

	err := query.Find(&engines).Error
	if err != nil {
		return nil, err
	}

	return engines, nil
}

// UpdateByID 根据 ID 更新资源（支持部分更新）
func (r *EngineRepository) UpdateByID(ctx context.Context, id uint, updates map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&models.Engine{}).Where("id = ?", id).Updates(updates).Error
}

// CreateWithContext 创建资源（带 context）
func (r *EngineRepository) CreateWithContext(ctx context.Context, engine *models.Engine) error {
	return r.db.WithContext(ctx).Create(engine).Error
}

// FindByNameAndTenant 根据名称和租户查找资源
func (r *EngineRepository) FindByNameAndTenant(name string, tenantID uint) (*models.Engine, error) {
	var engine models.Engine
	err := r.db.Where("name = ? AND tenant_id = ?", name, tenantID).First(&engine).Error
	if err != nil {
		return nil, err
	}
	return &engine, nil
}

// FindByConnection 根据连接信息查找 PostgreSQL 资源
func (r *EngineRepository) FindByConnection(tenantID uint, host string, port int, database string) (*models.Engine, error) {
	var engine models.Engine

	// 使用 JSONB 查询
	err := r.db.Where("tenant_id = ? AND engine_type IN (?, ?)", tenantID, "postgresql", "postgres").
		Where("connection_info->>'host' = ?", host).
		Where("connection_info->>'port' = ?", fmt.Sprintf("%d", port)).
		Where("connection_info->>'database' = ?", database).
		First(&engine).Error

	if err != nil {
		return nil, err
	}
	return &engine, nil
}
