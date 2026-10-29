package repository

import (
	"errors"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// lockContinuousClaim uses the same task -> execution -> lease order as recovery
// and stop finalization. Admission uses persisted ownership, never claim metadata.
func lockContinuousClaim(tx *gorm.DB, claim RuntimeLeaseClaim) (models.TransferTask, commonExecution.TaskExecution, models.RuntimeLease, error) {
	var task models.TransferTask
	var execution commonExecution.TaskExecution
	var lease models.RuntimeLease
	if claim.Task.ID == 0 || claim.Task.TenantID == 0 || claim.Execution.ExecutionID == "" ||
		claim.Lease.TaskID != claim.Task.ID || claim.Lease.ExecutionID != claim.Execution.ExecutionID ||
		claim.Lease.OwnerInstanceID == "" || claim.Lease.FencingToken == 0 {
		return task, execution, lease, ErrRuntimeLeaseLost
	}
	queries := []*gorm.DB{
		tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", claim.Task.ID, claim.Task.TenantID),
		tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("execution_id = ? AND tenant_id = ? AND module = ? AND task_type = ? AND execution_boundary = ? AND source_task_id = ? AND status = ?",
			claim.Execution.ExecutionID, claim.Task.TenantID, commonExecution.ModuleTransfer, commonExecution.TaskTypeSync, commonExecution.ExecutionBoundaryContinuous, fmt.Sprint(claim.Task.ID), commonExecution.ExecutionStatusRunning),
		tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ? AND execution_id = ? AND owner_instance_id = ? AND fencing_token = ?", claim.Task.ID, claim.Execution.ExecutionID, claim.Lease.OwnerInstanceID, claim.Lease.FencingToken),
	}
	for i, dest := range []interface{}{&task, &execution, &lease} {
		if err := queries[i].First(dest).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				err = ErrRuntimeLeaseLost
			}
			return task, execution, lease, err
		}
	}
	return task, execution, lease, nil
}

func continuousEventIdentity(task models.TransferTask, item commonExecution.TaskExecution) bool {
	return task.ID != 0 && task.TenantID != 0 && item.TenantID == int(task.TenantID) &&
		item.Module == commonExecution.ModuleTransfer && item.TaskType == commonExecution.TaskTypeSync &&
		item.ExecutionBoundary == commonExecution.ExecutionBoundaryContinuous &&
		item.SourceTaskID != nil && *item.SourceTaskID == fmt.Sprint(task.ID)
}

// All arguments are read with UPDATE locks in the enclosing owner transaction.
func appendContinuousRuntimeEvent(tx *gorm.DB, task models.TransferTask, execution commonExecution.TaskExecution, lease models.RuntimeLease, input commonExecution.EventInput, requireRunning bool) error {
	return commonExecution.AppendContinuousEvent(tx.Statement.Context, tx, execution.ExecutionID, input, continuousRuntimeAdmission(task, execution, lease, requireRunning))
}

func continuousRuntimeAdmission(task models.TransferTask, execution commonExecution.TaskExecution, lease models.RuntimeLease, requireRunning bool) commonExecution.ContinuousAdmission {
	return func(item commonExecution.TaskExecution, now time.Time) error {
		if !continuousEventIdentity(task, item) || item.ID != execution.ID ||
			lease.TaskID != task.ID || lease.ExecutionID != item.ExecutionID ||
			lease.OwnerInstanceID == "" || lease.FencingToken == 0 || !lease.LeaseUntil.After(now) ||
			(requireRunning && task.DesiredState != models.TaskDesiredStateRunning) {
			return ErrRuntimeLeaseLost
		}
		return nil
	}
}

// Only the owner supervisor / stop transaction may close an unowned execution.
// The task and execution are locked before reading/locking its private lease.
func appendContinuousUnownedEvent(tx *gorm.DB, task models.TransferTask, execution commonExecution.TaskExecution, kind string, stopping bool) error {
	admit, err := continuousUnownedAdmission(tx, task, execution, stopping)
	if err != nil {
		return err
	}
	return commonExecution.AppendContinuousEvent(tx.Statement.Context, tx, execution.ExecutionID, commonExecution.EventInput{Kind: kind, Counters: continuousExecutionCounters(execution)}, admit)
}

func continuousUnownedAdmission(tx *gorm.DB, task models.TransferTask, execution commonExecution.TaskExecution, stopping bool) (commonExecution.ContinuousAdmission, error) {
	var lease models.RuntimeLease
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ?", task.ID).First(&lease).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return func(item commonExecution.TaskExecution, now time.Time) error {
		if !continuousEventIdentity(task, item) || item.ID != execution.ID || (lease.ID != 0 && lease.OwnerInstanceID != "" && lease.LeaseUntil.After(now)) || (stopping && task.DesiredState == models.TaskDesiredStateRunning) || (!stopping && task.DesiredState != models.TaskDesiredStateRunning) {
			return ErrRuntimeLeaseLost
		}
		return nil
	}, nil
}

func completeUnownedContinuous(tx *gorm.DB, task models.TransferTask, execution commonExecution.TaskExecution, status string, fields map[string]interface{}, stopping bool) error {
	admit, err := continuousUnownedAdmission(tx, task, execution, stopping)
	if err != nil {
		return err
	}
	return commonExecution.CompleteContinuous(tx.Statement.Context, tx, execution.ExecutionID, execution.Status, status, fields, admit)
}

func continuousTerminalEventKind(status string) (string, error) {
	switch status {
	case commonExecution.ExecutionStatusSuccess:
		return "completed", nil
	case commonExecution.ExecutionStatusFailed, commonExecution.ExecutionStatusCancelled, commonExecution.ExecutionStatusTimeout:
		return status, nil
	default:
		return "", fmt.Errorf("invalid continuous terminal status %q", status)
	}
}

func continuousExecutionCounters(item commonExecution.TaskExecution) map[string]int64 {
	counters := map[string]int64{}
	if item.RecordsRead != nil {
		counters["records_read"] = *item.RecordsRead
	}
	if item.RecordsWritten != nil {
		counters["records_written"] = *item.RecordsWritten
	}
	return counters
}
