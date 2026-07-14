package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	commonrepo "github.com/addp/common/repository"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrTaskAlreadyRunning = errors.New("transfer task is already running")
var ErrContinuousTaskAlreadyRunning = errors.New("continuous transfer task is already running")
var ErrContinuousTaskBlocked = errors.New("continuous transfer task is blocked")

// TaskRepository 任务数据访问层
type TaskRepository struct {
	db *gorm.DB
}

// NewTaskRepository 创建任务仓库
func NewTaskRepository(db *gorm.DB) *TaskRepository {
	return &TaskRepository{db: db}
}

// Create 创建任务
func (r *TaskRepository) Create(task *models.TransferTask) error {
	return r.db.Create(task).Error
}

// ClaimExecution atomically claims one idle task and creates its pending execution.
// For incremental tasks it also advances the source-state fencing token.
func (r *TaskRepository) ClaimExecution(ctx context.Context, taskID, tenantID uint, execution *commonExecution.TaskExecution, sourceIdentity string) (*models.TransferTask, *models.SyncState, error) {
	var claimedTask models.TransferTask
	var claimedState *models.SyncState
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", taskID, tenantID).
			First(&claimedTask).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if claimedTask.Status == models.TaskStatusRunning {
			return ErrTaskAlreadyRunning
		}
		if strings.TrimSpace(sourceIdentity) != "" {
			initial := models.SyncState{
				TaskID: taskID, SourceIdentity: sourceIdentity, Partition: "default",
				PositionType: "watermark", PositionVersion: "v1",
			}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
				return err
			}
			var state models.SyncState
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("task_id = ? AND source_identity = ? AND partition = ?", taskID, sourceIdentity, "default").
				First(&state).Error; err != nil {
				return err
			}
			state.FencingToken++
			if err := tx.Model(&state).Update("fencing_token", state.FencingToken).Error; err != nil {
				return err
			}
			if execution.Metadata == nil {
				execution.Metadata = commonModels.JSONMap{}
			}
			execution.Metadata["sync_state_id"] = state.ID
			execution.Metadata["sync_state_version"] = state.StateVersion
			execution.Metadata["fencing_token"] = state.FencingToken
			claimedState = &state
		}
		if err := tx.Create(execution).Error; err != nil {
			return err
		}
		if err := tx.Model(&claimedTask).Updates(map[string]interface{}{
			"status":                models.TaskStatusRunning,
			"progress":              0,
			"last_execution_id":     execution.ExecutionID,
			"last_execution_status": commonExecution.ExecutionStatusPending,
		}).Error; err != nil {
			return err
		}
		claimedTask.Status = models.TaskStatusRunning
		claimedTask.Progress = 0
		return nil
	})
	return &claimedTask, claimedState, err
}

// StartContinuousExecution 原子写入用户期望状态与 pending execution。
func (r *TaskRepository) StartContinuousExecution(ctx context.Context, taskID, tenantID uint, execution *commonExecution.TaskExecution) (*models.TransferTask, error) {
	var task models.TransferTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", taskID, tenantID).First(&task).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		if task.Status == models.TaskStatusBlocked {
			return ErrContinuousTaskBlocked
		}
		if task.DesiredState == models.TaskDesiredStateRunning || task.Status == models.TaskStatusRunning {
			return ErrContinuousTaskAlreadyRunning
		}
		if err := tx.Create(execution).Error; err != nil {
			return err
		}
		if err := tx.Model(&task).Updates(map[string]interface{}{
			"desired_state":         models.TaskDesiredStateRunning,
			"status":                models.TaskStatusIdle,
			"progress":              0,
			"last_execution_id":     execution.ExecutionID,
			"last_execution_status": commonExecution.ExecutionStatusPending,
		}).Error; err != nil {
			return err
		}
		task.DesiredState = models.TaskDesiredStateRunning
		task.LastExecutionID = &execution.ExecutionID
		status := commonExecution.ExecutionStatusPending
		task.LastExecutionStatus = &status
		return nil
	})
	return &task, err
}

// SetContinuousDesiredState 原子改变 continuous task 的期望状态。
// 尚未被 supervisor claim 的 pending execution 会在同一事务内取消。
func (r *TaskRepository) SetContinuousDesiredState(ctx context.Context, taskID, tenantID uint, desired models.TaskDesiredState, stopReason string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.TransferTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", taskID, tenantID).First(&task).Error; err != nil {
			return commonrepo.WrapDBError(err)
		}
		updates := map[string]interface{}{"desired_state": desired}
		if desired == models.TaskDesiredStateStopped && task.Status == models.TaskStatusBlocked {
			updates["status"] = models.TaskStatusIdle
		}
		if desired != models.TaskDesiredStateRunning && task.LastExecutionID != nil {
			var execution commonExecution.TaskExecution
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("execution_id = ? AND tenant_id = ?", *task.LastExecutionID, tenantID).
				First(&execution).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err == nil && execution.Status == commonExecution.ExecutionStatusPending {
				now := time.Now()
				metadata := execution.Metadata
				if metadata == nil {
					metadata = commonModels.JSONMap{}
				}
				metadata["stop_reason"] = stopReason
				if err := tx.Model(&execution).Updates(map[string]interface{}{
					"status": commonExecution.ExecutionStatusCancelled, "metadata": metadata,
					"completed_at": now, "updated_at": now,
				}).Error; err != nil {
					return err
				}
				updates["status"] = models.TaskStatusIdle
				updates["last_execution_status"] = commonExecution.ExecutionStatusCancelled
			}
		}
		return tx.Model(&task).Updates(updates).Error
	})
}

// WaitContinuousRuntimeStopped 等待 active owner 释放或 lease 过期。
// capture cleanup 必须在此之后删除 CDC topic，避免仍在运行的 worker 读取已删除资源。
func (r *TaskRepository) WaitContinuousRuntimeStopped(ctx context.Context, taskID uint, timeout, pollInterval time.Duration) error {
	if timeout <= 0 || pollInterval <= 0 {
		return fmt.Errorf("continuous runtime stop wait requires positive timeout and poll interval")
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		var active int64
		if err := r.db.WithContext(ctx).Model(&models.RuntimeLease{}).
			Where("task_id = ? AND owner_instance_id <> '' AND lease_until > ?", taskID, time.Now()).Count(&active).Error; err != nil {
			return err
		}
		if active == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for continuous runtime of task %d to stop", taskID)
		case <-ticker.C:
		}
	}
}

// GetByID 根据 ID 获取任务
func (r *TaskRepository) GetByID(id uint) (*models.TransferTask, error) {
	var task models.TransferTask
	err := r.db.First(&task, id).Error
	if err != nil {
		return nil, commonrepo.WrapDBError(err)
	}
	return &task, nil
}

// Update 更新任务
func (r *TaskRepository) Update(task *models.TransferTask) error {
	return r.db.Save(task).Error
}

// UpdateStatus 更新任务状态
func (r *TaskRepository) UpdateStatus(id uint, status models.TaskStatus) error {
	return r.db.Model(&models.TransferTask{}).
		Where("id = ?", id).
		Update("status", status).Error
}

// UpdateFields 批量更新任务字段
func (r *TaskRepository) UpdateFields(id uint, fields map[string]interface{}) error {
	return r.db.Model(&models.TransferTask{}).
		Where("id = ?", id).
		Updates(fields).Error
}

// UpdateProgress 更新任务进度
func (r *TaskRepository) UpdateProgress(id uint, progress float64) error {
	return r.db.Model(&models.TransferTask{}).
		Where("id = ?", id).
		Update("progress", progress).Error
}

// Delete 删除任务
func (r *TaskRepository) Delete(id uint) error {
	return r.db.Delete(&models.TransferTask{}, id).Error
}

// List 列出任务
func (r *TaskRepository) List(tenantID uint, filters map[string]interface{}, page, pageSize int) ([]models.TransferTask, int64, error) {
	var tasks []models.TransferTask
	var total int64

	query := r.db.Model(&models.TransferTask{}).Where("tenant_id = ?", tenantID)

	// 应用过滤条件
	if status, ok := filters["status"].(models.TaskStatus); ok {
		query = query.Where("status = ?", status)
	}
	if taskType, ok := filters["task_type"].(string); ok && taskType != "" {
		query = query.Where("task_type = ?", taskType)
	}
	if boundary, ok := filters["runtime_boundary"].(string); ok && boundary != "" {
		if r.db.Dialector.Name() == "sqlite" {
			query = query.Where("json_extract(config, '$.runtime.boundary') = ?", boundary)
		} else {
			query = query.Where("config -> 'runtime' ->> 'boundary' = ?", boundary)
		}
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
func (r *TaskRepository) ListByStatus(tenantID uint, status models.TaskStatus) ([]models.TransferTask, error) {
	var tasks []models.TransferTask
	err := r.db.Where("tenant_id = ? AND status = ?", tenantID, status).
		Find(&tasks).Error
	return tasks, err
}

// GetRunningTasks 获取运行中的任务
func (r *TaskRepository) GetRunningTasks(tenantID uint) ([]models.TransferTask, error) {
	return r.ListByStatus(tenantID, models.TaskStatusRunning)
}

func (r *TaskRepository) ListScheduledTasksMissingNextRun(ctx context.Context) ([]models.TransferTask, error) {
	var tasks []models.TransferTask
	err := r.db.WithContext(ctx).
		Where("enabled = ? AND schedule <> '' AND next_run_at IS NULL", true).
		Find(&tasks).Error
	return tasks, err
}

func (r *TaskRepository) UpdateNextRunAt(ctx context.Context, id uint, nextRunAt *time.Time) error {
	return r.db.WithContext(ctx).
		Model(&models.TransferTask{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{"next_run_at": nextRunAt}).Error
}

func (r *TaskRepository) ListDueScheduledTaskIDs(ctx context.Context, now time.Time, limit int) ([]uint, error) {
	if limit <= 0 {
		limit = 100
	}
	var taskIDs []uint
	err := r.db.WithContext(ctx).
		Model(&models.TransferTask{}).
		Where("enabled = ? AND schedule <> '' AND next_run_at IS NOT NULL AND next_run_at <= ? AND status <> ?", true, now, models.TaskStatusRunning).
		Order("next_run_at ASC").
		Limit(limit).
		Pluck("id", &taskIDs).Error
	return taskIDs, err
}

func (r *TaskRepository) ClaimDueScheduledExecution(ctx context.Context, taskID uint, schedule string, now time.Time, nextRunAt *time.Time, execution *commonExecution.TaskExecution, sourceIdentity string) (*models.TransferTask, *models.SyncState, error) {
	var claimed *models.TransferTask
	var claimedState *models.SyncState
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.TransferTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("id = ? AND enabled = ? AND schedule = ? AND next_run_at IS NOT NULL AND next_run_at <= ? AND status <> ?", taskID, true, schedule, now, models.TaskStatusRunning).
			First(&task).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		if strings.TrimSpace(sourceIdentity) != "" {
			initial := models.SyncState{TaskID: task.ID, SourceIdentity: sourceIdentity, Partition: "default", PositionType: "watermark", PositionVersion: "v1"}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
				return err
			}
			var state models.SyncState
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ? AND source_identity = ? AND partition = ?", task.ID, sourceIdentity, "default").First(&state).Error; err != nil {
				return err
			}
			state.FencingToken++
			if err := tx.Model(&state).Update("fencing_token", state.FencingToken).Error; err != nil {
				return err
			}
			if execution.Metadata == nil {
				execution.Metadata = commonModels.JSONMap{}
			}
			execution.Metadata["sync_state_id"] = state.ID
			execution.Metadata["sync_state_version"] = state.StateVersion
			execution.Metadata["fencing_token"] = state.FencingToken
			claimedState = &state
		}
		if err := tx.Create(execution).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.TransferTask{}).
			Where("id = ?", task.ID).
			Updates(map[string]interface{}{
				"next_run_at":           nextRunAt,
				"status":                models.TaskStatusRunning,
				"progress":              0,
				"last_execution_id":     execution.ExecutionID,
				"last_execution_status": commonExecution.ExecutionStatusPending,
			}).Error; err != nil {
			return err
		}

		claimed = &task
		return nil
	})
	return claimed, claimedState, err
}

// GetTaskWithLastExecution 获取任务及其最后一次执行记录
func (r *TaskRepository) GetTaskWithLastExecution(taskID uint) (*models.TransferTask, *models.TaskExecution, error) {
	var task models.TransferTask
	if err := r.db.First(&task, taskID).Error; err != nil {
		return nil, nil, commonrepo.WrapDBError(err)
	}

	var lastExecution models.TaskExecution
	err := r.db.Where("task_id = ?", taskID).
		Order("start_time DESC").
		First(&lastExecution).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
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
	if err := r.db.Raw("SELECT COUNT(*) FROM transfer.transfer_tasks WHERE tenant_id = ? AND deleted_at IS NULL", tenantID).Scan(&totalTasks).Error; err != nil {
		return nil, err
	}
	stats.TotalTasks = totalTasks

	// 执行中的任务数
	var runningTasks int64
	r.db.Raw("SELECT COUNT(*) FROM transfer.transfer_tasks WHERE tenant_id = ? AND deleted_at IS NULL AND status = ?", tenantID, models.TaskStatusRunning).Scan(&runningTasks)
	stats.RunningTasks = runningTasks

	// 空闲任务数（复用 pending_tasks 字段）
	var pendingTasks int64
	r.db.Raw("SELECT COUNT(*) FROM transfer.transfer_tasks WHERE tenant_id = ? AND deleted_at IS NULL AND status = ?", tenantID, models.TaskStatusIdle).Scan(&pendingTasks)
	stats.PendingTasks = pendingTasks

	// 定时任务：已启动数量（复用 success_tasks 字段）
	var successTasks int64
	r.db.Raw("SELECT COUNT(*) FROM transfer.transfer_tasks WHERE tenant_id = ? AND deleted_at IS NULL AND schedule IS NOT NULL AND schedule != '' AND enabled = ?", tenantID, true).Scan(&successTasks)
	stats.SuccessTasks = successTasks

	// 定时任务：未启动数量（复用 failed_tasks 字段）
	var failedTasks int64
	r.db.Raw("SELECT COUNT(*) FROM transfer.transfer_tasks WHERE tenant_id = ? AND deleted_at IS NULL AND schedule IS NOT NULL AND schedule != '' AND enabled = ?", tenantID, false).Scan(&failedTasks)
	stats.FailedTasks = failedTasks

	if err := r.fillExecutionStatistics(tenantID, &stats); err != nil {
		return nil, err
	}

	return &stats, nil
}

func (r *TaskRepository) fillExecutionStatistics(tenantID uint, stats *models.TaskStatistics) error {
	var taskIDs []string
	if err := r.db.Model(&models.TransferTask{}).
		Where("tenant_id = ? AND deleted_at IS NULL", tenantID).
		Select("CAST(id AS TEXT)").
		Pluck("CAST(id AS TEXT)", &taskIDs).Error; err != nil {
		return err
	}
	if len(taskIDs) == 0 {
		stats.NotExecutedTasks = 0
		return nil
	}

	type executionStatRow struct {
		SourceTaskID   string
		Status         string
		ID             int64
		RecordsWritten *int64
		BytesWritten   *int64
	}

	var executions []executionStatRow
	if err := r.db.Table("common.task_executions").
		Select("id, source_task_id, status, started_at, records_written, bytes_written").
		Where("module = ? AND task_type = ? AND source_task_id IN ?", commonExecution.ModuleTransfer, commonExecution.TaskTypeSync, taskIDs).
		Order("source_task_id ASC, started_at DESC, id DESC").
		Find(&executions).Error; err != nil {
		return err
	}

	taskIDSet := make(map[string]struct{}, len(taskIDs))
	for _, taskID := range taskIDs {
		taskIDSet[taskID] = struct{}{}
	}

	latestStatusByTask := make(map[string]string)
	for _, execution := range executions {
		if execution.SourceTaskID == "" {
			continue
		}
		stats.TotalExecutions++
		if execution.RecordsWritten != nil {
			stats.TotalRecords += *execution.RecordsWritten
		}
		if execution.BytesWritten != nil {
			stats.TotalBytes += *execution.BytesWritten
		}
		if _, ok := latestStatusByTask[execution.SourceTaskID]; !ok {
			latestStatusByTask[execution.SourceTaskID] = execution.Status
		}
	}

	stats.NotExecutedTasks = int64(len(taskIDSet) - len(latestStatusByTask))
	for _, status := range latestStatusByTask {
		switch status {
		case string(models.ExecutionStatusRunning):
			stats.LastRunningTasks++
		case string(models.ExecutionStatusSuccess):
			stats.LastSuccessTasks++
		case string(models.ExecutionStatusFailed):
			stats.LastFailedTasks++
		}
	}

	if stats.NotExecutedTasks < 0 {
		return fmt.Errorf("invalid transfer execution statistics: not_executed=%d", stats.NotExecutedTasks)
	}
	return nil
}
