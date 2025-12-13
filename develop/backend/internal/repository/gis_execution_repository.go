package repository

import (
	"time"

	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
)

type GISExecutionRepository struct {
	db *gorm.DB
}

func NewGISExecutionRepository(db *gorm.DB) *GISExecutionRepository {
	return &GISExecutionRepository{db: db}
}

// Create 创建执行记录
func (r *GISExecutionRepository) Create(execution *models.GISExecution) error {
	return r.db.Create(execution).Error
}

// Update 更新执行记录
func (r *GISExecutionRepository) Update(execution *models.GISExecution) error {
	return r.db.Save(execution).Error
}

// GetByID 根据ID获取执行记录
func (r *GISExecutionRepository) GetByID(id uint, tenantID uint) (*models.GISExecution, error) {
	var execution models.GISExecution
	if err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&execution).Error; err != nil {
		return nil, err
	}
	return &execution, nil
}

// GetByIDWithTask 根据ID获取执行记录（包含任务信息）
func (r *GISExecutionRepository) GetByIDWithTask(id uint, tenantID uint) (*models.GISExecutionResponse, error) {
	var execution models.GISExecution
	if err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&execution).Error; err != nil {
		return nil, err
	}

	response := &models.GISExecutionResponse{
		GISExecution: execution,
	}

	// 加载关联任务
	if execution.TaskID != nil {
		var task models.SpatialTask
		if err := r.db.Where("id = ? AND tenant_id = ?", *execution.TaskID, tenantID).First(&task).Error; err == nil {
			response.Task = &task
		}
	}

	return response, nil
}

// List 查询执行列表（支持多条件筛选）
func (r *GISExecutionRepository) List(req *models.ListExecutionsRequest, tenantID uint) ([]models.GISExecutionResponse, int64, error) {
	var executions []models.GISExecution
	var total int64

	query := r.db.Model(&models.GISExecution{}).Where("tenant_id = ?", tenantID)

	// 任务ID筛选
	if req.TaskID != nil {
		query = query.Where("task_id = ?", *req.TaskID)
	}

	// 状态筛选
	if req.Status != "" {
		query = query.Where("status = ?", req.Status)
	}

	// 触发类型筛选
	if req.TriggerType != "" {
		query = query.Where("trigger_type = ?", req.TriggerType)
	}

	// 时间范围筛选
	if req.StartDate != "" {
		startTime, err := time.Parse("2006-01-02", req.StartDate)
		if err == nil {
			query = query.Where("started_at >= ?", startTime)
		}
	}
	if req.EndDate != "" {
		endTime, err := time.Parse("2006-01-02", req.EndDate)
		if err == nil {
			// 包含当天全天
			endTime = endTime.Add(24 * time.Hour)
			query = query.Where("started_at < ?", endTime)
		}
	}

	// 查询总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (req.Page - 1) * req.PageSize
	if err := query.Order("started_at DESC").
		Limit(req.PageSize).
		Offset(offset).
		Find(&executions).Error; err != nil {
		return nil, 0, err
	}

	// 构建响应（包含任务信息）
	responses := make([]models.GISExecutionResponse, len(executions))
	for i, exec := range executions {
		responses[i] = models.GISExecutionResponse{
			GISExecution: exec,
		}

		// 加载关联任务
		if exec.TaskID != nil {
			var task models.SpatialTask
			if err := r.db.Where("id = ? AND tenant_id = ?", *exec.TaskID, tenantID).First(&task).Error; err == nil {
				responses[i].Task = &task
			}
		}
	}

	return responses, total, nil
}

// ListByTask 获取指定任务的执行记录
func (r *GISExecutionRepository) ListByTask(taskID uint, tenantID uint, limit int) ([]models.GISExecution, error) {
	var executions []models.GISExecution
	if err := r.db.Where("task_id = ? AND tenant_id = ?", taskID, tenantID).
		Order("started_at DESC").
		Limit(limit).
		Find(&executions).Error; err != nil {
		return nil, err
	}
	return executions, nil
}

// GetLatestByTask 获取指定任务的最新执行记录
func (r *GISExecutionRepository) GetLatestByTask(taskID uint, tenantID uint) (*models.GISExecution, error) {
	var execution models.GISExecution
	if err := r.db.Where("task_id = ? AND tenant_id = ?", taskID, tenantID).
		Order("started_at DESC").
		First(&execution).Error; err != nil {
		return nil, err
	}
	return &execution, nil
}

// CountByStatus 统计指定状态的执行数量
func (r *GISExecutionRepository) CountByStatus(tenantID uint, status string) (int64, error) {
	var count int64
	if err := r.db.Model(&models.GISExecution{}).
		Where("tenant_id = ? AND status = ?", tenantID, status).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// DeleteByID 删除执行记录
func (r *GISExecutionRepository) DeleteByID(id uint, tenantID uint) error {
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.GISExecution{}).Error
}
