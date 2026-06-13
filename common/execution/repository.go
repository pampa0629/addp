package execution

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/models"
	"gorm.io/gorm"
)

// TaskExecutionRepository 统一执行记录仓库
type TaskExecutionRepository struct {
	db *gorm.DB
}

// NewTaskExecutionRepository 创建执行记录仓库
func NewTaskExecutionRepository(db *gorm.DB) *TaskExecutionRepository {
	return &TaskExecutionRepository{db: db}
}

// Create 创建执行记录
func (r *TaskExecutionRepository) Create(ctx context.Context, exec *TaskExecution) error {
	return r.db.WithContext(ctx).Create(exec).Error
}

// Update 更新执行记录
func (r *TaskExecutionRepository) Update(ctx context.Context, exec *TaskExecution) error {
	return r.db.WithContext(ctx).Save(exec).Error
}

// UpdateFields 部分更新字段
func (r *TaskExecutionRepository) UpdateFields(ctx context.Context, executionID string, tenantID int, fields map[string]interface{}) error {
	result := r.db.WithContext(ctx).
		Model(&TaskExecution{}).
		Where("execution_id = ? AND tenant_id = ?", executionID, tenantID).
		Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: execution_id=%s tenant_id=%d", commonapi.ErrNotFound, executionID, tenantID)
	}
	return nil
}

// GetByID 根据 ID 查询
func (r *TaskExecutionRepository) GetByID(ctx context.Context, id int64, tenantID int) (*TaskExecution, error) {
	var exec TaskExecution
	query := r.db.WithContext(ctx).Where("id = ?", id)

	// 如果 tenantID > 0，则进行租户过滤；否则不过滤（用于内部调用）
	if tenantID > 0 {
		query = query.Where("tenant_id = ?", tenantID)
	}

	err := query.First(&exec).Error
	return &exec, wrapDBError(err)
}

// GetByExecutionID 根据 UUID 查询
func (r *TaskExecutionRepository) GetByExecutionID(ctx context.Context, executionID string, tenantID int) (*TaskExecution, error) {
	var exec TaskExecution
	query := r.db.WithContext(ctx).Where("execution_id = ?", executionID)
	if tenantID != 0 {
		query = query.Where("tenant_id = ?", tenantID)
	}
	err := query.First(&exec).Error
	return &exec, wrapDBError(err)
}

// ListChildrenByParentExecutionID 根据父执行 UUID 查询直接子执行
func (r *TaskExecutionRepository) ListChildrenByParentExecutionID(ctx context.Context, parentExecutionID string, tenantID int) ([]*TaskExecution, error) {
	var executions []*TaskExecution
	query := r.db.WithContext(ctx).
		Where("parent_execution_id = ?", parentExecutionID)
	if tenantID > 0 {
		query = query.Where("tenant_id = ?", tenantID)
	}
	err := query.
		Order("created_at ASC, id ASC").
		Find(&executions).Error
	return executions, err
}

// List 分页查询（支持多种过滤条件）
func (r *TaskExecutionRepository) List(ctx context.Context, filter TaskExecutionFilter) ([]*TaskExecution, int64, error) {
	query := r.buildFilterQuery(ctx, filter)

	// 计算总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	var executions []*TaskExecution
	err := query.
		Order("created_at DESC").
		Offset((filter.Page - 1) * filter.PageSize).
		Limit(filter.PageSize).
		Find(&executions).Error

	return executions, total, err
}

// GetStatistics 获取执行统计信息
func (r *TaskExecutionRepository) GetStatistics(ctx context.Context, filter TaskExecutionFilter) (*ExecutionStatistics, error) {
	stats := &ExecutionStatistics{}

	// 总数和状态分布
	var statusCounts []struct {
		Status string
		Count  int64
	}
	err := r.buildFilterQuery(ctx, filter).Select("status, COUNT(*) as count").
		Group("status").
		Scan(&statusCounts).Error
	if err != nil {
		return nil, err
	}

	for _, sc := range statusCounts {
		stats.Total += sc.Count
		switch sc.Status {
		case ExecutionStatusSuccess:
			stats.SuccessCount = sc.Count
		case ExecutionStatusFailed:
			stats.FailedCount = sc.Count
		case ExecutionStatusRunning:
			stats.RunningCount = sc.Count
		case ExecutionStatusPending:
			stats.PendingCount = sc.Count
		}
	}

	// 平均执行时间
	var avgTime sql.NullFloat64
	err = r.buildFilterQuery(ctx, filter).Where("execution_time_ms IS NOT NULL").
		Select("AVG(execution_time_ms)").
		Scan(&avgTime).Error
	if err != nil {
		return nil, err
	}
	if avgTime.Valid {
		stats.AvgExecutionTimeMs = avgTime.Float64
	}

	// 成功率
	if stats.Total > 0 {
		stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.Total) * 100
	}

	return stats, nil
}

func (r *TaskExecutionRepository) buildFilterQuery(ctx context.Context, filter TaskExecutionFilter) *gorm.DB {
	query := r.db.WithContext(ctx).Model(&TaskExecution{})

	// 租户过滤（必须）
	query = query.Where("tenant_id = ?", filter.TenantID)

	if filter.Module != "" {
		query = query.Where("module = ?", filter.Module)
	}
	if filter.TaskType != "" {
		query = query.Where("task_type = ?", filter.TaskType)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.TriggerType != "" {
		query = query.Where("trigger_type = ?", filter.TriggerType)
	}
	if filter.Source != "" {
		query = query.Where("source = ?", filter.Source)
	}
	if filter.SourceTaskID != nil {
		query = query.Where("source_task_id = ?", *filter.SourceTaskID)
	}
	if filter.StartDate != nil {
		query = query.Where("created_at >= ?", *filter.StartDate)
	}
	if filter.EndDate != nil {
		query = query.Where("created_at <= ?", *filter.EndDate)
	}

	return query
}

// GetTrendData 获取趋势数据（按天聚合）
func (r *TaskExecutionRepository) GetTrendData(ctx context.Context, tenantID int, module string, days int) ([]TrendDataPoint, error) {
	query := `
		SELECT
			DATE(created_at) as date,
			COUNT(*) as total,
			SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) as success_count,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed_count,
			AVG(CASE WHEN execution_time_ms IS NOT NULL THEN execution_time_ms ELSE NULL END) as avg_time_ms
		FROM common.task_executions
		WHERE tenant_id = ?
		  AND created_at >= NOW() - INTERVAL '1 day' * ?
		  AND (? = '' OR module = ?)
		GROUP BY DATE(created_at)
		ORDER BY date ASC
	`

	var results []TrendDataPoint
	err := r.db.WithContext(ctx).Raw(query, tenantID, days, module, module).Scan(&results).Error
	return results, err
}

// GetRunningExecutions 获取所有正在运行的执行
func (r *TaskExecutionRepository) GetRunningExecutions(ctx context.Context, tenantID int) ([]*TaskExecution, error) {
	var executions []*TaskExecution
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND status IN (?, ?)", tenantID, ExecutionStatusPending, ExecutionStatusRunning).
		Order("created_at DESC").
		Find(&executions).Error
	return executions, err
}

// CleanOrphanExecutions 清理孤儿执行记录（超时未完成的）
func (r *TaskExecutionRepository) CleanOrphanExecutions(ctx context.Context, timeout time.Duration) (int64, error) {
	result := r.db.WithContext(ctx).
		Model(&TaskExecution{}).
		Where("status IN (?, ?) AND created_at < ?",
			ExecutionStatusPending,
			ExecutionStatusRunning,
			time.Now().Add(-timeout)).
		Updates(map[string]interface{}{
			"status": ExecutionStatusTimeout,
			"error_details": models.JSONMap{
				"message":    "Execution timed out due to worker crash or long-running task",
				"cleaned_at": time.Now(),
			},
		})
	return result.RowsAffected, result.Error
}

// DeleteOldRecords 删除旧记录（数据清理）
func (r *TaskExecutionRepository) DeleteOldRecords(ctx context.Context, tenantID int, beforeDate time.Time) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("tenant_id = ? AND created_at < ?", tenantID, beforeDate).
		Delete(&TaskExecution{})
	return result.RowsAffected, result.Error
}

// UpdateStatus 原子更新状态（防止并发冲突）
func (r *TaskExecutionRepository) UpdateStatus(ctx context.Context, executionID string, tenantID int, fromStatus, toStatus string) error {
	result := r.db.WithContext(ctx).
		Model(&TaskExecution{}).
		Where("execution_id = ? AND tenant_id = ? AND status = ?", executionID, tenantID, fromStatus).
		Update("status", toStatus)

	if result.RowsAffected == 0 {
		return fmt.Errorf("status update failed: current status is not %s", fromStatus)
	}
	return result.Error
}

// TaskExecutionFilter 查询过滤器
type TaskExecutionFilter struct {
	TenantID     int
	Module       string
	TaskType     string
	Status       string
	TriggerType  string
	Source       string
	SourceTaskID *string
	StartDate    *time.Time
	EndDate      *time.Time
	Page         int
	PageSize     int
}

// ExecutionStatistics 执行统计
type ExecutionStatistics struct {
	Total              int64   `json:"total"`
	SuccessCount       int64   `json:"success_count"`
	FailedCount        int64   `json:"failed_count"`
	RunningCount       int64   `json:"running_count"`
	PendingCount       int64   `json:"pending_count"`
	SuccessRate        float64 `json:"success_rate"`
	AvgExecutionTimeMs float64 `json:"avg_execution_time_ms"`
}

// TrendDataPoint 趋势数据点
type TrendDataPoint struct {
	Date         time.Time `json:"date"`
	Total        int64     `json:"total"`
	SuccessCount int64     `json:"success_count"`
	FailedCount  int64     `json:"failed_count"`
	AvgTimeMs    float64   `json:"avg_time_ms"`
}

func wrapDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return commonapi.ErrNotFound
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return commonapi.ErrConflict
	}
	return err
}
