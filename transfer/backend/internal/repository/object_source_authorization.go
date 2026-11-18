package repository

import (
	"context"
	"encoding/json"
	"strconv"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *TaskRepository) BindTransferObjectAuthorization(ctx context.Context, record *execution.TaskExecution, issued *client.IssuedTransferObjectAuthorization) error {
	if record == nil {
		return commonAPI.ErrForbidden
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row struct {
			Config json.RawMessage `gorm:"column:frozen_config"`
			execution.TaskExecution
		}
		if err := tx.Model(&execution.TaskExecution{}).Select("*, execution_config AS frozen_config").Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("execution_id=? AND tenant_id=? AND module='transfer' AND source='transfer' AND task_type='sync' AND source_task_id IS NOT NULL AND parent_execution_id IS NULL AND trigger_type='manual' AND execution_boundary='bounded' AND max_attempts=1", record.ExecutionID, record.TenantID).Take(&row).Error; err != nil {
			return err
		}
		scope, err := execution.NewTransferObjectReadScope(row.Config)
		if err != nil || !issued.Matches(uint(row.TenantID), row.ExecutionID, scope) || row.ActorPrincipalID == nil || row.ActorTenantMembershipID == nil ||
			row.IssuedAuthorizationVersion == nil || row.TriggeredBy == nil || int64(*row.TriggeredBy) != *row.ActorPrincipalID {
			return commonAPI.ErrForbidden
		}
		id, _ := strconv.ParseInt(issued.ID, 10, 64)
		identity := execution.PendingIdentity{ExecutionID: row.ExecutionID, TenantID: row.TenantID, Module: row.Module, TaskType: row.TaskType, Source: row.Source}
		return execution.AttachPendingAuthorization(ctx, tx, identity, execution.PendingAuthorization{ID: id, ExpiresAt: issued.ExpiresAt,
			ActorPrincipalID: *row.ActorPrincipalID, ActorTenantMembershipID: *row.ActorTenantMembershipID, IssuedAuthorizationVersion: *row.IssuedAuthorizationVersion})
	})
}

func (r *TaskRepository) RejectObjectAuthorization(ctx context.Context, record *execution.TaskExecution) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		identity := execution.PendingIdentity{ExecutionID: record.ExecutionID, TenantID: record.TenantID, Module: record.Module, TaskType: record.TaskType, Source: record.Source}
		if err := execution.RejectUnstarted(ctx, tx, identity, map[string]interface{}{"error_details": commonModels.JSONMap{"code": "transfer.execution.source_authorization_required", "message": "Transfer exact object authorization was not bound"}}); err != nil {
			return err
		}
		return rejectObjectTaskSummary(ctx, tx, record)
	})
}

func rejectObjectTaskSummary(ctx context.Context, tx *gorm.DB, record *execution.TaskExecution) error {
	if record.SourceTaskID == nil {
		return nil
	}
	id, err := execution.ParseSourceTaskIDUint(record.SourceTaskID)
	if err != nil {
		return err
	}
	return tx.WithContext(ctx).Model(&models.TransferTask{}).Where("id=? AND tenant_id=? AND last_execution_id=? AND last_execution_status='pending'", id, record.TenantID, record.ExecutionID).
		Updates(map[string]interface{}{"status": models.TaskStatusIdle, "last_execution_status": execution.ExecutionStatusFailed, "progress": 0}).Error
}
