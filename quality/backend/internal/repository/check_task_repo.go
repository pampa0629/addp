package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	commonRepository "github.com/addp/common/repository"
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

func (r *CheckTaskRepository) List(tenantID int64, page, pageSize int) ([]models.CheckTask, int64, error) {
	var items []models.CheckTask
	q := r.db.Where("tenant_id = ?", tenantID)
	var total int64
	if err := q.Model(&models.CheckTask{}).Count(&total).Error; err != nil {
		return nil, 0, commonRepository.WrapDBError(err)
	}
	page, pageSize = normalizePage(page, pageSize)
	err := q.Order("updated_at desc, id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, commonRepository.WrapDBError(err)
}

func (r *CheckTaskRepository) Get(id, tenantID int64) (*models.CheckTask, error) {
	var item models.CheckTask
	err := r.db.Where("id = ? AND tenant_id = ?", id, tenantID).First(&item).Error
	if err != nil {
		return nil, commonRepository.WrapDBError(err)
	}
	return &item, nil
}

func (r *CheckTaskRepository) Create(item *models.CheckTask) error {
	return commonRepository.WrapDBError(r.db.Create(item).Error)
}

func (r *CheckTaskRepository) Update(item *models.CheckTask) error {
	return commonRepository.WrapDBError(r.db.Save(item).Error)
}

func (r *CheckTaskRepository) Replace(ctx context.Context, item *models.CheckTask) error {
	return commonRepository.WrapDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.CheckTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", item.ID, item.TenantID).First(&current).Error; err != nil {
			return err
		}
		active, err := qualityTaskActiveExecutionCount(tx, item.ID, item.TenantID)
		if err != nil {
			return err
		}
		if active > 0 {
			return fmt.Errorf("%w: quality check task %d has an active execution", commonAPI.ErrConflict, item.ID)
		}
		return tx.Model(&current).Updates(map[string]interface{}{
			"name": item.Name, "description": item.Description, "engine_id": item.EngineID,
			"schema_name": item.SchemaName, "table_name": item.Table, "updated_by": item.UpdatedBy,
		}).Error
	}))
}

func (r *CheckTaskRepository) Delete(id, tenantID int64) error {
	return commonRepository.WrapDBError(r.db.Transaction(func(tx *gorm.DB) error {
		var task models.CheckTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", id, tenantID).First(&task).Error; err != nil {
			return err
		}
		active, err := qualityTaskActiveExecutionCount(tx, id, tenantID)
		if err != nil {
			return err
		}
		if active > 0 {
			return fmt.Errorf("%w: quality check task %d has an active execution", commonAPI.ErrConflict, id)
		}
		return tx.Delete(&task).Error
	}))
}

func qualityTaskActiveExecutionCount(tx *gorm.DB, taskID, tenantID int64) (int64, error) {
	sourceTaskID := strconv.FormatInt(taskID, 10)
	var count int64
	err := tx.Model(&commonExecution.TaskExecution{}).
		Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ? AND status IN ?", tenantID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityCheck, sourceTaskID, []string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}).
		Count(&count).Error
	return count, err
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
		var ruleApps []models.RuleApplication
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id = ? AND engine_id = ? AND schema_name = ? AND table_name = ?", tenantID, task.EngineID, task.SchemaName, task.Table).
			Order("id ASC").Find(&ruleApps).Error; err != nil {
			return err
		}
		snapshots := make([]map[string]interface{}, 0, len(ruleApps))
		for _, application := range ruleApps {
			snapshots = append(snapshots, map[string]interface{}{
				"id": application.ID, "element_id": application.ElementID, "engine_id": application.EngineID,
				"schema_name": application.SchemaName, "table_name": application.Table,
				"column_name": application.ColumnName, "rule_config": json.RawMessage(application.RuleConfig),
			})
		}
		execution.ExecutionConfig["rule_applications"] = snapshots
		if execution.MaxAttempts <= 0 {
			execution.MaxAttempts = 3
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

// ClaimPendingExecution atomically claims one durable Quality execution. The
// transaction only locks and updates control-plane rows; external calls happen
// after it returns.
func (r *CheckTaskRepository) ClaimPendingExecution(ctx context.Context, workerID string, now time.Time, lease time.Duration) (*commonExecution.TaskExecution, *models.CheckTask, error) {
	var execution commonExecution.TaskExecution
	var task models.CheckTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("module = ? AND task_type = ? AND status = ? AND execution_authorization_id IS NOT NULL", commonExecution.ModuleQuality, commonExecution.TaskTypeQualityCheck, commonExecution.ExecutionStatusPending).
			Order("created_at ASC, id ASC").Limit(1).Find(&execution)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		if execution.SourceTaskID == nil {
			return fmt.Errorf("quality execution %s has no source_task_id", execution.ExecutionID)
		}
		taskID, err := strconv.ParseInt(*execution.SourceTaskID, 10, 64)
		if err != nil || taskID <= 0 {
			return fmt.Errorf("quality execution %s has invalid source_task_id", execution.ExecutionID)
		}
		if err := tx.Where("id = ? AND tenant_id = ?", taskID, execution.TenantID).First(&task).Error; err != nil {
			return err
		}
		leaseOwner := workerID
		leaseExpiresAt := now.Add(lease)
		result := tx.Model(&commonExecution.TaskExecution{}).
			Where("execution_id = ? AND tenant_id = ? AND status = ?", execution.ExecutionID, execution.TenantID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{
				"status": commonExecution.ExecutionStatusRunning, "started_at": now, "updated_at": now,
				"lease_owner": leaseOwner, "lease_expires_at": leaseExpiresAt, "attempt": gorm.Expr("attempt + 1"), "progress": 1,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("quality execution %s was claimed by another worker", execution.ExecutionID)
		}
		return tx.Model(&models.CheckTask{}).
			Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?", task.ID, task.TenantID, execution.ExecutionID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{"last_run_at": now, "last_execution_status": commonExecution.ExecutionStatusRunning}).Error
	})
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	execution.Status = commonExecution.ExecutionStatusRunning
	execution.StartedAt = &now
	execution.Attempt++
	execution.LeaseOwner = &workerID
	leaseExpiresAt := now.Add(lease)
	execution.LeaseExpiresAt = &leaseExpiresAt
	return &execution, &task, nil
}

func (r *CheckTaskRepository) AttachExecutionAuthorization(ctx context.Context, tenantID int64, executionID string, fields map[string]interface{}) error {
	fields["updated_at"] = time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&commonExecution.TaskExecution{}).
		Where("tenant_id = ? AND execution_id = ? AND status = ? AND execution_authorization_id IS NULL", tenantID, executionID, commonExecution.ExecutionStatusPending).
		Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: quality execution %s cannot attach authorization", commonAPI.ErrConflict, executionID)
	}
	return nil
}

func (r *CheckTaskRepository) FailPendingExecution(ctx context.Context, taskID, tenantID int64, executionID, errorCode string, completedAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&commonExecution.TaskExecution{}).
			Where("tenant_id = ? AND execution_id = ? AND status = ?", tenantID, executionID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{
				"status": commonExecution.ExecutionStatusFailed, "completed_at": completedAt, "updated_at": completedAt,
				"error_details": commonModels.JSONMap{"code": errorCode, "message": "quality execution authorization could not be prepared"},
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: quality execution %s is not pending", commonAPI.ErrConflict, executionID)
		}
		return tx.Model(&models.CheckTask{}).
			Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?", taskID, tenantID, executionID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{"last_run_at": completedAt, "last_execution_status": commonExecution.ExecutionStatusFailed}).Error
	})
}

// RecoverExpiredExecutions returns expired leases to pending or marks them
// failed after the configured attempt limit.
func (r *CheckTaskRepository) RecoverExpiredExecutions(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var executions []commonExecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("module = ? AND task_type = ? AND status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at < ?", commonExecution.ModuleQuality, commonExecution.TaskTypeQualityCheck, commonExecution.ExecutionStatusRunning, now).
			Find(&executions).Error; err != nil {
			return err
		}
		for _, execution := range executions {
			status := commonExecution.ExecutionStatusPending
			fields := map[string]interface{}{"status": status, "lease_owner": nil, "lease_expires_at": nil, "updated_at": now}
			if execution.Attempt >= execution.MaxAttempts {
				status = commonExecution.ExecutionStatusFailed
				fields = map[string]interface{}{
					"status": status, "completed_at": now, "updated_at": now,
					"error_details": commonModels.JSONMap{"code": "quality.execution.lease_expired", "message": "quality execution worker lease expired"},
					"lease_owner":   nil, "lease_expires_at": nil,
				}
				if execution.StartedAt != nil {
					fields["execution_time_ms"] = now.Sub(*execution.StartedAt).Milliseconds()
				}
			}
			if err := tx.Model(&commonExecution.TaskExecution{}).Where("execution_id = ? AND status = ?", execution.ExecutionID, commonExecution.ExecutionStatusRunning).Updates(fields).Error; err != nil {
				return err
			}
			if execution.SourceTaskID != nil {
				if taskID, parseErr := strconv.ParseInt(*execution.SourceTaskID, 10, 64); parseErr == nil {
					if err := tx.Model(&models.CheckTask{}).Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?", taskID, execution.TenantID, execution.ExecutionID, commonExecution.ExecutionStatusRunning).
						Updates(map[string]interface{}{"last_execution_status": status}).Error; err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
}

func (r *CheckTaskRepository) CompleteExecutionWithLease(ctx context.Context, taskID, tenantID int64, executionID, workerID string, executionFields map[string]interface{}, completedAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fields := make(map[string]interface{}, len(executionFields)+4)
		for key, value := range executionFields {
			fields[key] = value
		}
		fields["completed_at"] = completedAt
		fields["updated_at"] = completedAt
		fields["lease_owner"] = nil
		fields["lease_expires_at"] = nil
		result := tx.Model(&commonExecution.TaskExecution{}).
			Where("execution_id = ? AND tenant_id = ? AND status = ? AND lease_owner = ?", executionID, tenantID, commonExecution.ExecutionStatusRunning, workerID).
			Updates(fields)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: quality execution %s lease is no longer owned", commonAPI.ErrConflict, executionID)
		}
		status, _ := executionFields["status"].(string)
		result = tx.Model(&models.CheckTask{}).
			Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?", taskID, tenantID, executionID, commonExecution.ExecutionStatusRunning).
			Updates(map[string]interface{}{"last_run_at": completedAt, "last_execution_status": status})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: quality check task %d is not running for execution %s", commonAPI.ErrConflict, taskID, executionID)
		}
		return nil
	})
}

func (r *CheckTaskRepository) RenewLease(ctx context.Context, executionID string, tenantID int64, workerID string, expiresAt time.Time) error {
	result := r.db.WithContext(ctx).Model(&commonExecution.TaskExecution{}).
		Where("execution_id = ? AND tenant_id = ? AND status = ? AND lease_owner = ?", executionID, tenantID, commonExecution.ExecutionStatusRunning, workerID).
		Updates(map[string]interface{}{"lease_expires_at": expiresAt, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: quality execution %s lease is no longer owned", commonAPI.ErrConflict, executionID)
	}
	return nil
}
