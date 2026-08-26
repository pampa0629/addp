package repository

import (
	"context"
	"fmt"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/model/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MaterializationBatchRepository struct{ db *gorm.DB }

func NewMaterializationBatchRepository(db *gorm.DB) *MaterializationBatchRepository {
	return &MaterializationBatchRepository{db: db}
}

func (r *MaterializationBatchRepository) CreatePrepareExecution(
	ctx context.Context,
	batch *models.MaterializationBatch,
	execution *commonExecution.TaskExecution,
	tableName string,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var table models.LogicalTable
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).
			Where("id = ? AND tenant_id = ?", batch.LogicalTableID, batch.TenantID).
			First(&table).Error; err != nil {
			return err
		}
		if table.Status != "approved" || table.Version != batch.LogicalTableVersion {
			return fmt.Errorf("%w: logical table approval or version changed", commonAPI.ErrConflict)
		}
		if err := tx.Create(batch).Error; err != nil {
			return err
		}
		sourceTaskID := fmt.Sprintf("%d", batch.LogicalTableID)
		execution.SourceTaskID = &sourceTaskID
		execution.SourceTaskName = &tableName
		return tx.Create(execution).Error
	})
}

func (r *MaterializationBatchRepository) CreatePublishExecution(
	ctx context.Context,
	tenantID, logicalTableID int64,
	parentExecutionID *string,
	execution *commonExecution.TaskExecution,
) (*models.MaterializationBatch, error) {
	var batch models.MaterializationBatch
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Table("model.materialization_batches AS batch").
			Select("batch.*").
			Joins("JOIN common.task_executions AS prepare_execution ON prepare_execution.execution_id = batch.prepare_execution_id").
			Where("batch.tenant_id = ? AND batch.logical_table_id = ? AND batch.status = ?",
				tenantID, logicalTableID, models.MaterializationBatchPrepared)
		if parentExecutionID == nil {
			return fmt.Errorf("%w: materialization publish requires an orchestration parent execution", commonAPI.ErrConflict)
		}
		query = query.Where("prepare_execution.parent_execution_id = ?", *parentExecutionID)
		if err := query.Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "batch"}}).
			First(&batch).Error; err != nil {
			return err
		}
		var table models.LogicalTable
		if err := tx.Select("id", "name", "status", "version").
			Where("id = ? AND tenant_id = ?", logicalTableID, tenantID).First(&table).Error; err != nil {
			return err
		}
		if table.Status != "approved" || table.Version != batch.LogicalTableVersion {
			return fmt.Errorf("%w: logical table approval or version changed", commonAPI.ErrConflict)
		}
		sourceTaskID := fmt.Sprintf("%d", logicalTableID)
		execution.SourceTaskID = &sourceTaskID
		execution.SourceTaskName = &table.Name
		execution.ExecutionConfig["batch_id"] = batch.ID
		if err := tx.Create(execution).Error; err != nil {
			return err
		}
		if result := tx.Model(&batch).Where("status = ?", models.MaterializationBatchPrepared).
			Updates(map[string]interface{}{
				"status":               models.MaterializationBatchPublishing,
				"publish_execution_id": execution.ExecutionID,
				"updated_at":           time.Now().UTC(),
			}); result.Error != nil || result.RowsAffected != 1 {
			if result.Error != nil {
				return result.Error
			}
			return fmt.Errorf("%w: materialization batch state changed", commonAPI.ErrConflict)
		}
		return nil
	})
	return &batch, err
}

func (r *MaterializationBatchRepository) AttachAuthorization(
	ctx context.Context,
	tenantID int64,
	executionID string,
	fields map[string]interface{},
) error {
	fields["updated_at"] = time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&commonExecution.TaskExecution{}).
		Where("tenant_id = ? AND execution_id = ? AND status = ? AND execution_authorization_id IS NULL",
			tenantID, executionID, commonExecution.ExecutionStatusPending).
		Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("%w: materialization execution cannot attach authorization", commonAPI.ErrConflict)
	}
	return nil
}

func (r *MaterializationBatchRepository) FailPendingExecution(
	ctx context.Context,
	tenantID int64,
	executionID, batchID, taskType, errorCode string,
) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&commonExecution.TaskExecution{}).
			Where("tenant_id = ? AND execution_id = ? AND status = ?", tenantID, executionID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{
				"status":        commonExecution.ExecutionStatusFailed,
				"completed_at":  now,
				"updated_at":    now,
				"error_details": commonModels.JSONMap{"code": errorCode, "message": "materialization authorization could not be prepared"},
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: materialization execution is not pending", commonAPI.ErrConflict)
		}
		batchStatus := models.MaterializationBatchFailed
		if taskType == commonExecution.TaskTypeMaterializationPublish {
			batchStatus = models.MaterializationBatchPrepared
		}
		return tx.Model(&models.MaterializationBatch{}).
			Where("id = ? AND tenant_id = ?", batchID, tenantID).
			Updates(map[string]interface{}{"status": batchStatus, "updated_at": now}).Error
	})
}

func (r *MaterializationBatchRepository) ClaimPendingExecution(
	ctx context.Context,
	taskType, workerID string,
	now time.Time,
	lease time.Duration,
) (*commonExecution.TaskExecution, *models.MaterializationBatch, error) {
	var execution *commonExecution.TaskExecution
	var batch models.MaterializationBatch
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		execution, _, err = commonExecution.ClaimNext(ctx, tx, commonExecution.ClaimOptions{
			Module: commonExecution.ModuleModel, TaskType: taskType, WorkerID: workerID,
			Now: now, LeaseDuration: lease, RequireAuthorization: true,
		})
		if err != nil || execution == nil {
			return err
		}
		batchID, ok := execution.ExecutionConfig["batch_id"].(string)
		if !ok || batchID == "" {
			return fmt.Errorf("materialization execution %s has no batch_id", execution.ExecutionID)
		}
		return tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", batchID, execution.TenantID).First(&batch).Error
	})
	if err != nil || execution == nil {
		return execution, nil, err
	}
	return execution, &batch, nil
}

func (r *MaterializationBatchRepository) CompleteExecution(
	ctx context.Context,
	lease commonExecution.Lease,
	batchID, taskType, executionStatus, batchStatus string,
	metadata, errorDetails commonModels.JSONMap,
) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fields := map[string]interface{}{"progress": 100}
		if metadata != nil {
			fields["metadata"] = metadata
		}
		if errorDetails != nil {
			fields["error_details"] = errorDetails
		}
		if err := commonExecution.CompleteWithLease(ctx, tx, lease, executionStatus, now, fields); err != nil {
			return err
		}
		updates := map[string]interface{}{"status": batchStatus, "updated_at": now}
		if batchStatus == models.MaterializationBatchPublished {
			updates["published_at"] = now
		}
		if taskType == commonExecution.TaskTypeMaterializationPublish && executionStatus != commonExecution.ExecutionStatusSuccess {
			updates["status"] = models.MaterializationBatchPrepared
		}
		return tx.Model(&models.MaterializationBatch{}).
			Where("id = ? AND tenant_id = ?", batchID, lease.TenantID).Updates(updates).Error
	})
}

func (r *MaterializationBatchRepository) GetByID(ctx context.Context, id string, tenantID int64) (*models.MaterializationBatch, error) {
	var batch models.MaterializationBatch
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&batch).Error
	return &batch, err
}

func (r *MaterializationBatchRepository) ResolvePreparedByParentExecution(
	ctx context.Context,
	tenantID, logicalTableID int64,
	parentExecutionID string,
) (*models.MaterializationBatch, error) {
	var batches []models.MaterializationBatch
	err := r.db.WithContext(ctx).
		Table("model.materialization_batches AS batch").
		Select("batch.*").
		Joins("JOIN common.task_executions AS prepare_execution ON prepare_execution.execution_id = batch.prepare_execution_id").
		Joins("JOIN common.task_executions AS parent_execution ON parent_execution.execution_id = prepare_execution.parent_execution_id").
		Where(`batch.tenant_id = ? AND batch.logical_table_id = ? AND batch.status = ?
			AND prepare_execution.tenant_id = batch.tenant_id
			AND prepare_execution.parent_execution_id = ?
			AND prepare_execution.status = ?
			AND parent_execution.tenant_id = batch.tenant_id
			AND parent_execution.module = ?
			AND parent_execution.status = ?
			AND prepare_execution.actor_principal_id = parent_execution.actor_principal_id
			AND prepare_execution.actor_tenant_membership_id = parent_execution.actor_tenant_membership_id`,
			tenantID, logicalTableID, models.MaterializationBatchPrepared,
			parentExecutionID, commonExecution.ExecutionStatusSuccess,
			commonExecution.ModuleOrchestrator, commonExecution.ExecutionStatusRunning).
		Order("batch.created_at DESC").Limit(2).Find(&batches).Error
	if err != nil {
		return nil, err
	}
	if len(batches) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	if len(batches) != 1 {
		return nil, fmt.Errorf("%w: multiple prepared materialization batches match execution lineage", commonAPI.ErrConflict)
	}
	return &batches[0], nil
}

func (r *MaterializationBatchRepository) RecoverExpiredExecutions(ctx context.Context, taskType string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		items, err := commonExecution.FindExpiredForUpdate(ctx, tx, commonExecution.ExpiredOptions{
			Module: commonExecution.ModuleModel, TaskType: taskType, Now: now, Limit: 100,
		})
		if err != nil {
			return err
		}
		for _, item := range items {
			lease, err := commonExecution.LeaseFromExecution(item)
			if err != nil {
				return err
			}
			if item.Attempt < item.MaxAttempts {
				if err := commonExecution.RetryExpired(ctx, tx, lease, now, "retrying controlled materialization"); err != nil {
					return err
				}
				continue
			}
			if err := commonExecution.FailExpired(ctx, tx, lease, now, map[string]interface{}{
				"error_details": commonModels.JSONMap{"code": "model.materialization.lease_expired", "message": "materialization worker lease expired"},
			}); err != nil {
				return err
			}
			batchID, _ := item.ExecutionConfig["batch_id"].(string)
			batchStatus := models.MaterializationBatchFailed
			if taskType == commonExecution.TaskTypeMaterializationPublish {
				batchStatus = models.MaterializationBatchPrepared
			}
			if err := tx.Model(&models.MaterializationBatch{}).
				Where("id = ? AND tenant_id = ?", batchID, item.TenantID).
				Updates(map[string]interface{}{"status": batchStatus, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
