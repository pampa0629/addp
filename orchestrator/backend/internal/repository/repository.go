package repository

import (
	"github.com/addp/orchestrator/internal/models"
	"gorm.io/gorm"
)

// OrchestrationRepository 编排数据访问
type OrchestrationRepository struct {
	db *gorm.DB
}

// NewOrchestrationRepository 创建编排仓库
func NewOrchestrationRepository(db *gorm.DB) *OrchestrationRepository {
	return &OrchestrationRepository{db: db}
}

// Create 创建编排
func (r *OrchestrationRepository) Create(orch *models.Orchestration) error {
	return r.db.Create(orch).Error
}

// GetByID 根据 ID 获取编排
func (r *OrchestrationRepository) GetByID(id uint) (*models.Orchestration, error) {
	var orch models.Orchestration
	if err := r.db.First(&orch, id).Error; err != nil {
		return nil, err
	}
	return &orch, nil
}

// List 列出租户的编排
func (r *OrchestrationRepository) List(tenantID uint) ([]models.Orchestration, error) {
	var orchs []models.Orchestration
	err := r.db.Where("tenant_id = ?", tenantID).Order("created_at DESC").Find(&orchs).Error
	return orchs, err
}

// ListEnabled 列出所有启用的编排
func (r *OrchestrationRepository) ListEnabled() ([]models.Orchestration, error) {
	var orchs []models.Orchestration
	err := r.db.Where("enabled = ?", true).Find(&orchs).Error
	return orchs, err
}

// Update 更新编排
func (r *OrchestrationRepository) Update(orch *models.Orchestration) error {
	return r.db.Save(orch).Error
}

// Delete 删除编排
func (r *OrchestrationRepository) Delete(id uint) error {
	return r.db.Delete(&models.Orchestration{}, id).Error
}

// ExecutionRepository 执行数据访问
type ExecutionRepository struct {
	db *gorm.DB
}

// NewExecutionRepository 创建执行仓库
func NewExecutionRepository(db *gorm.DB) *ExecutionRepository {
	return &ExecutionRepository{db: db}
}

// Create 创建执行
func (r *ExecutionRepository) Create(exec *models.Execution) error {
	return r.db.Create(exec).Error
}

// GetByID 根据 ID 获取执行
func (r *ExecutionRepository) GetByID(id uint) (*models.Execution, error) {
	var exec models.Execution
	if err := r.db.First(&exec, id).Error; err != nil {
		return nil, err
	}
	return &exec, nil
}

// List 列出编排的执行记录
func (r *ExecutionRepository) List(orchID uint, limit, offset int) ([]models.Execution, int64, error) {
	var execs []models.Execution
	var total int64

	query := r.db.Where("orchestration_id = ?", orchID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&execs).Error
	return execs, total, err
}

// ListAll 列出所有执行记录（支持租户过滤）
func (r *ExecutionRepository) ListAll(tenantID uint, limit, offset int) ([]models.Execution, int64, error) {
	var execs []models.Execution
	var total int64

	query := r.db.Joins("JOIN orchestrations ON executions.orchestration_id = orchestrations.id").
		Where("orchestrations.tenant_id = ?", tenantID)

	if err := query.Model(&models.Execution{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	err := query.Order("executions.created_at DESC").
		Preload("Orchestration").
		Limit(limit).
		Offset(offset).
		Find(&execs).Error
	return execs, total, err
}

// Update 更新执行
func (r *ExecutionRepository) Update(exec *models.Execution) error {
	return r.db.Save(exec).Error
}
