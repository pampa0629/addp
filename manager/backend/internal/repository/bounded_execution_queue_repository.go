package repository

import (
	"context"
	"fmt"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	managerLeaseExpiredCode     = "manager.execution.lease_expired"
	managerLeaseMissingCode     = "manager.execution.lease_missing"
	managerOwnerUnavailableCode = "manager.execution.owner_unavailable"
)

type managerExecutionOwnership struct {
	taskTable      string
	resultTable    string
	buildingStatus string
}

var managerExecutionOwnerships = map[string]managerExecutionOwnership{
	commonExecution.TaskTypeVectorTileCacheGeneration:        {"manager.vector_tile_cache_tasks", "manager.vector_tile_cache", "generating"},
	commonExecution.TaskTypeVectorTileSetGeneration:          {taskTable: "manager.vector_tile_set_tasks"},
	commonExecution.TaskTypeVectorMaterializedViewGeneration: {"manager.vector_materialized_view_tasks", "manager.vector_materialized_view", "building"},
	commonExecution.TaskTypeRasterCOGGeneration:              {"manager.raster_cog_tasks", "manager.raster_cog", "building"},
	commonExecution.TaskTypeRasterMosaicGeneration:           {taskTable: "manager.raster_mosaic_tasks"},
	commonExecution.TaskTypeModel3DGLBGeneration:             {"manager.model_3d_glb_tasks", "manager.model_3d_glb", "building"},
	commonExecution.TaskTypeModel3DTilesGeneration:           {"manager.model3d_tiles_tasks", "manager.model3d_tiles", "building"},
	commonExecution.TaskTypeGaussianSplatKSplatGeneration:    {"manager.gaussian_splat_ksplat_tasks", "manager.gaussian_splat_ksplat", "building"},
	commonExecution.TaskTypePointCloudCOPCGeneration:         {"manager.point_cloud_copc_tasks", "manager.point_cloud_copc", "building"},
	commonExecution.TaskTypePPTXPDFGeneration:                {"manager.pptx_pdf_tasks", "manager.pptx_pdf", "building"},
	commonExecution.TaskTypeEmbedding:                        {taskTable: "manager.embedding_tasks"},
	commonExecution.TaskTypeDataProfiling:                    {},
}

func ManagerBoundedTaskTypes() []string {
	return []string{
		commonExecution.TaskTypeVectorTileCacheGeneration,
		commonExecution.TaskTypeVectorTileSetGeneration,
		commonExecution.TaskTypeVectorMaterializedViewGeneration,
		commonExecution.TaskTypeRasterCOGGeneration,
		commonExecution.TaskTypeRasterMosaicGeneration,
		commonExecution.TaskTypeModel3DGLBGeneration,
		commonExecution.TaskTypeModel3DTilesGeneration,
		commonExecution.TaskTypeGaussianSplatKSplatGeneration,
		commonExecution.TaskTypePointCloudCOPCGeneration,
		commonExecution.TaskTypePPTXPDFGeneration,
		commonExecution.TaskTypeEmbedding,
		commonExecution.TaskTypeDataProfiling,
	}
}

type BoundedExecutionQueueRepository struct{ db *gorm.DB }

func NewBoundedExecutionQueueRepository(db *gorm.DB) *BoundedExecutionQueueRepository {
	return &BoundedExecutionQueueRepository{db: db}
}

// UpdateExecutionWithOwnership applies progress/metadata updates only through
// the exact attempt-scoped lease owned by the current Manager supervisor.
func UpdateExecutionWithOwnership(ctx context.Context, db *gorm.DB, executionID string, tenantID int, fields map[string]interface{}) error {
	lease, ok := commonExecution.LeaseFromContext(ctx)
	if !ok || lease.ExecutionID != executionID || lease.TenantID != tenantID {
		return fmt.Errorf("%w: execution lease does not match %s", commonAPI.ErrConflict, executionID)
	}
	return commonExecution.UpdateWithLease(ctx, db, lease, fields)
}

func (r *BoundedExecutionQueueRepository) ClaimNext(ctx context.Context, taskType, owner string, now time.Time, leaseDuration time.Duration) (*commonExecution.TaskExecution, *commonExecution.Lease, error) {
	ownership, ok := managerExecutionOwnerships[taskType]
	if !ok {
		return nil, nil, fmt.Errorf("unsupported Manager bounded task type %q", taskType)
	}
	var execution *commonExecution.TaskExecution
	var lease *commonExecution.Lease
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		execution, lease, err = commonExecution.ClaimNext(ctx, tx, commonExecution.ClaimOptions{
			Module: commonExecution.ModuleManager, TaskType: taskType, WorkerID: owner,
			Now: now, LeaseDuration: leaseDuration,
		})
		if err != nil || execution == nil || ownership.taskTable == "" {
			return err
		}
		// Ad-hoc embedding executions intentionally have no persisted task owner.
		if execution.SourceTaskID == nil && taskType == commonExecution.TaskTypeEmbedding {
			return nil
		}
		if execution.SourceTaskID == nil {
			if err := failUnclaimableManagerExecution(ctx, tx, execution, *lease, now, "Manager bounded execution has no task owner"); err != nil {
				return err
			}
			execution, lease = nil, nil
			return nil
		}
		taskID, err := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
		if err != nil {
			if failErr := failUnclaimableManagerExecution(ctx, tx, execution, *lease, now, "Manager bounded execution has an invalid task owner"); failErr != nil {
				return failErr
			}
			execution, lease = nil, nil
			return nil
		}
		result := tx.Table(ownership.taskTable).
			Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?", taskID, execution.TenantID, execution.ExecutionID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{"last_run_at": now.UTC(), "last_execution_status": commonExecution.ExecutionStatusRunning, "updated_at": now.UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			if err := failUnclaimableManagerExecution(ctx, tx, execution, *lease, now, "Manager bounded execution task owner is unavailable"); err != nil {
				return err
			}
			execution, lease = nil, nil
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return execution, lease, err
}

func failUnclaimableManagerExecution(
	ctx context.Context,
	tx *gorm.DB,
	execution *commonExecution.TaskExecution,
	lease commonExecution.Lease,
	now time.Time,
	message string,
) error {
	fields := map[string]interface{}{
		"error_details":     commonModels.JSONMap{"code": managerOwnerUnavailableCode, "message": message},
		"execution_time_ms": int64(0),
	}
	return commonExecution.CompleteWithLease(ctx, tx, lease, commonExecution.ExecutionStatusFailed, now.UTC(), fields)
}

func (r *BoundedExecutionQueueRepository) RenewLease(ctx context.Context, lease commonExecution.Lease, expiresAt time.Time) error {
	return commonExecution.RenewLease(ctx, r.db, lease, expiresAt)
}

func (r *BoundedExecutionQueueRepository) AttemptIsTerminal(ctx context.Context, lease commonExecution.Lease) (bool, error) {
	return commonExecution.AttemptIsTerminal(ctx, r.db, lease)
}

func (r *BoundedExecutionQueueRepository) FailClaimed(ctx context.Context, execution *commonExecution.TaskExecution, lease commonExecution.Lease, code, message string, now time.Time) error {
	if execution == nil {
		return fmt.Errorf("claimed execution is required")
	}
	ownership, ok := managerExecutionOwnerships[execution.TaskType]
	if !ok {
		return fmt.Errorf("unsupported Manager bounded task type %q", execution.TaskType)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		fields := map[string]interface{}{"error_details": commonModels.JSONMap{"code": code, "message": message}}
		if execution.StartedAt != nil {
			fields["execution_time_ms"] = now.Sub(*execution.StartedAt).Milliseconds()
		}
		if err := commonExecution.CompleteWithLease(ctx, tx, lease, commonExecution.ExecutionStatusFailed, now, fields); err != nil {
			return err
		}
		return updateManagerOwnershipFailure(tx, ownership, execution, now, message)
	})
}

// RecoverUnleased closes running Manager bounded executions that predate or
// violate the lease protocol. A valid claim changes status and lease identity
// atomically, so a visible running row without a complete lease can never be a
// legal current attempt.
func (r *BoundedExecutionQueueRepository) RecoverUnleased(ctx context.Context, now time.Time, limit int) (int, error) {
	total := 0
	for _, taskType := range ManagerBoundedTaskTypes() {
		count, err := r.recoverUnleasedTaskType(ctx, taskType, now, limit-total)
		if err != nil {
			return total, err
		}
		total += count
		if limit > 0 && total >= limit {
			break
		}
	}
	return total, nil
}

func (r *BoundedExecutionQueueRepository) recoverUnleasedTaskType(ctx context.Context, taskType string, now time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	ownership := managerExecutionOwnerships[taskType]
	recovered := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where(
			"module = ? AND task_type = ? AND execution_boundary = ? AND status = ? AND (attempt <= 0 OR lease_token IS NULL OR lease_owner IS NULL OR lease_expires_at IS NULL)",
			commonExecution.ModuleManager, taskType, commonExecution.ExecutionBoundaryBounded, commonExecution.ExecutionStatusRunning,
		).Order("created_at ASC, id ASC").Limit(limit)
		if tx.Dialector.Name() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		var items []commonExecution.TaskExecution
		if err := query.Find(&items).Error; err != nil {
			return err
		}
		for index := range items {
			item := &items[index]
			message := "Manager bounded execution has no valid lease"
			fields := map[string]interface{}{
				"status": commonExecution.ExecutionStatusFailed, "completed_at": now.UTC(), "updated_at": now.UTC(),
				"lease_owner": nil, "lease_token": nil, "lease_expires_at": nil,
				"error_details": commonModels.JSONMap{"code": managerLeaseMissingCode, "message": message},
			}
			if item.StartedAt != nil {
				fields["execution_time_ms"] = now.Sub(*item.StartedAt).Milliseconds()
			}
			result := tx.Model(&commonExecution.TaskExecution{}).
				Where("execution_id = ? AND tenant_id = ? AND status = ? AND (attempt <= 0 OR lease_token IS NULL OR lease_owner IS NULL OR lease_expires_at IS NULL)", item.ExecutionID, item.TenantID, commonExecution.ExecutionStatusRunning).
				Updates(fields)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("%w: unleased Manager execution %s changed concurrently", commonAPI.ErrConflict, item.ExecutionID)
			}
			if err := updateManagerOwnershipFailure(tx, ownership, item, now, message); err != nil {
				return err
			}
			recovered++
		}
		return nil
	})
	return recovered, err
}

func (r *BoundedExecutionQueueRepository) RecoverExpired(ctx context.Context, now time.Time, limit int) (int, error) {
	total := 0
	for _, taskType := range ManagerBoundedTaskTypes() {
		count, err := r.recoverExpiredTaskType(ctx, taskType, now, limit-total)
		if err != nil {
			return total, err
		}
		total += count
		if limit > 0 && total >= limit {
			break
		}
	}
	return total, nil
}

func (r *BoundedExecutionQueueRepository) recoverExpiredTaskType(ctx context.Context, taskType string, now time.Time, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	ownership := managerExecutionOwnerships[taskType]
	recovered := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		items, err := commonExecution.FindExpiredForUpdate(ctx, tx, commonExecution.ExpiredOptions{
			Module: commonExecution.ModuleManager, TaskType: taskType, Now: now, Limit: limit,
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
			details := commonModels.JSONMap{"code": managerLeaseExpiredCode, "message": "Manager bounded execution lease expired"}
			fields := map[string]interface{}{"error_details": details}
			if item.StartedAt != nil {
				fields["execution_time_ms"] = now.Sub(*item.StartedAt).Milliseconds()
			}
			if err := commonExecution.FailExpired(ctx, tx, lease, now, fields); err != nil {
				return err
			}
			if err := updateManagerOwnershipFailure(tx, ownership, &item, now, "Manager bounded execution lease expired"); err != nil {
				return err
			}
			recovered++
		}
		return nil
	})
	return recovered, err
}

func updateManagerOwnershipFailure(tx *gorm.DB, ownership managerExecutionOwnership, execution *commonExecution.TaskExecution, now time.Time, message string) error {
	if ownership.taskTable != "" && execution.SourceTaskID != nil {
		taskID, err := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
		if err != nil {
			return err
		}
		result := tx.Table(ownership.taskTable).
			Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?", taskID, execution.TenantID, execution.ExecutionID, commonExecution.ExecutionStatusRunning).
			Updates(map[string]interface{}{"last_execution_status": commonExecution.ExecutionStatusFailed, "updated_at": now.UTC()})
		if result.Error != nil {
			return result.Error
		}
		// A deleted task or a task already pointing at a newer execution must not
		// prevent the abandoned execution itself from reaching a terminal state.
	}
	if ownership.resultTable != "" {
		result := tx.Table(ownership.resultTable).
			Where("tenant_id = ? AND last_execution_id = ? AND status = ?", execution.TenantID, execution.ExecutionID, ownership.buildingStatus).
			Updates(map[string]interface{}{"status": "failed", "error_message": message, "updated_at": now.UTC()})
		if result.Error != nil {
			return result.Error
		}
	}
	return nil
}
