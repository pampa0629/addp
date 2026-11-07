package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *TaskRepository) BindFileGDBExportAuthorization(ctx context.Context, tenant uint, id string, issued *commonClient.IssuedFileGDBExportAuthorization) error {
	if issued == nil || !issued.Matches(tenant, id, &issued.SourceReadScope) {
		return commonAPI.ErrForbidden
	}
	identity := commonExecution.PendingIdentity{
		ExecutionID: id, TenantID: int(tenant), Module: commonExecution.ModuleTransfer, TaskType: commonExecution.TaskTypeSync, Source: commonExecution.ModuleManager,
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row struct {
			Config json.RawMessage `gorm:"column:frozen_config"`
			commonExecution.TaskExecution
		}
		query := tx.Model(&commonExecution.TaskExecution{}).Select("*, execution_config AS frozen_config").
			Where("execution_id = ? AND tenant_id = ? AND module = ? AND task_type = ? AND source = ?", id, tenant, identity.Module, identity.TaskType, identity.Source).
			Where("execution_boundary = ? AND trigger_type = ? AND source_task_id IS NULL AND parent_execution_id IS NULL AND max_attempts = 1", commonExecution.ExecutionBoundaryBounded, commonExecution.TriggerTypeManual)
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Take(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return commonAPI.ErrNotFound
			}
			return err
		}
		scope, err := commonExecution.NewFileGDBExportReadScope(row.Config)
		if err != nil || !issued.Matches(tenant, id, scope) || row.Metadata[models.ExportProtectionMetadataKey] == nil ||
			row.ActorPrincipalID == nil || row.ActorTenantMembershipID == nil || row.IssuedAuthorizationVersion == nil ||
			row.TriggeredBy == nil || int64(*row.TriggeredBy) != *row.ActorPrincipalID {
			return commonAPI.ErrForbidden
		}
		authorizationID, _ := strconv.ParseInt(issued.ID, 10, 64)
		if row.ExecutionAuthorizationID != nil && *row.ExecutionAuthorizationID == authorizationID &&
			row.AuthorizationExpiresAt != nil && row.AuthorizationExpiresAt.Equal(issued.ExpiresAt) {
			return nil
		}
		return commonExecution.AttachPendingAuthorization(ctx, tx, identity, commonExecution.PendingAuthorization{
			ID: authorizationID, ExpiresAt: issued.ExpiresAt, ActorPrincipalID: *row.ActorPrincipalID,
			ActorTenantMembershipID: *row.ActorTenantMembershipID, IssuedAuthorizationVersion: *row.IssuedAuthorizationVersion,
		})
	})
}

func (r *TaskRepository) ClaimNextBoundedExecution(ctx context.Context, workerID string, leaseDuration time.Duration) (*commonExecution.TaskExecution, *commonExecution.Lease, error) {
	var execution *commonExecution.TaskExecution
	var lease *commonExecution.Lease
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		// The Manager freezes a FileGDB execution before System can issue its
		// source authorization. Leave it pending until the owner binds the reference.
		eligible := tx.Where(`(
			source <> ? OR source_task_id IS NOT NULL OR execution_config->'filegdb_source' IS NULL
			OR (execution_authorization_id IS NOT NULL AND authorization_expires_at IS NOT NULL)
		)`, commonExecution.ModuleManager)
		execution, lease, err = commonExecution.ClaimNext(ctx, eligible, commonExecution.ClaimOptions{
			Module: commonExecution.ModuleTransfer, TaskType: commonExecution.TaskTypeSync,
			WorkerID: workerID, LeaseDuration: leaseDuration,
		})
		if err != nil || execution == nil {
			return err
		}
		if err := commonExecution.AppendBoundedEvent(ctx, tx, *lease, commonExecution.EventInput{Kind: "started"}); err != nil {
			return err
		}
		if execution.SourceTaskID == nil {
			return nil
		}
		taskID, err := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
		if err != nil {
			return fmt.Errorf("transfer execution %s has invalid source task: %w", execution.ExecutionID, err)
		}
		var task models.TransferTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND tenant_id = ?", taskID, execution.TenantID).First(&task).Error; err != nil {
			return err
		}
		if isReplayExecution(execution) {
			return commonExecution.UpdateWithLease(ctx, tx, *lease, nil)
		}
		result := tx.Model(&task).
			Where("last_execution_id = ? AND last_execution_status = ?", execution.ExecutionID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{
				"status": models.TaskStatusRunning, "last_run_at": execution.StartedAt,
				"last_execution_status": commonExecution.ExecutionStatusRunning,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("transfer task %d summary no longer matches execution %s", task.ID, execution.ExecutionID)
		}
		return commonExecution.UpdateWithLease(ctx, tx, *lease, nil)
	})
	if err != nil {
		return nil, nil, err
	}
	if execution == nil {
		return nil, nil, nil
	}
	return execution, lease, nil
}

// FailExpiredBoundedExecutions fails closed for lost attempts and abandoned
// FileGDB authorization preparations. A fresh orchestration execution must
// bind a fresh target instead of replaying a lease with possible partial writes.
func (r *TaskRepository) FailExpiredBoundedExecutions(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	count := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		items, err := commonExecution.FindExpiredForUpdate(ctx, tx, commonExecution.ExpiredOptions{
			Module: commonExecution.ModuleTransfer, TaskType: commonExecution.TaskTypeSync, Limit: limit,
		})
		if err != nil {
			return err
		}
		for i := range items {
			item := items[i]
			lease, err := commonExecution.LeaseFromExecution(item)
			if err != nil {
				return err
			}
			fields := map[string]interface{}{
				"error_details": commonModels.JSONMap{
					"code":    "transfer.execution.lease_expired_recovery_required",
					"message": "transfer bounded execution lease expired; inspect the target state before retrying",
				},
			}
			if err := commonExecution.FailExpired(ctx, tx, lease, fields); err != nil {
				return err
			}
			if item.SourceTaskID != nil && !isReplayExecution(&item) {
				taskID, parseErr := strconv.ParseUint(*item.SourceTaskID, 10, 64)
				if parseErr != nil {
					return parseErr
				}
				result := tx.Model(&models.TransferTask{}).
					Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?", taskID, item.TenantID, item.ExecutionID, commonExecution.ExecutionStatusRunning).
					Updates(map[string]interface{}{
						"status": models.TaskStatusIdle, "progress": 0,
						"last_execution_status": commonExecution.ExecutionStatusFailed,
					})
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return fmt.Errorf("transfer task %d summary no longer matches expired execution %s", taskID, item.ExecutionID)
				}
			}
			count++
		}
		if count < limit {
			abandoned, err := failAbandonedFileGDBExports(ctx, tx, limit-count)
			if err != nil {
				return err
			}
			count += abandoned
		}
		return nil
	})
	return count, err
}

func failAbandonedFileGDBExports(ctx context.Context, tx *gorm.DB, limit int) (int, error) {
	now, err := commonExecution.DatabaseNow(tx)
	if err != nil {
		return 0, err
	}
	query := tx.Where(`
		module = ? AND source = ? AND task_type = ? AND execution_boundary = ? AND status = ?
		AND source_task_id IS NULL AND parent_execution_id IS NULL AND attempt = 0
		AND execution_config->'filegdb_source' IS NOT NULL AND execution_authorization_id IS NULL
		AND authorization_expires_at IS NULL AND created_at < ?
	`, commonExecution.ModuleTransfer, commonExecution.ModuleManager, commonExecution.TaskTypeSync,
		commonExecution.ExecutionBoundaryBounded, commonExecution.ExecutionStatusPending, now.Add(-2*time.Minute)).
		Order("created_at ASC, id ASC").Limit(limit)
	if tx.Dialector.Name() == "postgres" {
		query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
	}
	var items []commonExecution.TaskExecution
	if err := query.Find(&items).Error; err != nil {
		return 0, err
	}
	fields := map[string]interface{}{
		"error_details": commonModels.JSONMap{
			"code":    "transfer.execution.source_authorization_required",
			"message": "FileGDB export source authorization preparation expired",
		},
	}
	for _, item := range items {
		identity := commonExecution.PendingIdentity{
			ExecutionID: item.ExecutionID, TenantID: item.TenantID, Module: item.Module, TaskType: item.TaskType, Source: item.Source,
		}
		if err := commonExecution.RejectUnstarted(ctx, tx, identity, fields); err != nil {
			return 0, err
		}
	}
	return len(items), nil
}

func (r *TaskRepository) RenewBoundedExecutionLease(ctx context.Context, lease commonExecution.Lease, duration time.Duration) error {
	return commonExecution.RenewLease(ctx, r.db, lease, duration)
}

func (r *TaskRepository) BoundedExecutionAttemptIsTerminal(ctx context.Context, lease commonExecution.Lease) (bool, error) {
	return commonExecution.AttemptIsTerminal(ctx, r.db, lease)
}

func isReplayExecution(execution *commonExecution.TaskExecution) bool {
	if execution == nil {
		return false
	}
	if _, ok := execution.ExecutionConfig["replay"]; ok {
		return true
	}
	_, ok := execution.Metadata["replay"]
	return ok
}
