package repository

import (
	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
)

type SpatialTaskRepository struct {
	db *gorm.DB
}

func NewSpatialTaskRepository(db *gorm.DB) *SpatialTaskRepository {
	return &SpatialTaskRepository{db: db}
}

// Create 创建 GIS 任务
func (r *SpatialTaskRepository) Create(task *models.SpatialTask) error {
	return r.db.Create(task).Error
}

// Update 更新 GIS 任务
func (r *SpatialTaskRepository) Update(task *models.SpatialTask) error {
	return r.db.Save(task).Error
}

// GetByID 根据ID获取任务
func (r *SpatialTaskRepository) GetByID(id uint, tenantID uint) (*models.SpatialTask, error) {
	var task models.SpatialTask
	if err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// ListByTenant 获取租户的任务列表
func (r *SpatialTaskRepository) ListByTenant(tenantID uint, page, pageSize int, forOrchestrator bool) ([]models.SpatialTask, int64, error) {
	var tasks []models.SpatialTask
	var total int64

	query := r.db.Model(&models.SpatialTask{}).Where("tenant_id = ?", tenantID)

	// Orchestrator 专用：只返回启用的任务
	if forOrchestrator {
		query = query.Where("status = ?", "active")
	}

	// 查询总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").
		Limit(pageSize).
		Offset(offset).
		Find(&tasks).Error; err != nil {
		return nil, 0, err
	}

	return tasks, total, nil
}

// DeleteByID 删除任务
func (r *SpatialTaskRepository) DeleteByID(id uint, tenantID uint) error {
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.SpatialTask{}).Error
}

// GetByName 根据名称获取任务
func (r *SpatialTaskRepository) GetByName(name string, tenantID uint) (*models.SpatialTask, error) {
	var task models.SpatialTask
	if err := r.db.Where("name = ? AND tenant_id = ?", name, tenantID).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// UpdateStatus 更新任务状态
func (r *SpatialTaskRepository) UpdateStatus(id uint, tenantID uint, status string) error {
	return r.db.Model(&models.SpatialTask{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("status", status).Error
}
