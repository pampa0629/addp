package repository

import (
	"time"

	"github.com/addp/develop/backend/internal/models"
	"gorm.io/gorm"
)

// DevExecutionRepository 执行记录数据访问层
type DevExecutionRepository struct {
	db *gorm.DB
}

// NewDevExecutionRepository 创建执行记录Repository
func NewDevExecutionRepository(db *gorm.DB) *DevExecutionRepository {
	return &DevExecutionRepository{db: db}
}

// Create 创建执行记录
func (r *DevExecutionRepository) Create(execution *models.DevExecution) error {
	return r.db.Create(execution).Error
}

// Update 更新执行记录
func (r *DevExecutionRepository) Update(execution *models.DevExecution) error {
	return r.db.Save(execution).Error
}

// FindByID 根据ID获取执行记录
func (r *DevExecutionRepository) FindByID(id uint, tenantID uint) (*models.DevExecution, error) {
	var execution models.DevExecution
	if err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&execution).Error; err != nil {
		return nil, err
	}
	return &execution, nil
}

// FindByExecutionID 根据ExecutionID获取执行记录
func (r *DevExecutionRepository) FindByExecutionID(executionID string) (*models.DevExecution, error) {
	var execution models.DevExecution
	if err := r.db.Where("execution_id = ?", executionID).First(&execution).Error; err != nil {
		return nil, err
	}
	return &execution, nil
}

// List 查询执行列表（支持多条件筛选和分页）
func (r *DevExecutionRepository) List(req *models.ListExecutionsRequest, tenantID uint) ([]models.ExecutionWithItem, int64, error) {
	var executions []models.DevExecution
	var total int64

	query := r.db.Model(&models.DevExecution{}).Where("tenant_id = ?", tenantID)

	// 开发项ID筛选
	if req.DevItemID != nil {
		query = query.Where("dev_item_id = ?", *req.DevItemID)
	}

	// 类型筛选
	if req.DevType != "" {
		query = query.Where("dev_type = ?", req.DevType)
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

	// 分页查询（使用Preload预加载关联的dev_item，避免N+1查询）
	// 注意：列表查询不包含result和inputs字段，避免传输大量数据（工作流结果可能有几十MB）
	offset := (req.Page - 1) * req.PageSize
	if err := query.Preload("DevItem", "tenant_id = ?", tenantID).
		Select("id", "dev_item_id", "tenant_id", "execution_id", "dev_type", "trigger_type",
			"triggered_by", "status", "progress", "current_step", "error_message",
			"execution_time_ms", "engine_id", "rows_affected", "result_size_bytes",
			"started_at", "completed_at", "created_at").
		Order("created_at DESC").
		Limit(req.PageSize).
		Offset(offset).
		Find(&executions).Error; err != nil {
		return nil, 0, err
	}

	// 构建响应（包含开发项信息）
	responses := make([]models.ExecutionWithItem, len(executions))
	for i, exec := range executions {
		responses[i] = models.ExecutionWithItem{
			DevExecution: exec,
		}

		// 关联的开发项已经通过Preload加载，直接使用
		if exec.DevItemID != nil && exec.DevItem != nil {
			responses[i].DevItem = exec.DevItem
		}
	}

	return responses, total, nil
}

// FindByDevItemID 获取指定开发项的执行记录
func (r *DevExecutionRepository) FindByDevItemID(devItemID uint, tenantID uint, limit int) ([]models.DevExecution, error) {
	var executions []models.DevExecution
	query := r.db.Where("dev_item_id = ? AND tenant_id = ?", devItemID, tenantID).
		Order("created_at DESC")

	if limit > 0 {
		query = query.Limit(limit)
	}

	if err := query.Find(&executions).Error; err != nil {
		return nil, err
	}
	return executions, nil
}

// GetLatestByDevItemID 获取指定开发项的最新执行记录
func (r *DevExecutionRepository) GetLatestByDevItemID(devItemID uint, tenantID uint) (*models.DevExecution, error) {
	var execution models.DevExecution
	if err := r.db.Where("dev_item_id = ? AND tenant_id = ?", devItemID, tenantID).
		Order("created_at DESC").
		First(&execution).Error; err != nil {
		return nil, err
	}
	return &execution, nil
}

// GetStatistics 获取执行统计信息（优化版：单次聚合查询）
func (r *DevExecutionRepository) GetStatistics(tenantID uint, devItemID *uint, startDate, endDate string) (*models.ExecutionStatistics, error) {
	// 构建基础查询条件
	baseQuery := r.db.Model(&models.DevExecution{}).Where("tenant_id = ?", tenantID)

	// 开发项ID过滤
	if devItemID != nil {
		baseQuery = baseQuery.Where("dev_item_id = ?", *devItemID)
	}

	// 时间范围过滤
	if startDate != "" {
		startTime, err := time.Parse("2006-01-02", startDate)
		if err == nil {
			baseQuery = baseQuery.Where("started_at >= ?", startTime)
		}
	}
	if endDate != "" {
		endTime, err := time.Parse("2006-01-02", endDate)
		if err == nil {
			endTime = endTime.Add(24 * time.Hour)
			baseQuery = baseQuery.Where("started_at < ?", endTime)
		}
	}

	stats := &models.ExecutionStatistics{}

	// 使用单次聚合查询获取所有统计数据
	type AggResult struct {
		TotalExecutions   int64
		SuccessCount      int64
		FailedCount       int64
		RunningCount      int64
		AvgExecutionTime  float64
		TotalRowsAffected int64
	}

	var aggResult AggResult
	err := baseQuery.Select(`
		COUNT(*) as total_executions,
		COUNT(CASE WHEN status = 'success' THEN 1 END) as success_count,
		COUNT(CASE WHEN status IN ('failed', 'timeout', 'cancelled') THEN 1 END) as failed_count,
		COUNT(CASE WHEN status IN ('pending', 'running') THEN 1 END) as running_count,
		COALESCE(AVG(CASE WHEN execution_time_ms IS NOT NULL AND status = 'success' THEN execution_time_ms END), 0) as avg_execution_time,
		COALESCE(SUM(CASE WHEN rows_affected IS NOT NULL THEN rows_affected END), 0) as total_rows_affected
	`).Scan(&aggResult).Error

	if err != nil {
		return nil, err
	}

	// 填充统计结果
	stats.TotalExecutions = aggResult.TotalExecutions
	stats.SuccessCount = aggResult.SuccessCount
	stats.FailedCount = aggResult.FailedCount
	stats.RunningCount = aggResult.RunningCount
	stats.AvgExecutionTime = aggResult.AvgExecutionTime
	stats.TotalRowsAffected = aggResult.TotalRowsAffected

	// 计算成功率
	if stats.TotalExecutions > 0 {
		stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.TotalExecutions) * 100
	}

	return stats, nil
}

// CountByStatus 统计指定状态的执行数量
func (r *DevExecutionRepository) CountByStatus(tenantID uint, status string) (int64, error) {
	var count int64
	if err := r.db.Model(&models.DevExecution{}).
		Where("tenant_id = ? AND status = ?", tenantID, status).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// UpdateStatus 更新执行状态
func (r *DevExecutionRepository) UpdateStatus(executionID string, status string, errorMessage string, completedAt *time.Time) error {
	updates := map[string]interface{}{
		"status": status,
	}

	if errorMessage != "" {
		updates["error_message"] = errorMessage
	}

	if completedAt != nil {
		updates["completed_at"] = completedAt
	}

	return r.db.Model(&models.DevExecution{}).
		Where("execution_id = ?", executionID).
		Updates(updates).Error
}

// UpdateProgress 更新执行进度
func (r *DevExecutionRepository) UpdateProgress(executionID string, progress int, currentStep string) error {
	updates := map[string]interface{}{
		"progress": progress,
	}

	if currentStep != "" {
		updates["current_step"] = currentStep
	}

	return r.db.Model(&models.DevExecution{}).
		Where("execution_id = ?", executionID).
		Updates(updates).Error
}

// DeleteOldExecutions 删除旧的执行记录（清理历史数据）
func (r *DevExecutionRepository) DeleteOldExecutions(tenantID uint, beforeDate time.Time) (int64, error) {
	result := r.db.Where("tenant_id = ? AND created_at < ?", tenantID, beforeDate).
		Delete(&models.DevExecution{})

	if result.Error != nil {
		return 0, result.Error
	}

	return result.RowsAffected, nil
}

// FindRunningExecutions 查找所有运行中的执行
func (r *DevExecutionRepository) FindRunningExecutions(tenantID uint) ([]models.DevExecution, error) {
	var executions []models.DevExecution
	if err := r.db.Where("tenant_id = ? AND status IN ?", tenantID, []string{"pending", "running"}).
		Order("created_at ASC").
		Find(&executions).Error; err != nil {
		return nil, err
	}
	return executions, nil
}

// CountByDevItemAndDateRange 统计指定开发项在时间范围内的执行次数
func (r *DevExecutionRepository) CountByDevItemAndDateRange(devItemID uint, tenantID uint, startDate, endDate time.Time) (int64, error) {
	var count int64
	if err := r.db.Model(&models.DevExecution{}).
		Where("dev_item_id = ? AND tenant_id = ? AND started_at >= ? AND started_at < ?",
			devItemID, tenantID, startDate, endDate).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
