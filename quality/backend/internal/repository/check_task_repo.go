package repository

import (
	"context"
	"fmt"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CheckTaskRepository struct {
	db *gorm.DB
}

func NewCheckTaskRepository(db *gorm.DB) *CheckTaskRepository {
	return &CheckTaskRepository{db: db}
}

func (r *CheckTaskRepository) List(tenantID int64) ([]models.CheckTask, error) {
	var items []models.CheckTask
	err := r.db.Where("tenant_id = ?", tenantID).Order("id desc").Find(&items).Error
	return items, err
}

func (r *CheckTaskRepository) Get(id, tenantID int64) (*models.CheckTask, error) {
	var item models.CheckTask
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *CheckTaskRepository) Create(item *models.CheckTask) error {
	return r.db.Create(item).Error
}

func (r *CheckTaskRepository) Update(item *models.CheckTask) error {
	return r.db.Save(item).Error
}

func (r *CheckTaskRepository) Delete(id, tenantID int64) error {
	return r.db.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.CheckTask{}).Error
}

func (r *CheckTaskRepository) ClaimExecution(ctx context.Context, taskID, tenantID int64, execution *commonExecution.TaskExecution) (*models.CheckTask, error) {
	var task models.CheckTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", taskID, tenantID).First(&task).Error; err != nil {
			return err
		}

		sourceTaskID := commonExecution.NewSourceTaskIDFromInt(int(taskID))
		var activeCount int64
		if err := tx.Model(&commonExecution.TaskExecution{}).
			Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ? AND status IN ?",
				tenantID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityCheck, *sourceTaskID,
				[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}).
			Count(&activeCount).Error; err != nil {
			return err
		}
		if activeCount > 0 {
			return fmt.Errorf("%w: quality check task %d already has an active execution", commonAPI.ErrConflict, taskID)
		}

		execution.SourceTaskID = sourceTaskID
		execution.SourceTaskName = &task.Name
		execution.ExecutionConfig = commonModels.JSONMap{
			"engine_id": task.EngineID, "schema_name": task.SchemaName, "table_name": task.Table,
		}
		if err := tx.Create(execution).Error; err != nil {
			return err
		}
		return tx.Model(&task).Updates(map[string]interface{}{
			"last_execution_id": execution.ExecutionID, "last_execution_status": commonExecution.ExecutionStatusPending,
		}).Error
	})
	return &task, err
}

func (r *CheckTaskRepository) StartExecution(ctx context.Context, taskID, tenantID int64, executionID string, startedAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&commonExecution.TaskExecution{}).
			Where("execution_id = ? AND tenant_id = ? AND status = ?", executionID, tenantID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{
				"status": commonExecution.ExecutionStatusRunning, "started_at": startedAt, "updated_at": startedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: quality execution %s is not pending", commonAPI.ErrConflict, executionID)
		}

		result = tx.Model(&models.CheckTask{}).
			Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?",
				taskID, tenantID, executionID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{
				"last_run_at": startedAt, "last_execution_status": commonExecution.ExecutionStatusRunning,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: quality check task %d is not pending for execution %s", commonAPI.ErrConflict, taskID, executionID)
		}
		return nil
	})
}

func (r *CheckTaskRepository) CompleteExecution(ctx context.Context, taskID, tenantID int64, executionID string, executionFields map[string]interface{}, completedAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&commonExecution.TaskExecution{}).
			Where("execution_id = ? AND tenant_id = ? AND status = ?", executionID, tenantID, commonExecution.ExecutionStatusRunning).
			Updates(executionFields)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: quality execution %s is not running", commonAPI.ErrConflict, executionID)
		}
		status, _ := executionFields["status"].(string)
		result = tx.Model(&models.CheckTask{}).
			Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?",
				taskID, tenantID, executionID, commonExecution.ExecutionStatusRunning).
			Updates(map[string]interface{}{
				"last_run_at": completedAt, "last_execution_id": executionID, "last_execution_status": status,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: quality check task %d is not running for execution %s", commonAPI.ErrConflict, taskID, executionID)
		}
		return nil
	})
}
