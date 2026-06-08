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

// GetByIDAndTenant 根据 ID 和租户获取编排。
func (r *OrchestrationRepository) GetByIDAndTenant(id uint, tenantID uint) (*models.Orchestration, error) {
	var orch models.Orchestration
	if err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&orch).Error; err != nil {
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

// ListPaged 分页列出租户的编排。
func (r *OrchestrationRepository) ListPaged(tenantID uint, page, pageSize int) ([]models.Orchestration, int64, error) {
	var total int64
	query := r.db.Model(&models.Orchestration{}).Where("tenant_id = ?", tenantID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var orchs []models.Orchestration
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&orchs).Error
	return orchs, total, err
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

// 注意：ExecutionRepository 已废弃，现在使用统一的 ExecutionService
// 统一执行记录存储在 common.task_executions 表中
