package repository

import (
	"context"
	"fmt"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/graph/internal/models"
	"gorm.io/gorm"
)

func (r *BuildRepository) ProgressBuild(ctx context.Context, progress int, metadata commonmodels.JSONMap) error {
	return r.withBuildTransaction(ctx, func(tx *gorm.DB, task *models.BuildTask, e *execution.TaskExecution) error {
		lease, _ := execution.LeaseFromContext(ctx)
		return execution.UpdateWithLease(ctx, tx, lease, map[string]interface{}{"progress": progress, "metadata": metadata})
	})
}

func (r *BuildRepository) CompleteBuild(ctx context.Context, value *models.BuildTask, status, message string, metadata commonmodels.JSONMap, written *int64) error {
	return r.withBuildTransaction(ctx, func(tx *gorm.DB, task *models.BuildTask, e *execution.TaskExecution) error {
		var pending int64
		if err := tx.Model(&models.GraphOperation{}).Where("tenant_id = ? AND execution_id = ? AND state <> ?", e.TenantID, e.ExecutionID, models.GraphOperationKnown).Count(&pending).Error; err != nil {
			return err
		}
		var control models.BuildExecutionControl
		if err := tx.Where("execution_id = ? AND tenant_id = ?", e.ExecutionID, e.TenantID).First(&control).Error; err != nil {
			return err
		}
		if pending > 0 {
			status = execution.ExecutionStatusFailed
			if message != "" {
				message += "; "
			}
			message += "graph external effects require reconciliation"
		} else if control.CancelRequested {
			status = execution.ExecutionStatusCancelled
		}
		now, err := execution.DatabaseNow(tx)
		if err != nil {
			return err
		}
		fields := map[string]interface{}{"status": status, "completed_at": now, "error_message": message}
		if value.Stats != nil {
			fields["stats"] = value.Stats
		}
		if err := tx.Model(task).Updates(fields).Error; err != nil {
			return err
		}
		execFields := map[string]interface{}{}
		if metadata != nil {
			execFields["metadata"] = metadata
		}
		if written != nil {
			execFields["records_written"] = *written
		}
		if status == execution.ExecutionStatusSuccess {
			execFields["progress"] = 100
		}
		if status == execution.ExecutionStatusFailed || status == execution.ExecutionStatusTimeout {
			execFields["error_details"] = commonmodels.JSONMap{"code": "graph_build_failed", "type": "execution", "message": message}
		}
		lease, _ := execution.LeaseFromContext(ctx)
		return execution.CompleteWithLease(ctx, tx, lease, status, execFields)
	})
}

func (r *BuildRepository) BeginBuildOperation(ctx context.Context, op *models.GraphOperation) error {
	return r.WithBuildLease(ctx, func(tx *gorm.DB, task *models.BuildTask, e *execution.TaskExecution) error {
		if err := validateOperation(*op); err != nil {
			return err
		}
		lease, _ := execution.LeaseFromContext(ctx)
		op.RunnerID, op.LeaseTokenDigest = lease.Owner, tokenDigest(lease.Token)
		if op.ID == "" || op.TenantID != task.TenantID || op.GraphID != task.GraphID || op.ExecutionID != e.ExecutionID || op.Attempt != e.Attempt || op.TargetDigest == "" {
			return commonapi.ErrConflict
		}
		var c models.BuildExecutionControl
		if err := tx.Where("execution_id = ? AND tenant_id = ?", e.ExecutionID, e.TenantID).First(&c).Error; err != nil {
			return err
		}
		input, err := DecodeBuildInput(*e, c, *task)
		if err != nil {
			return err
		}
		kg := models.KnowledgeGraph{EngineID: input.EngineID, CatalogPath: input.CatalogPath, IndexManagement: input.IndexManagement}
		if !operationMatchesGraph(*op, kg, input.EngineVersion) {
			return commonapi.ErrConflict
		}
		if c.CancelRequested {
			return context.Canceled
		}
		if err := CheckGraphOpen(tx, task.GraphID, task.TenantID); err != nil {
			return err
		}
		now, err := execution.DatabaseNow(tx)
		if err != nil {
			return err
		}
		op.State = models.GraphOperationInFlight
		op.CreatedAt = now
		return tx.Create(op).Error
	})
}

func (r *BuildRepository) SettleBuildOperation(ctx context.Context, op models.GraphOperation, digest string, known bool) error {
	return r.WithBuildLease(ctx, func(tx *gorm.DB, task *models.BuildTask, e *execution.TaskExecution) error {
		if task.GraphID != op.GraphID || task.TenantID != op.TenantID || e.ExecutionID != op.ExecutionID || e.Attempt != op.Attempt {
			return commonapi.ErrConflict
		}
		state := models.GraphOperationUncertain
		if known {
			if !validDigest(digest) {
				return fmt.Errorf("operation evidence digest is required")
			}
			state = models.GraphOperationKnown
		}
		now, err := execution.DatabaseNow(tx)
		if err != nil {
			return err
		}
		result := tx.Model(&models.GraphOperation{}).Where("id = ? AND tenant_id = ? AND graph_id = ? AND execution_id = ? AND attempt = ? AND state = ?", op.ID, op.TenantID, op.GraphID, op.ExecutionID, op.Attempt, models.GraphOperationInFlight).Updates(map[string]interface{}{"state": state, "settled_at": now, "evidence_digest": digest})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return commonapi.ErrConflict
		}
		return nil
	})
}
