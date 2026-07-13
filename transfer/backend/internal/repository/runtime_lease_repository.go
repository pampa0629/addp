package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrRuntimeLeaseLost = errors.New("continuous runtime lease lost")

type RuntimeLeaseClaim struct {
	Task      models.TransferTask
	Execution commonExecution.TaskExecution
	Lease     models.RuntimeLease
}

type ContinuousProgress struct {
	RecordsRead    int64
	RecordsWritten int64
	Partition      string
	Position       models.JSONMap
	CommittedAt    time.Time
}

type RuntimeLeaseRepository struct {
	db *gorm.DB
}

func NewRuntimeLeaseRepository(db *gorm.DB) *RuntimeLeaseRepository {
	return &RuntimeLeaseRepository{db: db}
}

// ClaimNext 使用 SKIP LOCKED 领取一个 pending continuous execution，并递增 fencing token。
func (r *RuntimeLeaseRepository) ClaimNext(ctx context.Context, owner string, now time.Time, duration time.Duration) (*RuntimeLeaseClaim, error) {
	var claim *RuntimeLeaseClaim
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var execution commonExecution.TaskExecution
		query := tx.Table("common.task_executions AS e").
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED", Table: clause.Table{Name: "e"}}).
			Select("e.*").
			Joins("JOIN transfer.transfer_tasks AS t ON CAST(t.id AS TEXT) = e.source_task_id").
			Joins("LEFT JOIN transfer.runtime_leases AS l ON l.task_id = t.id").
			Where("e.module = ? AND e.task_type = ? AND e.status IN (?, ?)", commonExecution.ModuleTransfer, commonExecution.TaskTypeSync, commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning).
			Where("t.desired_state = ? AND t.deleted_at IS NULL", models.TaskDesiredStateRunning).
			Where("l.id IS NULL OR l.lease_until <= ?", now).
			Order("e.created_at ASC, e.id ASC").Limit(1)
		if err := query.Scan(&execution).Error; err != nil {
			return err
		}
		if execution.ID == 0 {
			return nil
		}
		taskID, err := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
		if err != nil {
			return err
		}
		var task models.TransferTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, taskID).Error; err != nil {
			return err
		}
		if task.DesiredState != models.TaskDesiredStateRunning {
			return nil
		}
		var existing models.RuntimeLease
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ?", taskID).First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && existing.LeaseUntil.After(now) {
			return nil
		}
		token := uint64(1)
		claimedAt := now
		if err == nil {
			token = existing.FencingToken + 1
		}
		lease := models.RuntimeLease{
			TaskID: taskID, ExecutionID: execution.ExecutionID, OwnerInstanceID: owner,
			LeaseUntil: now.Add(duration), HeartbeatAt: now, FencingToken: token, ClaimedAt: claimedAt,
		}
		if err == nil {
			lease.ID = existing.ID
			if err := tx.Model(&existing).Updates(map[string]interface{}{
				"execution_id": lease.ExecutionID, "owner_instance_id": owner,
				"lease_until": lease.LeaseUntil, "heartbeat_at": now,
				"fencing_token": token, "claimed_at": claimedAt,
			}).Error; err != nil {
				return err
			}
		} else if err := tx.Create(&lease).Error; err != nil {
			return err
		}
		if err := tx.Model(&execution).Where("status = ?", commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{"status": commonExecution.ExecutionStatusRunning, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&task).Updates(map[string]interface{}{
			"status": models.TaskStatusRunning, "last_execution_status": commonExecution.ExecutionStatusRunning,
		}).Error; err != nil {
			return err
		}
		claim = &RuntimeLeaseClaim{Task: task, Execution: execution, Lease: lease}
		claim.Execution.Status = commonExecution.ExecutionStatusRunning
		return nil
	})
	return claim, err
}

func (r *RuntimeLeaseRepository) Renew(ctx context.Context, taskID uint, owner string, token uint64, now time.Time, duration time.Duration) error {
	result := r.db.WithContext(ctx).Model(&models.RuntimeLease{}).
		Where("task_id = ? AND owner_instance_id = ? AND fencing_token = ?", taskID, owner, token).
		Where("EXISTS (SELECT 1 FROM transfer.transfer_tasks t WHERE t.id = ? AND t.desired_state = ?)", taskID, models.TaskDesiredStateRunning).
		Updates(map[string]interface{}{"lease_until": now.Add(duration), "heartbeat_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrRuntimeLeaseLost
	}
	return nil
}

func (r *RuntimeLeaseRepository) Finish(ctx context.Context, claim RuntimeLeaseClaim, status, stopReason, errorMessage string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.RuntimeLease{}).
			Where("task_id = ? AND owner_instance_id = ? AND fencing_token = ?", claim.Task.ID, claim.Lease.OwnerInstanceID, claim.Lease.FencingToken).
			Updates(map[string]interface{}{"owner_instance_id": "", "lease_until": now, "heartbeat_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRuntimeLeaseLost
		}
		var execution commonExecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("execution_id = ?", claim.Execution.ExecutionID).First(&execution).Error; err != nil {
			return err
		}
		metadata := execution.Metadata
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		if stopReason != "" {
			metadata["stop_reason"] = stopReason
		}
		updates := map[string]interface{}{
			"status": status, "metadata": metadata, "completed_at": now, "updated_at": now,
		}
		if errorMessage != "" {
			updates["error_details"] = commonModels.JSONMap{"message": errorMessage}
		}
		result = tx.Model(&execution).
			Where("execution_id = ? AND status = ?", claim.Execution.ExecutionID, commonExecution.ExecutionStatusRunning).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("finish continuous execution %s: current status is not running", claim.Execution.ExecutionID)
		}
		return tx.Model(&models.TransferTask{}).Where("id = ?", claim.Task.ID).Updates(map[string]interface{}{
			"status": models.TaskStatusIdle, "last_execution_status": status,
		}).Error
	})
}

func (r *RuntimeLeaseRepository) RecordProgress(ctx context.Context, claim RuntimeLeaseClaim, progress ContinuousProgress) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var leaseCount int64
		if err := tx.Model(&models.RuntimeLease{}).
			Where("task_id = ? AND owner_instance_id = ? AND fencing_token = ? AND lease_until > CURRENT_TIMESTAMP", claim.Task.ID, claim.Lease.OwnerInstanceID, claim.Lease.FencingToken).
			Count(&leaseCount).Error; err != nil {
			return err
		}
		if leaseCount != 1 {
			return ErrRuntimeLeaseLost
		}
		var execution commonExecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("execution_id = ? AND status = ?", claim.Execution.ExecutionID, commonExecution.ExecutionStatusRunning).
			First(&execution).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRuntimeLeaseLost
			}
			return err
		}
		metadata := execution.Metadata
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		continuousMeta, _ := metadata["continuous"].(map[string]interface{})
		if continuousMeta == nil {
			continuousMeta = map[string]interface{}{}
		}
		partitions, _ := continuousMeta["partitions"].(map[string]interface{})
		if partitions == nil {
			partitions = map[string]interface{}{}
		}
		partitions[progress.Partition] = progress.Position
		continuousMeta["partitions"] = partitions
		continuousMeta["last_committed_at"] = progress.CommittedAt
		metadata["continuous"] = continuousMeta
		return tx.Model(&execution).Updates(map[string]interface{}{
			"records_read": progress.RecordsRead, "records_written": progress.RecordsWritten,
			"metadata": metadata, "updated_at": progress.CommittedAt,
		}).Error
	})
}

func (r *RuntimeLeaseRepository) DesiredState(ctx context.Context, taskID uint) (models.TaskDesiredState, error) {
	var task models.TransferTask
	if err := r.db.WithContext(ctx).Select("id", "desired_state").Where("id = ?", taskID).First(&task).Error; err != nil {
		return "", err
	}
	return task.DesiredState, nil
}
