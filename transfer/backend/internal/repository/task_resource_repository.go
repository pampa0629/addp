package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrTaskDeletionRuntimeActive = errors.New("transfer task runtime became active during deletion")

type TaskDefinitionDeleteMode string

const (
	TaskDefinitionDeleteSoft     TaskDefinitionDeleteMode = "soft"
	TaskDefinitionDeletePhysical TaskDefinitionDeleteMode = "physical"
)

type TaskPrivateStateDeleteStats struct {
	DeadLetters          int64
	SyncStates           int64
	RuntimeLeases        int64
	SchemaChangeRequests int64
	CaptureResources     int64
	CancelledExecutions  int64
}

type TaskResourceRepository struct {
	db *gorm.DB
}

func NewTaskResourceRepository(db *gorm.DB) *TaskResourceRepository {
	return &TaskResourceRepository{db: db}
}

// DeleteTaskAndPrivateState 锁定 task 行，在同一事务终结 active execution、删除私有状态和任务定义。
func (r *TaskResourceRepository) DeleteTaskAndPrivateState(
	ctx context.Context,
	tenantID, taskID uint,
	continuous bool,
	mode TaskDefinitionDeleteMode,
	now time.Time,
) (TaskPrivateStateDeleteStats, error) {
	var stats TaskPrivateStateDeleteStats
	if r == nil || r.db == nil {
		return stats, fmt.Errorf("task resource repository database is not configured")
	}
	if tenantID == 0 || taskID == 0 || now.IsZero() {
		return stats, fmt.Errorf("task resource deletion requires tenant, task, and deletion time")
	}
	if mode != TaskDefinitionDeleteSoft && mode != TaskDefinitionDeletePhysical {
		return stats, fmt.Errorf("unsupported task definition delete mode %q", mode)
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.TransferTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", taskID, tenantID).First(&task).Error; err != nil {
			return err
		}
		if task.DesiredState != models.TaskDesiredStateStopped || (!continuous && task.Status == models.TaskStatusRunning) {
			return ErrTaskDeletionRuntimeActive
		}
		var activeLeases int64
		if err := tx.Model(&models.RuntimeLease{}).
			Where("task_id = ? AND owner_instance_id <> '' AND lease_until > ?", taskID, now).
			Count(&activeLeases).Error; err != nil {
			return err
		}
		if activeLeases > 0 {
			return ErrTaskDeletionRuntimeActive
		}
		cancelled, err := cancelTaskExecutionsForCleanup(tx, tenantID, taskID, now)
		if err != nil {
			return err
		}
		stats.CancelledExecutions = cancelled
		deletes := []struct {
			model interface{}
			where string
			args  []interface{}
			count *int64
		}{
			{model: &models.DeadLetter{}, where: "tenant_id = ? AND task_id = ?", args: []interface{}{tenantID, taskID}, count: &stats.DeadLetters},
			{model: &models.SyncState{}, where: "task_id = ?", args: []interface{}{taskID}, count: &stats.SyncStates},
			{model: &models.RuntimeLease{}, where: "task_id = ?", args: []interface{}{taskID}, count: &stats.RuntimeLeases},
			{model: &models.SchemaChangeRequest{}, where: "tenant_id = ? AND task_id = ?", args: []interface{}{tenantID, taskID}, count: &stats.SchemaChangeRequests},
			{model: &models.CaptureResource{}, where: "tenant_id = ? AND task_id = ?", args: []interface{}{tenantID, taskID}, count: &stats.CaptureResources},
		}
		for _, deletion := range deletes {
			result := tx.Where(deletion.where, deletion.args...).Delete(deletion.model)
			if result.Error != nil {
				return result.Error
			}
			*deletion.count = result.RowsAffected
		}
		if mode == TaskDefinitionDeletePhysical {
			return tx.Unscoped().Delete(&task).Error
		}
		return tx.Delete(&task).Error
	})
	return stats, err
}

func cancelTaskExecutionsForCleanup(tx *gorm.DB, tenantID, taskID uint, now time.Time) (int64, error) {
	var executions []commonExecution.TaskExecution
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ? AND status IN ?",
			int(tenantID), commonExecution.ModuleTransfer, commonExecution.TaskTypeSync, fmt.Sprint(taskID),
			[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}).
		Find(&executions).Error; err != nil {
		return 0, err
	}
	var cancelled int64
	for _, execution := range executions {
		metadata := execution.Metadata
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		metadata["stop_reason"] = "cleanup"
		updates := map[string]interface{}{
			"status": commonExecution.ExecutionStatusCancelled, "metadata": metadata,
			"completed_at": now, "updated_at": now,
		}
		if execution.StartedAt != nil {
			updates["execution_time_ms"] = now.Sub(*execution.StartedAt).Milliseconds()
		}
		result := tx.Model(&execution).Where("status IN ?", []string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}).Updates(updates)
		if result.Error != nil {
			return cancelled, result.Error
		}
		cancelled += result.RowsAffected
	}
	return cancelled, nil
}
