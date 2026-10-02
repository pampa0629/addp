package service

import (
	"context"
	"fmt"
	"time"

	commonapi "github.com/addp/common/api"
	execution "github.com/addp/common/execution"
	"github.com/addp/common/models"
	orchmodels "github.com/addp/orchestrator/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *ExecutionService) ClaimNext(ctx context.Context, owner string, duration time.Duration) (*execution.TaskExecution, *execution.Lease, error) {
	var item *execution.TaskExecution
	var lease *execution.Lease
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		item, lease, err = execution.ClaimNext(ctx, tx, execution.ClaimOptions{Module: execution.ModuleOrchestrator, TaskType: execution.TaskTypeOrchestration, WorkerID: owner, Now: time.Now().UTC(), LeaseDuration: duration})
		if err != nil || item == nil {
			return err
		}
		return execution.AppendBoundedEvent(ctx, tx, *lease, execution.EventInput{Kind: "started"})
	})
	return item, lease, err
}

func (s *ExecutionService) OwnedExecution(ctx context.Context, lease execution.Lease) (*execution.TaskExecution, error) {
	var item execution.TaskExecution
	err := s.db.WithContext(ctx).Where("module = ? AND task_type = ? AND execution_id = ? AND tenant_id = ? AND status = ? AND execution_boundary = ? AND attempt = ? AND lease_token = ? AND lease_owner = ? AND lease_expires_at >= ?",
		execution.ModuleOrchestrator, execution.TaskTypeOrchestration, lease.ExecutionID, lease.TenantID, execution.ExecutionStatusRunning, execution.ExecutionBoundaryBounded, lease.Attempt, lease.Token, lease.Owner, time.Now().UTC()).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("%w: orchestration lease is no longer current", commonapi.ErrConflict)
	}
	return &item, err
}

func (s *ExecutionService) OwnedPage(ctx context.Context, owner string, after int64, limit int) ([]execution.TaskExecution, error) {
	var items []execution.TaskExecution
	err := s.db.WithContext(ctx).Where("module = ? AND task_type = ? AND status = ? AND lease_owner = ? AND lease_token IS NOT NULL AND lease_expires_at >= ? AND id > ?", execution.ModuleOrchestrator, execution.TaskTypeOrchestration, execution.ExecutionStatusRunning, owner, time.Now().UTC(), after).Order("id ASC").Limit(limit).Find(&items).Error
	return items, err
}

func (s *ExecutionService) UpdateStepResults(ctx context.Context, lease execution.Lease, results orchmodels.StepResults, current string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		bound := *s
		bound.db = tx
		item, err := bound.OwnedExecution(ctx, lease)
		if err != nil {
			return err
		}
		plan, err := readExecutionPlan(item.ExecutionConfig)
		if err != nil {
			return err
		}
		data, err := freezeStepResults(results)
		if err != nil {
			return err
		}
		metadata := item.Metadata
		if metadata == nil {
			metadata = models.JSONMap{}
		}
		metadata["step_results"] = data
		progress := planProgress(plan.Steps, results)
		fields := map[string]interface{}{"metadata": metadata, "current_step": current, "progress": progress}
		if err := execution.UpdateWithLease(ctx, tx, lease, fields); err != nil {
			return err
		}
		return execution.AppendBoundedEvent(ctx, tx, lease, execution.EventInput{Kind: "progress", StepID: execution.SafeEventStepID(current), Counters: map[string]int64{"progress": int64(progress)}})
	})
}

func (s *ExecutionService) FinishExecution(ctx context.Context, lease execution.Lease, status, code string) error {
	item, err := s.OwnedExecution(ctx, lease)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	fields := map[string]interface{}{"current_step": nil}
	if status == execution.ExecutionStatusSuccess {
		fields["progress"] = 100
		fields["error_details"] = nil
	} else {
		fields["error_details"] = executionFailureDetails(code)
	}
	if item.StartedAt != nil {
		fields["execution_time_ms"] = now.Sub(*item.StartedAt).Milliseconds()
	}
	return execution.CompleteWithEvent(ctx, s.db, lease, status, now, fields)
}

// RecoverExpired never requeues a dispatched orchestration. Child execution facts remain intact.
func (s *ExecutionService) RecoverExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	count := 0
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		items, err := execution.FindExpiredForUpdate(ctx, tx, execution.ExpiredOptions{Module: execution.ModuleOrchestrator, TaskType: execution.TaskTypeOrchestration, Now: now, Limit: limit})
		if err != nil {
			return err
		}
		for _, item := range items {
			lease, err := execution.LeaseFromExecution(item)
			if err != nil {
				return err
			}
			code := "orchestrator.execution.lease_expired"
			results, _ := readStepResults(item.Metadata)
			for _, result := range results {
				if result.Phase == "dispatching" {
					code = "orchestrator.execution.dispatch_uncertain"
				}
			}
			fields := map[string]interface{}{"error_details": executionFailureDetails(code), "current_step": nil}
			if item.StartedAt != nil {
				fields["execution_time_ms"] = now.Sub(*item.StartedAt).Milliseconds()
			}
			if err := execution.FailExpiredWithEvent(ctx, tx, lease, now, fields); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

// CreateScheduled atomically consumes a due time and creates its frozen execution.
// System authorization resolution happens before entering this transaction.
func (s *ExecutionService) CreateScheduled(ctx context.Context, expected *orchmodels.Orchestration, now time.Time, next *time.Time, actor ExecutionActor) (*execution.TaskExecution, error) {
	var item *execution.TaskExecution
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var orch orchmodels.Orchestration
		query := tx.Where("id = ? AND tenant_id = ? AND enabled = ? AND schedule = ? AND next_run_at = ? AND next_run_at <= ?", expected.ID, expected.TenantID, true, expected.Schedule, expected.NextRunAt, now)
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if err := query.First(&orch).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil
			}
			return err
		}
		if expected.AuthorizationSubjectID == nil || orch.AuthorizationSubjectID == nil || expected.AuthorizationDefinitionHash == nil || orch.AuthorizationDefinitionHash == nil || *expected.AuthorizationSubjectID != *orch.AuthorizationSubjectID || *expected.AuthorizationDefinitionHash != *orch.AuthorizationDefinitionHash {
			return fmt.Errorf("scheduled orchestration authorization changed")
		}
		var err error
		item, err = createOrchestrationExecution(ctx, tx, &orch, execution.TriggerTypeScheduled, execution.ModuleOrchestrator, nil, actor)
		if err != nil {
			return err
		}
		return tx.Model(&orch).Updates(map[string]interface{}{"next_run_at": next}).Error
	})
	return item, err
}
