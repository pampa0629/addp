package repository

import (
	"context"
	"strconv"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	"github.com/addp/graph/internal/models"
	"gorm.io/gorm"
)

// withIdleTask serializes changes to referenced input and deletion against
// enqueue, active execution, review commands and unresolved external effects.
// It never locks an existing E after G.
func (r *BuildRepository) withIdleTask(ctx context.Context, taskID, graphID, tenantID uint, fn func(*gorm.DB, *models.BuildTask) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := LockAdmission(tx, tenantID, false); err != nil {
			return err
		}
		if err := CheckGraphOpen(tx, graphID, tenantID); err != nil {
			return err
		}
		task, err := lockTask(tx, taskID, graphID, tenantID)
		if err != nil {
			return err
		}
		var count int64
		if err = tx.Model(&execution.TaskExecution{}).Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ? AND status IN ?", tenantID, execution.ModuleGraph, execution.TaskTypeKGBuild, strconv.FormatUint(uint64(taskID), 10), []string{execution.ExecutionStatusPending, execution.ExecutionStatusRunning}).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return commonapi.ErrConflict
		}
		if err = tx.Model(&models.ReviewItem{}).Where("tenant_id = ? AND task_id = ? AND active_command_id <> ''", tenantID, taskID).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return commonapi.ErrConflict
		}
		return fn(tx, task)
	})
}

func (r *BuildRepository) CheckMaterialAdmission(ctx context.Context, taskID, graphID, tenantID uint) error {
	return r.withIdleTask(ctx, taskID, graphID, tenantID, func(*gorm.DB, *models.BuildTask) error { return nil })
}

func (r *BuildRepository) DeleteTask(ctx context.Context, id, graphID, tenantID uint) error {
	return r.withIdleTask(ctx, id, graphID, tenantID, func(tx *gorm.DB, task *models.BuildTask) error {
		for _, model := range []interface{}{&models.ReviewItem{}, &models.BuildMaterial{}} {
			if err := tx.Where("task_id = ? AND graph_id = ? AND tenant_id = ?", id, graphID, tenantID).Delete(model).Error; err != nil {
				return err
			}
		}
		return tx.Delete(task).Error
	})
}

func (r *BuildRepository) CreateMaterial(ctx context.Context, mat *models.BuildMaterial) error {
	return r.withIdleTask(ctx, mat.TaskID, mat.GraphID, mat.TenantID, func(tx *gorm.DB, _ *models.BuildTask) error { return tx.Create(mat).Error })
}

func (r *BuildRepository) DeleteMaterial(ctx context.Context, id, taskID, graphID, tenantID uint) error {
	return r.withIdleTask(ctx, taskID, graphID, tenantID, func(tx *gorm.DB, _ *models.BuildTask) error {
		result := tx.Where("id = ? AND task_id = ? AND graph_id = ? AND tenant_id = ?", id, taskID, graphID, tenantID).Delete(&models.BuildMaterial{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

// ExecutionAttempt is a read projection, never another source of lease facts.
func (r *BuildRepository) fillTaskExecution(task *models.BuildTask) error {
	if task.ExecutionID == "" {
		return nil
	}
	var e execution.TaskExecution
	err := r.db.Where("execution_id = ? AND tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ?", task.ExecutionID, task.TenantID, execution.ModuleGraph, execution.TaskTypeKGBuild, strconv.FormatUint(uint64(task.ID), 10)).First(&e).Error
	if err != nil {
		return err
	}
	task.ExecutionAttempt = &e.Attempt
	task.Status, task.StartedAt, task.CompletedAt = e.Status, e.StartedAt, e.CompletedAt
	return nil
}
