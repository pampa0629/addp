package repository

import (
	"fmt"

	commonrepo "github.com/addp/common/repository"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
)

// LocalEngineRepository 管理本地存储引擎配置
type LocalEngineRepository struct {
	db *gorm.DB
}

// NewLocalEngineRepository 创建仓储实例
func NewLocalEngineRepository(db *gorm.DB) *LocalEngineRepository {
	return &LocalEngineRepository{db: db}
}

// List 返回指定租户下的本地存储引擎列表
func (r *LocalEngineRepository) List(tenantID uint, engineType string) ([]models.LocalEngine, error) {
	var engines []models.LocalEngine
	query := r.db.Model(&models.LocalEngine{}).Where("tenant_id = ?", tenantID)
	if engineType != "" {
		query = query.Where("resource_type = ?", engineType)
	}
	if err := query.Order("updated_at DESC").Find(&engines).Error; err != nil {
		return nil, fmt.Errorf("failed to list local engines: %w", err)
	}
	return engines, nil
}

// GetByID 获取单个引擎
func (r *LocalEngineRepository) GetByID(id, tenantID uint) (*models.LocalEngine, error) {
	var engine models.LocalEngine
	if err := r.db.First(&engine, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &engine, nil
}

// Create 新增引擎
func (r *LocalEngineRepository) Create(engine *models.LocalEngine) error {
	if err := r.db.Create(engine).Error; err != nil {
		return fmt.Errorf("failed to create local engine: %w", err)
	}
	return nil
}

// Update 更新引擎
func (r *LocalEngineRepository) Update(engine *models.LocalEngine) error {
	if err := r.db.Save(engine).Error; err != nil {
		return fmt.Errorf("failed to update local engine: %w", err)
	}
	return nil
}

// Delete 删除引擎
func (r *LocalEngineRepository) Delete(id, tenantID uint) error {
	if err := r.db.Delete(&models.LocalEngine{}, "id = ? AND tenant_id = ?", id, tenantID).Error; err != nil {
		return fmt.Errorf("failed to delete local engine: %w", err)
	}
	return nil
}
