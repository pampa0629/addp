package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/graph/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrGraphAdmissionClosed = fmt.Errorf("%w: graph admission is closed or effects are unresolved", commonapi.ErrConflict)

func (r *BuildRepository) DB() *gorm.DB { return r.db }

// LockAdmission never creates a missing G on a read/write path. Migration and
// graph creation provision it before the graph becomes usable.
func LockAdmission(tx *gorm.DB, tenantID uint, exclusive bool) error {
	q := tx.Where("tenant_id = ?", tenantID)
	if tx.Dialector.Name() == "postgres" {
		strength := "SHARE"
		if exclusive {
			strength = "UPDATE"
		}
		q = q.Clauses(clause.Locking{Strength: strength})
	}
	return q.First(&models.TenantAdmissionLock{}).Error
}

func lockTask(tx *gorm.DB, taskID, graphID, tenantID uint) (*models.BuildTask, error) {
	var task models.BuildTask
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND graph_id = ? AND tenant_id = ?", taskID, graphID, tenantID).First(&task).Error
	return &task, err
}

func CheckGraphOpen(tx *gorm.DB, graphID, tenantID uint) error {
	var kg models.KnowledgeGraph
	if err := tx.Where("id = ? AND tenant_id = ?", graphID, tenantID).First(&kg).Error; err != nil {
		return err
	}
	if kg.AdmissionClosed || kg.Status == "archived" {
		return ErrGraphAdmissionClosed
	}
	var held int64
	// Every uncertain effect is blocking. An independent in-flight review is
	// blocking until its command settles; running build calls are fenced by E.
	if err := tx.Model(&models.GraphOperation{}).Where("tenant_id = ? AND graph_id = ? AND (state = ? OR (state = ? AND execution_id = ''))", tenantID, graphID, models.GraphOperationUncertain, models.GraphOperationInFlight).Count(&held).Error; err != nil {
		return err
	}
	if held != 0 {
		return ErrGraphAdmissionClosed
	}
	now, err := execution.DatabaseNow(tx)
	if err != nil {
		return err
	}
	var calls []models.GraphOperation
	if err := tx.Where("tenant_id = ? AND graph_id = ? AND state = ? AND execution_id <> ''", tenantID, graphID, models.GraphOperationInFlight).Find(&calls).Error; err != nil {
		return err
	}
	for _, call := range calls {
		var active int64
		if err := tx.Model(&execution.TaskExecution{}).Where("tenant_id = ? AND execution_id = ? AND status = ? AND attempt = ? AND lease_expires_at > ?", tenantID, call.ExecutionID, execution.ExecutionStatusRunning, call.Attempt, now).Count(&active).Error; err != nil {
			return err
		}
		if active != 1 {
			return ErrGraphAdmissionClosed
		}
	}
	return nil
}

// WithBuildLease composes E -> G -> T and leaves the final mutation to Common.
// A caller error or expired lease rolls back every private write in fn.
func (r *BuildRepository) WithBuildLease(ctx context.Context, fn func(*gorm.DB, *models.BuildTask, *execution.TaskExecution) error) error {
	return r.withBuildTransaction(ctx, func(tx *gorm.DB, task *models.BuildTask, e *execution.TaskExecution) error {
		if err := fn(tx, task, e); err != nil {
			return err
		}
		lease, _ := execution.LeaseFromContext(ctx)
		return execution.UpdateWithLease(ctx, tx, lease, map[string]interface{}{"progress": e.Progress})
	})
}

func (r *BuildRepository) withBuildTransaction(ctx context.Context, fn func(*gorm.DB, *models.BuildTask, *execution.TaskExecution) error) error {
	lease, ok := execution.LeaseFromContext(ctx)
	if !ok {
		return fmt.Errorf("%w: graph build lease required", commonapi.ErrConflict)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		e, err := execution.LockActiveLease(ctx, tx, lease)
		if err != nil {
			return err
		}
		if e.Module != execution.ModuleGraph || e.TaskType != execution.TaskTypeKGBuild || e.SourceTaskID == nil {
			return commonapi.ErrConflict
		}
		var control models.BuildExecutionControl
		if err := tx.Where("execution_id = ? AND tenant_id = ?", e.ExecutionID, e.TenantID).First(&control).Error; err != nil {
			return err
		}
		if err := LockAdmission(tx, uint(e.TenantID), false); err != nil {
			return err
		}
		task, err := lockTask(tx, control.TaskID, control.GraphID, control.TenantID)
		if err != nil {
			return err
		}
		if task.ExecutionID != e.ExecutionID || strconv.FormatUint(uint64(task.ID), 10) != *e.SourceTaskID {
			return commonapi.ErrConflict
		}
		return fn(tx, task, e)
	})
}

func (r *BuildRepository) CancelExecution(ctx context.Context, taskID, graphID, tenantID, actor uint, executionID string, attempt int) (*execution.TaskExecution, error) {
	var e execution.TaskExecution
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id = ? AND tenant_id = ? AND module = ? AND task_type = ?", executionID, tenantID, execution.ModuleGraph, execution.TaskTypeKGBuild).First(&e).Error; err != nil {
			return err
		}
		if e.Attempt != attempt {
			return commonapi.ErrConflict
		}
		if err := LockAdmission(tx, tenantID, false); err != nil {
			return err
		}
		task, err := lockTask(tx, taskID, graphID, tenantID)
		if err != nil {
			return err
		}
		if task.ExecutionID != e.ExecutionID {
			return commonapi.ErrConflict
		}
		if e.Status != execution.ExecutionStatusPending && e.Status != execution.ExecutionStatusRunning {
			return nil
		}
		var control models.BuildExecutionControl
		if err := tx.Where("execution_id = ? AND tenant_id = ? AND graph_id = ? AND task_id = ?", e.ExecutionID, tenantID, graphID, taskID).First(&control).Error; err != nil {
			return err
		}
		now, err := execution.DatabaseNow(tx)
		if err != nil {
			return err
		}
		if !control.CancelRequested {
			reason := "user_requested"
			if actor == 0 {
				reason = "cleanup_requested"
			}
			if err := tx.Model(&control).Updates(map[string]interface{}{"cancel_requested": true, "cancel_requested_by": actor, "cancel_requested_at": now, "cancel_reason": reason}).Error; err != nil {
				return err
			}
		}
		if e.Status == execution.ExecutionStatusPending {
			var count int64
			if err := tx.Model(&models.GraphOperation{}).Where("tenant_id = ? AND execution_id = ?", tenantID, e.ExecutionID).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				return commonapi.ErrConflict
			}
			id := execution.PendingIdentity{ExecutionID: e.ExecutionID, TenantID: e.TenantID, Module: e.Module, TaskType: e.TaskType, Source: e.Source}
			if err := execution.CancelUnstarted(ctx, tx, id); err != nil {
				return err
			}
			if err := tx.Model(task).Updates(map[string]interface{}{"status": models.BuildStatusCancelled, "completed_at": now}).Error; err != nil {
				return err
			}
			return tx.Where("execution_id = ? AND tenant_id = ?", e.ExecutionID, tenantID).First(&e).Error
		}
		return nil
	})
	return &e, err
}

func (r *BuildRepository) CancellationRequested(ctx context.Context, executionID string, tenantID int) (bool, error) {
	var c models.BuildExecutionControl
	err := r.db.WithContext(ctx).Where("execution_id = ? AND tenant_id = ?", executionID, tenantID).First(&c).Error
	return c.CancelRequested, err
}

func (r *BuildRepository) PendingBuilds(ctx context.Context, limit int) ([]execution.TaskExecution, error) {
	var rows []execution.TaskExecution
	controls := r.db.Model(&models.BuildExecutionControl{}).Select("execution_id")
	err := r.db.WithContext(ctx).Where("execution_id IN (?)", controls).Where("module = ? AND task_type = ? AND status = ?", execution.ModuleGraph, execution.TaskTypeKGBuild, execution.ExecutionStatusPending).Order("created_at ASC, id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *BuildRepository) ClaimBuild(ctx context.Context, candidate execution.TaskExecution, owner string, duration time.Duration) (*execution.TaskExecution, *execution.Lease, *models.BuildTask, models.BuildExecutionControl, error) {
	var e *execution.TaskExecution
	var lease *execution.Lease
	var task *models.BuildTask
	var control models.BuildExecutionControl
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked execution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id = ? AND tenant_id = ?", candidate.ExecutionID, candidate.TenantID).First(&locked).Error; err != nil {
			return err
		}
		if locked.Status != execution.ExecutionStatusPending || locked.Module != execution.ModuleGraph || locked.TaskType != execution.TaskTypeKGBuild || locked.Attempt != 0 {
			return commonapi.ErrConflict
		}
		if err := LockAdmission(tx, uint(locked.TenantID), false); err != nil {
			return err
		}
		if err := tx.Where("execution_id = ? AND tenant_id = ?", locked.ExecutionID, locked.TenantID).First(&control).Error; err != nil {
			return err
		}
		var err error
		task, err = lockTask(tx, control.TaskID, control.GraphID, control.TenantID)
		if err != nil {
			return err
		}
		if control.CancelRequested || task.ExecutionID != locked.ExecutionID {
			return commonapi.ErrConflict
		}

		id := execution.PendingIdentity{ExecutionID: locked.ExecutionID, TenantID: locked.TenantID, Module: locked.Module, TaskType: locked.TaskType, Source: locked.Source}
		reason := ""
		if _, err := DecodeBuildInput(locked, control, *task); err != nil {
			reason = "graph_input_invalid"
		}
		if err := CheckGraphOpen(tx, task.GraphID, task.TenantID); err != nil {
			if !errors.Is(err, ErrGraphAdmissionClosed) {
				return err
			}
			reason = "graph_admission_closed"
		}
		if reason != "" {
			var calls int64
			if err := tx.Model(&models.GraphOperation{}).Where("execution_id = ? AND tenant_id = ?", locked.ExecutionID, locked.TenantID).Count(&calls).Error; err != nil {
				return err
			}
			if calls != 0 {
				return commonapi.ErrConflict
			}
			if err := execution.RejectUnstarted(ctx, tx, id, map[string]interface{}{"error_details": commonmodels.JSONMap{"code": reason}}); err != nil {
				return err
			}
			var rejected execution.TaskExecution
			if err := tx.Where("execution_id = ? AND tenant_id = ?", locked.ExecutionID, locked.TenantID).First(&rejected).Error; err != nil {
				return err
			}
			return tx.Model(task).Updates(map[string]interface{}{"status": models.BuildStatusFailed, "completed_at": rejected.CompletedAt, "error_message": reason}).Error
		}

		e, lease, err = execution.ClaimPending(ctx, tx, id, execution.ClaimOptions{Module: locked.Module, TaskType: locked.TaskType, Source: locked.Source, WorkerID: owner, LeaseDuration: duration}, nil)
		if err != nil {
			return err
		}
		if err := tx.Model(task).Updates(map[string]interface{}{"status": models.BuildStatusRunning, "started_at": e.StartedAt}).Error; err != nil {
			return err
		}
		task.Status, task.StartedAt = models.BuildStatusRunning, e.StartedAt
		return execution.UpdateWithLease(ctx, tx, *lease, map[string]interface{}{"progress": 0})
	})
	return e, lease, task, control, err
}

// FailExpiredBuilds only fails old attempts. External calls never replay.
func (r *BuildRepository) FailExpiredBuilds(ctx context.Context) error {
	var candidates []execution.TaskExecution
	controls := r.db.Model(&models.BuildExecutionControl{}).Select("execution_id")
	if err := r.db.WithContext(ctx).Where("execution_id IN (?)", controls).Where("module = ? AND task_type = ? AND status = ?", execution.ModuleGraph, execution.TaskTypeKGBuild, execution.ExecutionStatusRunning).Find(&candidates).Error; err != nil {
		return err
	}
	for _, candidate := range candidates {
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var e execution.TaskExecution
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id = ? AND tenant_id = ?", candidate.ExecutionID, candidate.TenantID).First(&e).Error; err != nil {
				return err
			}
			now, err := execution.DatabaseNow(tx)
			if err != nil {
				return err
			}
			if e.Status != execution.ExecutionStatusRunning || e.LeaseExpiresAt == nil || e.LeaseExpiresAt.After(now) {
				return nil
			}
			lease, err := execution.LeaseFromExecution(e)
			if err != nil {
				return err
			}
			if err := LockAdmission(tx, uint(e.TenantID), false); err != nil {
				return err
			}
			var c models.BuildExecutionControl
			if err := tx.Where("execution_id = ? AND tenant_id = ?", e.ExecutionID, e.TenantID).First(&c).Error; err != nil {
				return err
			}
			task, err := lockTask(tx, c.TaskID, c.GraphID, c.TenantID)
			if err != nil {
				return err
			}
			if task.ExecutionID != e.ExecutionID {
				return commonapi.ErrConflict
			}
			if err := tx.Model(&models.GraphOperation{}).Where("tenant_id = ? AND execution_id = ? AND attempt = ? AND state = ?", e.TenantID, e.ExecutionID, e.Attempt, models.GraphOperationInFlight).Update("state", models.GraphOperationUncertain).Error; err != nil {
				return err
			}
			if err := tx.Model(task).Updates(map[string]interface{}{"status": models.BuildStatusFailed, "completed_at": now, "error_message": "graph build lease expired; external effects require reconciliation"}).Error; err != nil {
				return err
			}
			return execution.FailExpired(ctx, tx, lease, map[string]interface{}{"error_details": commonmodels.JSONMap{"code": "graph_build_lease_expired"}})
		})
		if err != nil && !errors.Is(err, commonapi.ErrConflict) {
			return err
		}
	}
	return nil
}

// DecodeBuildInput is shared by claim validation and the supervisor. It never
// reconstructs missing input from live mutable tables.
func DecodeBuildInput(e execution.TaskExecution, c models.BuildExecutionControl, task models.BuildTask) (*models.BuildInput, error) {
	if _, err := execution.DisplayLanguage(&e); err != nil {
		return nil, err
	}
	var input models.BuildInput
	digest := sha256.Sum256([]byte(c.Snapshot))
	if err := json.Unmarshal([]byte(c.Snapshot), &input); err != nil {
		return nil, err
	}
	if input.Schema != models.BuildInputSchema || e.ExecutionConfig["input_schema"] != models.BuildInputSchema || e.ExecutionConfig["input_digest"] != hex.EncodeToString(digest[:]) || input.Task.ID != task.ID || input.Task.ExecutionID != e.ExecutionID || input.TenantID != task.TenantID || input.GraphID != task.GraphID || c.TaskID != task.ID || c.TenantID != task.TenantID || c.GraphID != task.GraphID || c.ExecutionID != e.ExecutionID || e.TenantID != int(task.TenantID) || e.SourceTaskID == nil || *e.SourceTaskID != strconv.FormatUint(uint64(task.ID), 10) || len(input.Materials) == 0 {
		return nil, fmt.Errorf("immutable graph execution input is invalid")
	}
	return &input, nil
}
