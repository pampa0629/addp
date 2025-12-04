package repository

import (
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
)

type ExecutionRepository struct {
	db *gorm.DB
}

func NewExecutionRepository(db *gorm.DB) *ExecutionRepository {
	return &ExecutionRepository{db: db}
}

// Create 创建执行记录
func (r *ExecutionRepository) Create(execution *models.Execution) error {
	return r.db.Create(execution).Error
}

// Update 更新执行记录
func (r *ExecutionRepository) Update(execution *models.Execution) error {
	return r.db.Save(execution).Error
}

// GetByID 根据ID获取执行记录
func (r *ExecutionRepository) GetByID(id uint) (*models.Execution, error) {
	var execution models.Execution
	if err := r.db.First(&execution, id).Error; err != nil {
		return nil, err
	}
	return &execution, nil
}

// ListByTenant 获取租户的执行记录列表
func (r *ExecutionRepository) ListByTenant(tenantID uint, limit, offset int) ([]models.Execution, int64, error) {
	var executions []models.Execution
	var total int64

	// 查询总数
	if err := r.db.Model(&models.Execution{}).Where("tenant_id = ?", tenantID).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 查询列表
	if err := r.db.Where("tenant_id = ?", tenantID).
		Order("started_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&executions).Error; err != nil {
		return nil, 0, err
	}

	return executions, total, nil
}

// ListByScript 获取脚本的执行记录
func (r *ExecutionRepository) ListByScript(scriptID uint, limit int) ([]models.Execution, error) {
	var executions []models.Execution
	if err := r.db.Where("script_id = ?", scriptID).
		Order("started_at DESC").
		Limit(limit).
		Find(&executions).Error; err != nil {
		return nil, err
	}
	return executions, nil
}
