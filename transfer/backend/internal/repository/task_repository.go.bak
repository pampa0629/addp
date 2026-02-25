package repository

import (
	"time"

	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/pkg/pipeline"
	"gorm.io/gorm"
)

// TaskRepository 任务数据访问层
type TaskRepository struct {
	db *gorm.DB
}

// NewTaskRepository 创建任务仓库
func NewTaskRepository(db *gorm.DB) *TaskRepository {
	return &TaskRepository{db: db}
}

// Create 创建任务
func (r *TaskRepository) Create(task *models.Task) error {
	return r.db.Create(task).Error
}

// GetByID 根据 ID 获取任务
func (r *TaskRepository) GetByID(id uint) (*models.Task, error) {
	var task models.Task
	err := r.db.First(&task, id).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// Update 更新任务
func (r *TaskRepository) Update(task *models.Task) error {
	return r.db.Save(task).Error
}

// UpdateStatus 更新任务状态
func (r *TaskRepository) UpdateStatus(id uint, status models.TaskStatus) error {
	return r.db.Model(&models.Task{}).
		Where("id = ?", id).
		Update("status", status).Error
}

// UpdateFields 批量更新任务字段
func (r *TaskRepository) UpdateFields(id uint, fields map[string]interface{}) error {
	return r.db.Model(&models.Task{}).
		Where("id = ?", id).
		Updates(fields).Error
}

// UpdateProgress 更新任务进度
func (r *TaskRepository) UpdateProgress(id uint, progress float64) error {
	return r.db.Model(&models.Task{}).
		Where("id = ?", id).
		Update("progress", progress).Error
}

// Delete 删除任务
func (r *TaskRepository) Delete(id uint) error {
	return r.db.Delete(&models.Task{}, id).Error
}

// List 列出任务
func (r *TaskRepository) List(tenantID uint, filters map[string]interface{}, page, pageSize int) ([]models.Task, int64, error) {
	var tasks []models.Task
	var total int64

	query := r.db.Model(&models.Task{}).Where("tenant_id = ?", tenantID)

	// 应用过滤条件
	if status, ok := filters["status"].(models.TaskStatus); ok {
		query = query.Where("status = ?", status)
	}
	// 支持根据 enabled 状态过滤（用于调度器加载定时任务）
	if enabled, ok := filters["enabled"].(bool); ok {
		query = query.Where("enabled = ?", enabled)
	}
	// 支持筛选有 schedule 的任务
	if hasSchedule, ok := filters["has_schedule"].(bool); ok && hasSchedule {
		query = query.Where("schedule IS NOT NULL AND schedule != ''")
	}

	// 计算总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * pageSize
	err := query.Offset(offset).Limit(pageSize).
		Order("created_at DESC").
		Find(&tasks).Error

	return tasks, total, err
}

// ListByStatus 根据状态列出任务
func (r *TaskRepository) ListByStatus(tenantID uint, status models.TaskStatus) ([]models.Task, error) {
	var tasks []models.Task
	err := r.db.Where("tenant_id = ? AND status = ?", tenantID, status).
		Find(&tasks).Error
	return tasks, err
}

// GetRunningTasks 获取运行中的任务
func (r *TaskRepository) GetRunningTasks(tenantID uint) ([]models.Task, error) {
	return r.ListByStatus(tenantID, models.TaskStatusRunning)
}

// GetTaskWithLastExecution 获取任务及其最后一次执行记录
func (r *TaskRepository) GetTaskWithLastExecution(taskID uint) (*models.Task, *models.TaskExecution, error) {
	var task models.Task
	if err := r.db.First(&task, taskID).Error; err != nil {
		return nil, nil, err
	}

	var lastExecution models.TaskExecution
	err := r.db.Where("task_id = ?", taskID).
		Order("start_time DESC").
		First(&lastExecution).Error

	if err == gorm.ErrRecordNotFound {
		return &task, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	return &task, &lastExecution, nil
}

// GetStatistics 获取任务统计信息
func (r *TaskRepository) GetStatistics(tenantID uint) (*models.TaskStatistics, error) {
	var stats models.TaskStatistics

	// 总任务数
	var totalTasks int64
	if err := r.db.Raw("SELECT COUNT(*) FROM tasks WHERE tenant_id = ?", tenantID).Scan(&totalTasks).Error; err != nil {
		return nil, err
	}
	stats.TotalTasks = totalTasks

	// 执行中的任务数
	var runningTasks int64
	r.db.Raw("SELECT COUNT(*) FROM tasks WHERE tenant_id = ? AND status = ?", tenantID, models.TaskStatusRunning).Scan(&runningTasks)
	stats.RunningTasks = runningTasks

	// 空闲任务数（复用 pending_tasks 字段）
	var pendingTasks int64
	r.db.Raw("SELECT COUNT(*) FROM tasks WHERE tenant_id = ? AND status = ?", tenantID, models.TaskStatusIdle).Scan(&pendingTasks)
	stats.PendingTasks = pendingTasks

	// 定时任务：已启动数量（复用 success_tasks 字段）
	var successTasks int64
	r.db.Raw("SELECT COUNT(*) FROM tasks WHERE tenant_id = ? AND schedule IS NOT NULL AND schedule != '' AND enabled = ?", tenantID, true).Scan(&successTasks)
	stats.SuccessTasks = successTasks

	// 定时任务：未启动数量（复用 failed_tasks 字段）
	var failedTasks int64
	r.db.Raw("SELECT COUNT(*) FROM tasks WHERE tenant_id = ? AND schedule IS NOT NULL AND schedule != '' AND enabled = ?", tenantID, false).Scan(&failedTasks)
	stats.FailedTasks = failedTasks

	// 最后执行状态统计（从 task_executions 表获取）
	// 未执行过的任务数
	var notExecutedTasks int64
	r.db.Raw(`
		SELECT COUNT(*)
		FROM tasks
		WHERE tenant_id = ?
		AND id NOT IN (SELECT DISTINCT task_id FROM task_executions)
	`, tenantID).Scan(&notExecutedTasks)
	stats.NotExecutedTasks = notExecutedTasks

	// 最后执行状态为 running 的任务数
	var runningTaskIDs []uint
	r.db.Raw(`
		SELECT task_id
		FROM (
			SELECT DISTINCT ON (task_id) task_id, status
			FROM task_executions
			WHERE task_id IN (SELECT id FROM tasks WHERE tenant_id = ?)
			ORDER BY task_id, start_time DESC
		) latest_executions
		WHERE status = ?
	`, tenantID, models.ExecutionStatusRunning).Pluck("task_id", &runningTaskIDs)
	stats.LastRunningTasks = int64(len(runningTaskIDs))

	// 最后执行成功的任务数
	var successTaskIDs []uint
	r.db.Raw(`
		SELECT task_id
		FROM (
			SELECT DISTINCT ON (task_id) task_id, status
			FROM task_executions
			WHERE task_id IN (SELECT id FROM tasks WHERE tenant_id = ?)
			ORDER BY task_id, start_time DESC
		) latest_executions
		WHERE status = ?
	`, tenantID, models.ExecutionStatusSuccess).Pluck("task_id", &successTaskIDs)
	stats.LastSuccessTasks = int64(len(successTaskIDs))

	// 最后执行失败的任务数
	var failedTaskIDs []uint
	r.db.Raw(`
		SELECT task_id
		FROM (
			SELECT DISTINCT ON (task_id) task_id, status
			FROM task_executions
			WHERE task_id IN (SELECT id FROM tasks WHERE tenant_id = ?)
			ORDER BY task_id, start_time DESC
		) latest_executions
		WHERE status = ?
	`, tenantID, models.ExecutionStatusFailed).Pluck("task_id", &failedTaskIDs)
	stats.LastFailedTasks = int64(len(failedTaskIDs))

	// 总执行次数
	var totalExecutions int64
	r.db.Raw(`
		SELECT COUNT(*)
		FROM task_executions
		JOIN tasks ON task_executions.task_id = tasks.id
		WHERE tasks.tenant_id = ?
	`, tenantID).Scan(&totalExecutions)
	stats.TotalExecutions = totalExecutions

	// 总处理记录数和字节数
	var result struct {
		TotalRecords int64 `json:"total_records"`
		TotalBytes   int64 `json:"total_bytes"`
	}
	r.db.Raw(`
		SELECT
			COALESCE(SUM(records_written), 0) as total_records,
			COALESCE(SUM(bytes_written), 0) as total_bytes
		FROM task_executions
		JOIN tasks ON task_executions.task_id = tasks.id
		WHERE tasks.tenant_id = ?
	`, tenantID).Scan(&result)
	stats.TotalRecords = result.TotalRecords
	stats.TotalBytes = result.TotalBytes

	return &stats, nil
}

// ExecutionRepository 执行记录数据访问层
type ExecutionRepository struct {
    db *gorm.DB
}

// NewExecutionRepository 创建执行记录仓库
func NewExecutionRepository(db *gorm.DB) *ExecutionRepository {
	return &ExecutionRepository{db: db}
}

// Create 创建执行记录
func (r *ExecutionRepository) Create(execution *models.TaskExecution) error {
	return r.db.Create(execution).Error
}

// GetByID 根据 ID 获取执行记录
func (r *ExecutionRepository) GetByID(id uint) (*models.TaskExecution, error) {
	var execution models.TaskExecution
	err := r.db.First(&execution, id).Error
	if err != nil {
		return nil, err
	}
	return &execution, nil
}

// Update 更新执行记录
func (r *ExecutionRepository) Update(execution *models.TaskExecution) error {
	return r.db.Save(execution).Error
}

// UpdateStatus 更新执行状态
func (r *ExecutionRepository) UpdateStatus(id uint, status models.ExecutionStatus) error {
	return r.db.Model(&models.TaskExecution{}).
		Where("id = ?", id).
		Update("status", status).Error
}

// UpdateMetrics 更新执行指标
func (r *ExecutionRepository) UpdateMetrics(id uint, metrics map[string]interface{}) error {
	return r.db.Model(&models.TaskExecution{}).
		Where("id = ?", id).
		Updates(metrics).Error
}

// FinishExecution 完成执行（设置结束时间和状态）
func (r *ExecutionRepository) FinishExecution(id uint, status models.ExecutionStatus, errorMsg string) error {
	now := time.Now()
	updates := map[string]interface{}{
		"status":   status,
		"end_time": now,
	}
	if errorMsg != "" {
		updates["error_msg"] = errorMsg
	}
	return r.db.Model(&models.TaskExecution{}).
		Where("id = ?", id).
		Updates(updates).Error
}

// AppendLog 追加一行日志到执行记录
// 注意：当前实现依赖 PostgreSQL 的字符串连接运算符 "||"
// 如果后端数据库切换为 MySQL，需要改为使用 CONCAT
func (r *ExecutionRepository) AppendLog(executionID uint, line string) error {
    // 为避免nil，使用 COALESCE
    return r.db.Model(&models.TaskExecution{}).
        Where("id = ?", executionID).
        Update("logs", gorm.Expr("COALESCE(logs, '') || ?", line+"\n")).Error
}

// ListByTaskID 根据任务 ID 列出执行记录
func (r *ExecutionRepository) ListByTaskID(taskID uint, page, pageSize int) ([]models.TaskExecution, int64, error) {
	var executions []models.TaskExecution
	var total int64

	query := r.db.Model(&models.TaskExecution{}).Where("task_id = ?", taskID)

	// 计算总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询
	offset := (page - 1) * pageSize
	err := query.Offset(offset).Limit(pageSize).
		Order("start_time DESC").
		Find(&executions).Error

	return executions, total, err
}

// GetLatestByTaskID 获取任务的最新执行记录
func (r *ExecutionRepository) GetLatestByTaskID(taskID uint) (*models.TaskExecution, error) {
	var execution models.TaskExecution
	err := r.db.Where("task_id = ?", taskID).
		Order("start_time DESC").
		First(&execution).Error
	if err != nil {
		return nil, err
	}
	return &execution, nil
}

// GetRunningExecutions 获取运行中的执行记录
func (r *ExecutionRepository) GetRunningExecutions() ([]models.TaskExecution, error) {
	var executions []models.TaskExecution
	err := r.db.Where("status = ?", models.ExecutionStatusRunning).
		Find(&executions).Error
	return executions, err
}

// ListAllExecutions 列出租户的所有执行记录（跨任务）
func (r *ExecutionRepository) ListAllExecutions(tenantID uint, filters map[string]interface{}, page, pageSize int) ([]models.TaskExecution, int64, error) {
	var executions []models.TaskExecution
	var total int64

	query := r.db.Model(&models.TaskExecution{}).
		Joins("JOIN tasks ON task_executions.task_id = tasks.id").
		Where("tasks.tenant_id = ?", tenantID)

	// 应用过滤条件
	if status, ok := filters["status"].(string); ok && status != "" {
		query = query.Where("task_executions.status = ?", status)
	}
	if taskID, ok := filters["task_id"].(uint); ok && taskID > 0 {
		query = query.Where("task_executions.task_id = ?", taskID)
	}

	// 计算总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 分页查询，选择 task_executions 的所有字段
	offset := (page - 1) * pageSize
	err := query.Select("task_executions.*").
		Offset(offset).
		Limit(pageSize).
		Order("task_executions.start_time DESC").
		Find(&executions).Error

	return executions, total, err
}

// MappingRepository 字段映射数据访问层
type MappingRepository struct {
	db *gorm.DB
}

// NewMappingRepository 创建字段映射仓库
func NewMappingRepository(db *gorm.DB) *MappingRepository {
	return &MappingRepository{db: db}
}

// CreateBatch 批量创建字段映射
func (r *MappingRepository) CreateBatch(mappings []models.FieldMapping) error {
	return r.db.Create(&mappings).Error
}

// GetByTaskID 根据任务 ID 获取字段映射
func (r *MappingRepository) GetByTaskID(taskID uint) ([]models.FieldMapping, error) {
	var mappings []models.FieldMapping
	err := r.db.Where("task_id = ?", taskID).
		Order("id ASC").
		Find(&mappings).Error
	return mappings, err
}

// DeleteByTaskID 删除任务的所有字段映射
func (r *MappingRepository) DeleteByTaskID(taskID uint) error {
	return r.db.Where("task_id = ?", taskID).
		Delete(&models.FieldMapping{}).Error
}

// UpdateBatch 更新任务的字段映射（先删除再创建）
func (r *MappingRepository) UpdateBatch(taskID uint, mappings []models.FieldMapping) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// 删除旧映射
		if err := tx.Where("task_id = ?", taskID).Delete(&models.FieldMapping{}).Error; err != nil {
			return err
		}

		// 创建新映射
		if len(mappings) > 0 {
			for i := range mappings {
				mappings[i].TaskID = taskID
			}
			if err := tx.Create(&mappings).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

// CheckpointRepository Checkpoint 数据访问层
type CheckpointRepository struct {
	db *gorm.DB
}

// NewCheckpointRepository 创建 Checkpoint 仓库
func NewCheckpointRepository(db *gorm.DB) *CheckpointRepository {
	return &CheckpointRepository{db: db}
}

// Save 保存 Checkpoint
func (r *CheckpointRepository) Save(checkpoint *pipeline.Checkpoint) error {
	return r.db.Save(checkpoint).Error
}

// GetLatest 获取最新的 Checkpoint
func (r *CheckpointRepository) GetLatest(taskID, executionID uint) (*pipeline.Checkpoint, error) {
	var checkpoint pipeline.Checkpoint
	err := r.db.Where("task_id = ? AND execution_id = ?", taskID, executionID).
		Order("created_at DESC").
		First(&checkpoint).Error
	if err != nil {
		return nil, err
	}
	return &checkpoint, nil
}

// GetByPartition 获取指定分区的最新 Checkpoint
func (r *CheckpointRepository) GetByPartition(taskID, executionID uint, partitionID string) (*pipeline.Checkpoint, error) {
	var checkpoint pipeline.Checkpoint
	err := r.db.Where("task_id = ? AND execution_id = ? AND partition_id = ?", taskID, executionID, partitionID).
		Order("created_at DESC").
		First(&checkpoint).Error
	if err != nil {
		return nil, err
	}
	return &checkpoint, nil
}

// DeleteByExecution 删除执行的所有 Checkpoint
func (r *CheckpointRepository) DeleteByExecution(executionID uint) error {
	return r.db.Where("execution_id = ?", executionID).
		Delete(&pipeline.Checkpoint{}).Error
}
