package repository

import (
	"context"
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

type MaterializationGateRepository struct{ db *gorm.DB }

func NewMaterializationGateRepository(db *gorm.DB) *MaterializationGateRepository {
	return &MaterializationGateRepository{db: db}
}

func (r *MaterializationGateRepository) List(ctx context.Context, tenantID int64, page, pageSize int) ([]models.MaterializationGateTask, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	query := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	var total int64
	if err := query.Model(&models.MaterializationGateTask{}).Count(&total).Error; err != nil {
		return nil, 0, commonRepository.WrapDBError(err)
	}
	var items []models.MaterializationGateTask
	err := query.Order("updated_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, commonRepository.WrapDBError(err)
}

func (r *MaterializationGateRepository) Get(ctx context.Context, tenantID, id int64) (*models.MaterializationGateTask, error) {
	var task models.MaterializationGateTask
	err := r.db.WithContext(ctx).Where("tenant_id = ? AND id = ?", tenantID, id).First(&task).Error
	if err != nil {
		return nil, commonRepository.WrapDBError(err)
	}
	return &task, nil
}

func (r *MaterializationGateRepository) Create(ctx context.Context, task *models.MaterializationGateTask) error {
	return commonRepository.WrapDBError(r.db.WithContext(ctx).Create(task).Error)
}

func (r *MaterializationGateRepository) Replace(ctx context.Context, task *models.MaterializationGateTask, expectedVersion int64) error {
	return commonRepository.WrapDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.MaterializationGateTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", task.TenantID, task.ID).First(&current).Error; err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return fmt.Errorf("%w: materialization gate version changed", commonAPI.ErrConflict)
		}
		active, err := gateActiveExecutionCount(tx, task.ID, task.TenantID)
		if err != nil {
			return err
		}
		if active > 0 {
			return fmt.Errorf("%w: materialization gate has an active execution", commonAPI.ErrConflict)
		}
		result := tx.Model(&current).Where("version = ?", expectedVersion).Updates(map[string]interface{}{
			"name": task.Name, "description": task.Description,
			"materialization_group_id":      task.MaterializationGroupID,
			"materialization_group_version": task.MaterializationGroupVersion,
			"table_bindings":                task.TableBindings, "assertions": task.Assertions,
			"version": expectedVersion + 1, "updated_by": task.UpdatedBy, "updated_at": task.UpdatedAt,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: materialization gate version changed", commonAPI.ErrConflict)
		}
		return nil
	}))
}

func (r *MaterializationGateRepository) Delete(ctx context.Context, tenantID, id, version int64) error {
	return commonRepository.WrapDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.MaterializationGateTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, id).First(&task).Error; err != nil {
			return err
		}
		if task.Version != version {
			return fmt.Errorf("%w: materialization gate version changed", commonAPI.ErrConflict)
		}
		active, err := gateActiveExecutionCount(tx, id, tenantID)
		if err != nil {
			return err
		}
		if active > 0 {
			return fmt.Errorf("%w: materialization gate has an active execution", commonAPI.ErrConflict)
		}
		return tx.Delete(&task).Error
	}))
}

func gateActiveExecutionCount(tx *gorm.DB, taskID, tenantID int64) (int64, error) {
	var count int64
	err := tx.Model(&commonExecution.TaskExecution{}).Where(
		"tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ? AND status IN ?",
		tenantID, commonExecution.ModuleQuality, commonExecution.TaskTypeMaterializationGate, strconv.FormatInt(taskID, 10),
		[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning},
	).Count(&count).Error
	return count, err
}

func (r *MaterializationGateRepository) CreateExecution(ctx context.Context, taskID, tenantID int64, execution *commonExecution.TaskExecution) (*models.MaterializationGateTask, error) {
	var task models.MaterializationGateTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if execution.ParentExecutionID == nil {
			return fmt.Errorf("%w: materialization gate requires parent execution", commonAPI.ErrConflict)
		}
		var parent commonExecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("tenant_id = ? AND execution_id = ? AND module = ? AND status = ?",
				tenantID, *execution.ParentExecutionID, commonExecution.ModuleOrchestrator, commonExecution.ExecutionStatusRunning).
			First(&parent).Error; err != nil {
			return err
		}
		if parent.ActorPrincipalID == nil || parent.ActorTenantMembershipID == nil || parent.IssuedAuthorizationVersion == nil {
			return fmt.Errorf("%w: orchestration parent has no authorization lineage", commonAPI.ErrConflict)
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, taskID).First(&task).Error; err != nil {
			return err
		}
		active, err := gateActiveExecutionCount(tx, taskID, tenantID)
		if err != nil {
			return err
		}
		if active > 0 {
			return fmt.Errorf("%w: materialization gate already has an active execution", commonAPI.ErrConflict)
		}
		execution.SourceTaskID = commonExecution.NewSourceTaskIDFromInt(int(taskID))
		execution.SourceTaskName = &task.Name
		execution.ActorPrincipalID = parent.ActorPrincipalID
		execution.ActorTenantMembershipID = parent.ActorTenantMembershipID
		execution.IssuedAuthorizationVersion = parent.IssuedAuthorizationVersion
		if err := tx.Create(execution).Error; err != nil {
			return err
		}
		return tx.Model(&task).Updates(map[string]interface{}{
			"last_execution_id": execution.ExecutionID, "last_execution_status": commonExecution.ExecutionStatusPending,
		}).Error
	})
	return &task, commonRepository.WrapDBError(err)
}

func (r *MaterializationGateRepository) ClaimPendingExecution(ctx context.Context, workerID string, now time.Time, lease time.Duration) (*commonExecution.TaskExecution, *models.MaterializationGateTask, error) {
	var execution *commonExecution.TaskExecution
	var task models.MaterializationGateTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		execution, _, err = commonExecution.ClaimNext(ctx, tx, commonExecution.ClaimOptions{
			Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeMaterializationGate,
			WorkerID: workerID, Now: now, LeaseDuration: lease, RequireAuthorization: false,
		})
		if err != nil || execution == nil {
			return err
		}
		if execution.SourceTaskID == nil {
			return fmt.Errorf("quality gate execution %s has no source_task_id", execution.ExecutionID)
		}
		taskID, err := strconv.ParseInt(*execution.SourceTaskID, 10, 64)
		if err != nil || taskID <= 0 {
			return fmt.Errorf("quality gate execution %s has invalid source_task_id", execution.ExecutionID)
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", execution.TenantID, taskID).First(&task).Error; err != nil {
			return err
		}
		result := tx.Model(&task).Where("last_execution_id = ? AND last_execution_status = ?", execution.ExecutionID, commonExecution.ExecutionStatusPending).Updates(map[string]interface{}{
			"last_run_at": now, "last_execution_status": commonExecution.ExecutionStatusRunning,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: materialization gate summary changed", commonAPI.ErrConflict)
		}
		return nil
	})
	if err != nil || execution == nil {
		return nil, nil, commonRepository.WrapDBError(err)
	}
	return execution, &task, nil
}

func (r *MaterializationGateRepository) AttachExecutionAuthorization(ctx context.Context, lease commonExecution.Lease, fields map[string]interface{}) error {
	fields["updated_at"] = time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&commonExecution.TaskExecution{}).Where(
		"tenant_id = ? AND execution_id = ? AND status = ? AND attempt = ? AND lease_owner = ? AND lease_token = ? AND execution_authorization_id IS NULL",
		lease.TenantID, lease.ExecutionID, commonExecution.ExecutionStatusRunning, lease.Attempt, lease.Owner, lease.Token,
	).Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: materialization gate cannot attach authorization", commonAPI.ErrConflict)
	}
	return nil
}

func (r *MaterializationGateRepository) CompleteExecutionWithLease(ctx context.Context, taskID, tenantID int64, lease commonExecution.Lease, status string, fields map[string]interface{}, completedAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := commonExecution.CompleteWithLease(ctx, tx, lease, status, completedAt, fields); err != nil {
			return err
		}
		result := tx.Model(&models.MaterializationGateTask{}).Where(
			"tenant_id = ? AND id = ? AND last_execution_id = ? AND last_execution_status = ?",
			tenantID, taskID, lease.ExecutionID, commonExecution.ExecutionStatusRunning,
		).Updates(map[string]interface{}{"last_run_at": completedAt, "last_execution_status": status})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: materialization gate is not running", commonAPI.ErrConflict)
		}
		return nil
	})
}

func (r *MaterializationGateRepository) RenewLease(ctx context.Context, lease commonExecution.Lease, expiresAt time.Time) error {
	return commonExecution.RenewLease(ctx, r.db, lease, expiresAt)
}

func (r *MaterializationGateRepository) RecoverExpiredExecutions(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		executions, err := commonExecution.FindExpiredForUpdate(ctx, tx, commonExecution.ExpiredOptions{
			Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeMaterializationGate, Now: now, Limit: 100,
		})
		if err != nil {
			return err
		}
		for _, execution := range executions {
			lease, err := commonExecution.LeaseFromExecution(execution)
			if err != nil {
				return err
			}
			status := commonExecution.ExecutionStatusPending
			if execution.Attempt >= execution.MaxAttempts {
				status = commonExecution.ExecutionStatusFailed
				if err := commonExecution.FailExpired(ctx, tx, lease, now, map[string]interface{}{
					"error_details": commonModels.JSONMap{"code": "quality.execution.lease_expired", "message": "quality execution worker lease expired"},
				}); err != nil {
					return err
				}
			} else if err := commonExecution.RetryExpired(ctx, tx, lease, now, "worker lease expired; retry pending"); err != nil {
				return err
			}
			if execution.SourceTaskID != nil {
				taskID, parseErr := strconv.ParseInt(*execution.SourceTaskID, 10, 64)
				if parseErr == nil {
					result := tx.Model(&models.MaterializationGateTask{}).Where(
						"tenant_id = ? AND id = ? AND last_execution_id = ? AND last_execution_status = ?",
						execution.TenantID, taskID, execution.ExecutionID, commonExecution.ExecutionStatusRunning,
					).Updates(map[string]interface{}{"last_execution_status": status})
					if result.Error != nil {
						return result.Error
					}
				}
			}
		}
		return nil
	})
}
