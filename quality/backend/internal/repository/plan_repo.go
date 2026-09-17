package repository

import (
	"context"
	"database/sql"
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

type PlanRepository struct{ db *gorm.DB }

func (r *PlanRepository) RuleRepository() *RuleRepository { return NewRuleRepository(r.db) }

func NewPlanRepository(db *gorm.DB) *PlanRepository {
	return &PlanRepository{db: db}
}

func (r *PlanRepository) List(ctx context.Context, tenantID int64, ownerDomainID *int64, page, pageSize int) ([]models.QualityPlan, int64, error) {
	var items []models.QualityPlan
	var total int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		items, total, err = NewPlanRepository(tx).list(ctx, tenantID, ownerDomainID, page, pageSize)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return items, total, commonRepository.WrapDBError(err)
}
func (r *PlanRepository) list(ctx context.Context, tenantID int64, ownerDomainID *int64, page, pageSize int) ([]models.QualityPlan, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	query := r.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	query = filterOwnerDomain(query, ownerDomainID)
	var total int64
	if err := query.Model(&models.QualityPlan{}).Count(&total).Error; err != nil {
		return nil, 0, commonRepository.WrapDBError(err)
	}
	var items []models.QualityPlan
	err := query.Order("updated_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	if err == nil {
		for i := range items {
			if err = loadPlanItems(r.db.WithContext(ctx), &items[i]); err != nil {
				break
			}
		}
	}
	return items, total, commonRepository.WrapDBError(err)
}

func (r *PlanRepository) Get(ctx context.Context, tenantID, id int64) (*models.QualityPlan, error) {
	var task models.QualityPlan
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tenant_id = ? AND id = ?", tenantID, id).First(&task).Error; err != nil {
			return err
		}
		return loadPlanItems(tx, &task)
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, commonRepository.WrapDBError(err)
	}
	return &task, nil
}

func (r *PlanRepository) Create(ctx context.Context, task *models.QualityPlan) error {
	return commonRepository.WrapDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockOwnedDomain(tx, task.TenantID, task.OwnerDomainID); err != nil {
			return err
		}
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		return replacePlanItems(tx, task)
	}))
}

func (r *PlanRepository) Replace(ctx context.Context, task *models.QualityPlan, expectedVersion int64) error {
	return commonRepository.WrapDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.QualityPlan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", task.TenantID, task.ID).First(&current).Error; err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return ErrVersionConflict
		}
		active, err := planActiveExecutionCount(tx, task.ID, task.TenantID)
		if err != nil {
			return err
		}
		if active > 0 {
			return fmt.Errorf("%w: quality plan has an active execution", commonAPI.ErrConflict)
		}
		if err := lockOwnedDomain(tx, task.TenantID, task.OwnerDomainID); err != nil {
			return err
		}
		domainChanged := ownerDomainChanged(current.OwnerDomainID, task.OwnerDomainID)
		result := tx.Model(&current).Where("version = ?", expectedVersion).Updates(map[string]interface{}{
			"name": task.Name, "description": task.Description, "owner_domain_id": task.OwnerDomainID,
			"table_bindings": task.TableBindings,
			"version":        expectedVersion + 1, "updated_by": task.UpdatedBy, "updated_at": task.UpdatedAt,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrVersionConflict
		}
		// Current issues follow the plan; execution snapshots remain immutable.
		if domainChanged {
			if err := tx.Model(&models.Issue{}).Where("tenant_id = ? AND plan_id = ?", task.TenantID, task.ID).
				UpdateColumn("owner_domain_id", task.OwnerDomainID).Error; err != nil {
				return err
			}
		}
		return replacePlanItems(tx, task)
	}))
}

func ownerDomainChanged(before, after *int64) bool {
	if before == nil || after == nil {
		return before != after
	}
	return *before != *after
}

func (r *PlanRepository) Delete(ctx context.Context, tenantID, id, version int64) error {
	return commonRepository.WrapDBError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task models.QualityPlan
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, id).First(&task).Error; err != nil {
			return err
		}
		if task.Version != version {
			return ErrVersionConflict
		}
		active, err := planActiveExecutionCount(tx, id, tenantID)
		if err != nil {
			return err
		}
		if active > 0 {
			return fmt.Errorf("%w: quality plan has an active execution", commonAPI.ErrConflict)
		}
		if err := tx.Where("tenant_id = ? AND plan_id = ?", tenantID, id).Delete(&models.Issue{}).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id=? AND plan_id=?", tenantID, id).Delete(&models.PlanCheckItem{}).Error; err != nil {
			return err
		}
		return tx.Delete(&task).Error
	}))
}

func planActiveExecutionCount(tx *gorm.DB, taskID, tenantID int64) (int64, error) {
	var count int64
	err := tx.Model(&commonExecution.TaskExecution{}).Where(
		"tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ? AND status IN ?",
		tenantID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan, strconv.FormatInt(taskID, 10),
		[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning},
	).Count(&count).Error
	return count, err
}

func (r *PlanRepository) CreateExecution(ctx context.Context, taskID, tenantID int64, execution *commonExecution.TaskExecution, request models.PlanRunRequest) (*models.QualityPlan, error) {
	var task models.QualityPlan
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if execution.ParentExecutionID != nil {
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
			execution.ActorPrincipalID = parent.ActorPrincipalID
			execution.ActorTenantMembershipID = parent.ActorTenantMembershipID
			execution.IssuedAuthorizationVersion = parent.IssuedAuthorizationVersion
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", tenantID, taskID).First(&task).Error; err != nil {
			return err
		}
		bindings, targetKey, err := models.ResolvePlanTargets(task.TableBindings, request)
		if err != nil {
			return fmt.Errorf("%w: %v", commonAPI.ErrBadRequest, err)
		}
		var active int64
		err = tx.Model(&commonExecution.TaskExecution{}).Where("tenant_id = ? AND module = ? AND task_type = ? AND source_task_id = ? AND status IN ? AND execution_config->>'target_key' = ?", tenantID, commonExecution.ModuleQuality, commonExecution.TaskTypeQualityPlan, strconv.FormatInt(taskID, 10), []string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning}, targetKey).Count(&active).Error
		if err != nil {
			return err
		}
		if active > 0 {
			return fmt.Errorf("%w: quality plan already has an active execution", commonAPI.ErrConflict)
		}
		execution.SourceTaskID = commonExecution.NewSourceTaskIDFromInt(int(taskID))
		if err := loadPlanItems(tx, &task); err != nil {
			return err
		}
		execution.SourceTaskName = &task.Name
		if execution.ExecutionConfig == nil {
			return fmt.Errorf("execution config is required")
		}
		execution.ExecutionConfig["task_version"] = task.Version
		execution.ExecutionConfig["owner_domain_id"] = task.OwnerDomainID
		bindingsJSON, err := json.Marshal(bindings)
		if err != nil {
			return err
		}
		execution.ExecutionConfig["table_bindings"] = json.RawMessage(bindingsJSON)
		execution.ExecutionConfig["target_key"] = targetKey
		execution.ExecutionConfig["rules"] = json.RawMessage(task.Rules)
		if err := tx.Create(execution).Error; err != nil {
			return err
		}
		return tx.Model(&task).Updates(map[string]interface{}{
			"last_execution_id": execution.ExecutionID, "last_execution_status": commonExecution.ExecutionStatusPending,
		}).Error
	})
	return &task, commonRepository.WrapDBError(err)
}

func (r *PlanRepository) ClaimPendingExecution(ctx context.Context, workerID string, now time.Time, lease time.Duration) (*commonExecution.TaskExecution, *models.QualityPlan, error) {
	var execution *commonExecution.TaskExecution
	var task models.QualityPlan
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		execution, _, err = commonExecution.ClaimNext(ctx, tx, commonExecution.ClaimOptions{
			Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeQualityPlan, Source: commonExecution.ModuleQuality,
			WorkerID: workerID, Now: now, LeaseDuration: lease, RequireAuthorization: true,
		})
		if err == nil && execution == nil {
			execution, _, err = commonExecution.ClaimNext(ctx, tx, commonExecution.ClaimOptions{Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeQualityPlan, Source: commonExecution.ModuleOrchestrator, WorkerID: workerID, Now: now, LeaseDuration: lease})
		}
		if err != nil || execution == nil {
			return err
		}
		if execution.SourceTaskID == nil {
			return fmt.Errorf("quality plan execution %s has no source_task_id", execution.ExecutionID)
		}
		taskID, err := strconv.ParseInt(*execution.SourceTaskID, 10, 64)
		if err != nil || taskID <= 0 {
			return fmt.Errorf("quality plan execution %s has invalid source_task_id", execution.ExecutionID)
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
		// Another target scope may already be the plan's latest attempt.
		return nil
	})
	if err != nil || execution == nil {
		return nil, nil, commonRepository.WrapDBError(err)
	}
	return execution, &task, nil
}

func (r *PlanRepository) AttachExecutionAuthorization(ctx context.Context, lease commonExecution.Lease, fields map[string]interface{}) error {
	fields["updated_at"] = time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&commonExecution.TaskExecution{}).Where(
		"tenant_id = ? AND execution_id = ? AND status = ? AND attempt = ? AND lease_owner = ? AND lease_token = ? AND lease_expires_at > ? AND execution_authorization_id IS NULL",
		lease.TenantID, lease.ExecutionID, commonExecution.ExecutionStatusRunning, lease.Attempt, lease.Owner, lease.Token, time.Now().UTC(),
	).Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: quality plan cannot attach authorization", commonAPI.ErrConflict)
	}
	return nil
}

func (r *PlanRepository) CompleteExecutionWithLease(ctx context.Context, taskID, tenantID int64, lease commonExecution.Lease, status string, fields map[string]interface{}, completedAt time.Time, observations ...models.IssueObservation) error {
	copied := make(map[string]interface{}, len(fields))
	for key, value := range fields {
		copied[key] = value
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := commonExecution.CompleteWithLease(ctx, tx, lease, status, completedAt, copied); err != nil {
			return err
		}
		if len(observations) > 0 {
			if err := NewIssueRepository(tx).Reconcile(ctx, tenantID, lease.ExecutionID, observations, completedAt); err != nil {
				return err
			}
		}
		result := tx.Model(&models.QualityPlan{}).Where(
			"tenant_id = ? AND id = ? AND last_execution_id = ? AND last_execution_status = ?",
			tenantID, taskID, lease.ExecutionID, commonExecution.ExecutionStatusRunning,
		).Updates(map[string]interface{}{"last_run_at": completedAt, "last_execution_status": status})
		if result.Error != nil {
			return result.Error
		}
		// Lease fencing, not this latest-attempt projection, determines completion.
		return nil
	})
}

func (r *PlanRepository) RenewLease(ctx context.Context, lease commonExecution.Lease, expiresAt time.Time) error {
	return commonExecution.RenewLease(ctx, r.db, lease, expiresAt)
}

func (r *PlanRepository) RecoverExpiredExecutions(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		executions, err := commonExecution.FindExpiredForUpdate(ctx, tx, commonExecution.ExpiredOptions{
			Module: commonExecution.ModuleQuality, TaskType: commonExecution.TaskTypeQualityPlan, Now: now, Limit: 100,
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
				fields := map[string]interface{}{
					"error_details": commonModels.JSONMap{"code": "quality.execution.lease_expired", "message": "quality execution worker lease expired"},
				}
				if execution.StartedAt != nil {
					fields["execution_time_ms"] = now.Sub(*execution.StartedAt).Milliseconds()
				}
				if err := commonExecution.FailExpired(ctx, tx, lease, now, fields); err != nil {
					return err
				}
			} else if err := commonExecution.RetryExpired(ctx, tx, lease, now, "worker lease expired; retry pending"); err != nil {
				return err
			}
			if status == commonExecution.ExecutionStatusPending && execution.Source == commonExecution.ModuleOrchestrator {
				if err := tx.Model(&commonExecution.TaskExecution{}).Where("tenant_id=? AND execution_id=?", execution.TenantID, execution.ExecutionID).Updates(map[string]interface{}{"execution_authorization_id": nil, "authorization_expires_at": nil}).Error; err != nil {
					return err
				}
			}
			if execution.SourceTaskID != nil {
				taskID, parseErr := strconv.ParseInt(*execution.SourceTaskID, 10, 64)
				if parseErr == nil {
					result := tx.Model(&models.QualityPlan{}).Where(
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
