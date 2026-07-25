package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/google/uuid"
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
	LastEventAt    *time.Time
}

type ContinuousPartitionDiagnostics struct {
	Partition                  string   `json:"partition"`
	EarliestOffset             int64    `json:"earliest_offset"`
	LatestOffset               int64    `json:"latest_offset"`
	NextOffset                 *int64   `json:"next_offset,omitempty"`
	LagRecords                 *int64   `json:"lag_records,omitempty"`
	RecoveryHeadroomRecords    *int64   `json:"recovery_headroom_records,omitempty"`
	SourceRateRecordsPerSecond *float64 `json:"source_rate_records_per_second,omitempty"`
	RetentionHorizonSeconds    *float64 `json:"retention_horizon_seconds,omitempty"`
	Health                     string   `json:"health"`
	CheckpointAgeSeconds       *float64 `json:"checkpoint_age_seconds,omitempty"`
	CheckpointHealth           string   `json:"checkpoint_health"`
}

type ContinuousDiagnostics struct {
	SampledAt                   time.Time                                 `json:"sampled_at"`
	Health                      string                                    `json:"health"`
	DegradedHorizonSeconds      float64                                   `json:"degraded_horizon_seconds"`
	CriticalHorizonSeconds      float64                                   `json:"critical_horizon_seconds"`
	CheckpointStaleAfterSeconds float64                                   `json:"checkpoint_stale_after_seconds"`
	CheckpointHealth            string                                    `json:"checkpoint_health"`
	Partitions                  map[string]ContinuousPartitionDiagnostics `json:"partitions"`
	Error                       string                                    `json:"error,omitempty"`
}

type ContinuousSchemaChange struct {
	DetectedAt         time.Time `json:"detected_at"`
	Scope              string    `json:"scope"`
	SourcePartition    string    `json:"source_partition"`
	SourceOffset       int64     `json:"source_offset"`
	MissingFields      []string  `json:"missing_fields,omitempty"`
	UnexpectedFields   []string  `json:"unexpected_fields,omitempty"`
	IncompatibleFields []string  `json:"incompatible_fields,omitempty"`
}

type RuntimeLeaseRepository struct {
	db             *gorm.DB
	recoveryPolicy ContinuousRecoveryPolicy
}

func NewRuntimeLeaseRepository(db *gorm.DB, recoveryPolicy ContinuousRecoveryPolicy) *RuntimeLeaseRepository {
	return &RuntimeLeaseRepository{db: db, recoveryPolicy: recoveryPolicy}
}

// ClaimNext 使用 SKIP LOCKED 领取一个 pending continuous execution，并递增 fencing token。
// worker shutdown 或 lease 过期时先创建新的 recovery execution，不复用已结束或失联的 execution。
func (r *RuntimeLeaseRepository) ClaimNext(ctx context.Context, owner string, now time.Time, duration time.Duration) (*RuntimeLeaseClaim, error) {
	var claim *RuntimeLeaseClaim
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var execution commonExecution.TaskExecution
		if err := selectPendingContinuousExecution(tx, now, &execution); err != nil {
			return err
		}
		if execution.ID == 0 {
			recovered, err := createNextRecoveryExecution(tx, now, r.recoveryPolicy)
			if err != nil {
				return err
			}
			if !recovered {
				return nil
			}
			if err := selectPendingContinuousExecution(tx, now, &execution); err != nil {
				return err
			}
			if execution.ID == 0 {
				return nil
			}
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
		metadata := mergeContinuousRuntimeMetadata(execution.Metadata, owner, token, now, lease.LeaseUntil)
		metadata["recovery_claimed_at"] = now
		if metadata["recovery_circuit_state"] == recoveryCircuitOpen {
			metadata["recovery_circuit_state"] = recoveryCircuitHalfOpen
		}
		result := tx.Model(&execution).
			Where("status = ?", commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{
				"status": commonExecution.ExecutionStatusRunning, "metadata": metadata,
				"started_at": now, "updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRuntimeLeaseLost
		}
		if err := tx.Model(&task).Updates(map[string]interface{}{
			"status": models.TaskStatusRunning, "last_execution_status": commonExecution.ExecutionStatusRunning,
		}).Error; err != nil {
			return err
		}
		claim = &RuntimeLeaseClaim{Task: task, Execution: execution, Lease: lease}
		claim.Execution.Status = commonExecution.ExecutionStatusRunning
		claim.Execution.Metadata = metadata
		claim.Execution.StartedAt = &now
		return nil
	})
	return claim, err
}

func selectPendingContinuousExecution(tx *gorm.DB, now time.Time, execution *commonExecution.TaskExecution) error {
	return tx.Table("common.task_executions AS e").
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED", Table: clause.Table{Name: "e"}}).
		Select("e.*").
		Joins("JOIN transfer.transfer_tasks AS t ON CAST(t.id AS TEXT) = e.source_task_id").
		Joins("LEFT JOIN transfer.runtime_leases AS l ON l.task_id = t.id").
		Where("e.module = ? AND e.task_type = ? AND e.status = ?", commonExecution.ModuleTransfer, commonExecution.TaskTypeSync, commonExecution.ExecutionStatusPending).
		Where("t.desired_state = ? AND t.status <> ? AND t.deleted_at IS NULL", models.TaskDesiredStateRunning, models.TaskStatusBlocked).
		Where(`NOT EXISTS (
				SELECT 1 FROM transfer.schema_change_requests AS scr
				WHERE scr.task_id = t.id AND scr.tenant_id = t.tenant_id AND scr.status = ?
			)`, models.SchemaChangeRequestPending).
		Where("l.id IS NULL OR l.lease_until <= ?", now).
		Where("e.metadata->>'recovery_not_before' IS NULL OR (e.metadata->>'recovery_not_before')::timestamptz <= ?", now).
		Order("e.created_at ASC, e.id ASC").Limit(1).
		Scan(execution).Error
}

func createNextRecoveryExecution(tx *gorm.DB, now time.Time, policy ContinuousRecoveryPolicy) (bool, error) {
	var task models.TransferTask
	err := tx.Table("transfer.transfer_tasks AS t").
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED", Table: clause.Table{Name: "t"}}).
		Select("t.*").
		Joins(`JOIN LATERAL (
			SELECT e.execution_id, e.status, e.metadata, e.created_at, e.id
			FROM common.task_executions AS e
			WHERE e.module = ? AND e.task_type = ? AND e.source_task_id = CAST(t.id AS TEXT)
			ORDER BY e.created_at DESC, e.id DESC
			LIMIT 1
		) AS last_execution ON TRUE`, commonExecution.ModuleTransfer, commonExecution.TaskTypeSync).
		Joins("LEFT JOIN transfer.runtime_leases AS l ON l.task_id = t.id").
		Where("t.desired_state = ? AND t.status <> ? AND t.deleted_at IS NULL", models.TaskDesiredStateRunning, models.TaskStatusBlocked).
		Where(`NOT EXISTS (
			SELECT 1 FROM common.task_executions AS active
			WHERE active.module = ? AND active.task_type = ? AND active.source_task_id = CAST(t.id AS TEXT)
			  AND active.status = ?
		)`, commonExecution.ModuleTransfer, commonExecution.TaskTypeSync, commonExecution.ExecutionStatusPending).
		Where(`(
			(last_execution.status = ? AND (l.id IS NULL OR l.lease_until <= ?))
			OR
			(last_execution.status = ? AND last_execution.metadata->>'stop_reason' = ?)
		)`, commonExecution.ExecutionStatusRunning, now, commonExecution.ExecutionStatusCancelled, "worker_shutdown").
		Order("last_execution.created_at ASC, last_execution.id ASC").
		Limit(1).
		Scan(&task).Error
	if err != nil {
		return false, err
	}
	if task.ID == 0 {
		return false, nil
	}

	pendingSchemaChange, err := pendingSchemaChangeRequestTx(tx, task.ID, task.TenantID)
	if err != nil {
		return false, err
	}
	var previous commonExecution.TaskExecution
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("module = ? AND task_type = ? AND source_task_id = ?", commonExecution.ModuleTransfer, commonExecution.TaskTypeSync, fmt.Sprint(task.ID)).
		Order("created_at DESC, id DESC").First(&previous).Error; err != nil {
		return false, err
	}
	if pendingSchemaChange != nil {
		if pendingSchemaChange.ExecutionID != previous.ExecutionID {
			return false, ErrSchemaChangeRequestConflict
		}
		metadata := schemaChangeProjectionMetadata(previous.Metadata, pendingSchemaChange, schemaChangeProjectionPending, now)
		metadata["stop_reason"] = "schema_change_blocked"
		result := tx.Model(&previous).Where("status IN ?", []string{commonExecution.ExecutionStatusRunning, commonExecution.ExecutionStatusCancelled}).Updates(map[string]interface{}{
			"status": commonExecution.ExecutionStatusFailed, "metadata": metadata,
			"error_details": commonModels.JSONMap{"message": "continuous runtime stopped by a pending schema change"},
			"completed_at":  now, "updated_at": now,
		})
		if result.Error != nil {
			return false, result.Error
		}
		if result.RowsAffected != 1 {
			return false, ErrRuntimeLeaseLost
		}
		if err := tx.Model(&models.RuntimeLease{}).Where("task_id = ?", task.ID).Updates(map[string]interface{}{
			"owner_instance_id": "", "lease_until": now, "heartbeat_at": now,
		}).Error; err != nil {
			return false, err
		}
		if err := tx.Model(&task).Updates(map[string]interface{}{
			"status": models.TaskStatusBlocked, "last_execution_status": commonExecution.ExecutionStatusFailed,
		}).Error; err != nil {
			return false, err
		}
		return false, nil
	}

	recoveryReason := ""
	sessionStartedAt := executionSessionStartedAt(previous, time.Time{})
	switch previous.Status {
	case commonExecution.ExecutionStatusRunning:
		var lease models.RuntimeLease
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("task_id = ?", task.ID).First(&lease).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return false, err
		}
		if err == nil && lease.LeaseUntil.After(now) {
			return false, nil
		}
		if err == nil {
			sessionStartedAt = executionSessionStartedAt(previous, lease.ClaimedAt)
		}
		recoveryReason = continuousRecoveryReasonLeaseExpired
		metadata := previous.Metadata
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		metadata["stop_reason"] = recoveryReason
		result := tx.Model(&previous).Where("status = ?", commonExecution.ExecutionStatusRunning).Updates(map[string]interface{}{
			"status": commonExecution.ExecutionStatusFailed, "metadata": metadata,
			"error_details": commonModels.JSONMap{"message": "continuous runtime lease expired"},
			"completed_at":  now, "updated_at": now,
		})
		if result.Error != nil {
			return false, result.Error
		}
		if result.RowsAffected != 1 {
			return false, ErrRuntimeLeaseLost
		}
	case commonExecution.ExecutionStatusCancelled:
		if previous.Metadata["stop_reason"] != continuousRecoveryReasonWorkerShutdown {
			return false, nil
		}
		recoveryReason = continuousRecoveryReasonWorkerShutdown
	default:
		return false, nil
	}

	if _, err := createRecoveryExecution(tx, task, previous, recoveryReason, sessionStartedAt, now, policy); err != nil {
		return false, err
	}
	return true, nil
}

func createRecoveryExecution(tx *gorm.DB, task models.TransferTask, previous commonExecution.TaskExecution, reason string, sessionStartedAt, now time.Time, policy ContinuousRecoveryPolicy) (*commonExecution.TaskExecution, error) {
	plan := buildContinuousRecoveryPlan(previous, reason, sessionStartedAt, now, policy)
	taskName := task.Name
	recoveryExecution := &commonExecution.TaskExecution{
		TenantID: int(task.TenantID), ExecutionID: uuid.NewString(), Module: commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync, Source: commonExecution.ModuleTransfer,
		SourceTaskID: commonExecution.NewSourceTaskIDFromUint(task.ID), SourceTaskName: &taskName,
		Status: commonExecution.ExecutionStatusPending, TriggerType: previous.TriggerType,
		ExecutionConfig: task.Config,
		Metadata: commonModels.JSONMap{
			"recovery_reason":               reason,
			"recovered_from_execution_id":   previous.ExecutionID,
			"recovery_attempt":              plan.Attempt,
			"recovery_consecutive_failures": plan.Attempt,
			"recovery_not_before":           plan.NotBefore,
			"recovery_backoff_seconds":      plan.Backoff.Seconds(),
			"recovery_circuit_state":        plan.CircuitState,
		},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(recoveryExecution).Error; err != nil {
		return nil, err
	}
	if err := tx.Model(&task).Updates(map[string]interface{}{
		"status": models.TaskStatusIdle, "progress": 0,
		"last_execution_id": recoveryExecution.ExecutionID, "last_execution_status": commonExecution.ExecutionStatusPending,
	}).Error; err != nil {
		return nil, err
	}
	return recoveryExecution, nil
}

func executionSessionStartedAt(execution commonExecution.TaskExecution, fallback time.Time) time.Time {
	if value, ok := execution.Metadata["recovery_claimed_at"]; ok {
		switch typed := value.(type) {
		case time.Time:
			return typed
		case string:
			if parsed, err := time.Parse(time.RFC3339Nano, typed); err == nil {
				return parsed
			}
		}
	}
	if !fallback.IsZero() {
		return fallback
	}
	if execution.StartedAt != nil {
		return *execution.StartedAt
	}
	return time.Time{}
}

func (r *RuntimeLeaseRepository) Renew(ctx context.Context, taskID uint, owner string, token uint64, now time.Time, duration time.Duration) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		leaseUntil := now.Add(duration)
		result := tx.Model(&models.RuntimeLease{}).
			Where("task_id = ? AND owner_instance_id = ? AND fencing_token = ?", taskID, owner, token).
			Where("EXISTS (SELECT 1 FROM transfer.transfer_tasks t WHERE t.id = ? AND t.desired_state = ?)", taskID, models.TaskDesiredStateRunning).
			Updates(map[string]interface{}{"lease_until": leaseUntil, "heartbeat_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrRuntimeLeaseLost
		}
		var lease models.RuntimeLease
		if err := tx.Where("task_id = ? AND owner_instance_id = ? AND fencing_token = ?", taskID, owner, token).First(&lease).Error; err != nil {
			return ErrRuntimeLeaseLost
		}
		var execution commonExecution.TaskExecution
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("execution_id = ? AND status = ?", lease.ExecutionID, commonExecution.ExecutionStatusRunning).
			First(&execution).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRuntimeLeaseLost
			}
			return err
		}
		metadata := mergeContinuousRuntimeMetadata(execution.Metadata, owner, token, now, leaseUntil)
		return tx.Model(&execution).Updates(map[string]interface{}{"metadata": metadata, "updated_at": now}).Error
	})
}

func mergeContinuousRuntimeMetadata(metadata commonModels.JSONMap, owner string, token uint64, heartbeatAt, leaseUntil time.Time) commonModels.JSONMap {
	if metadata == nil {
		metadata = commonModels.JSONMap{}
	}
	continuousMeta, _ := metadata["continuous"].(map[string]interface{})
	if continuousMeta == nil {
		continuousMeta = map[string]interface{}{}
	}
	continuousMeta["owner_instance_id"] = owner
	continuousMeta["fencing_token"] = token
	continuousMeta["heartbeat_at"] = heartbeatAt
	continuousMeta["lease_until"] = leaseUntil
	metadata["continuous"] = continuousMeta
	return metadata
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
		var task models.TransferTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", claim.Task.ID).First(&task).Error; err != nil {
			return err
		}
		var stoppedSchemaChange *models.SchemaChangeRequest
		if stopReason == "schema_change_blocked" && task.DesiredState == models.TaskDesiredStateStopped {
			request, err := pendingSchemaChangeRequestTx(tx, task.ID, task.TenantID)
			if err != nil {
				return err
			}
			stoppedSchemaChange = request
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
		if stoppedSchemaChange != nil {
			metadata = schemaChangeProjectionMetadata(metadata, stoppedSchemaChange, schemaChangeProjectionStopped, now)
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
		taskStatus := models.TaskStatusIdle
		if stopReason == "schema_change_blocked" {
			if task.DesiredState != models.TaskDesiredStateStopped {
				taskStatus = models.TaskStatusBlocked
			}
		}
		if task.DesiredState == models.TaskDesiredStateRunning && stopReason != "schema_change_blocked" {
			recoveryReason := ""
			switch {
			case status == commonExecution.ExecutionStatusFailed:
				recoveryReason = continuousRecoveryReasonExecutionFailed
			case status == commonExecution.ExecutionStatusCancelled && stopReason == continuousRecoveryReasonWorkerShutdown:
				recoveryReason = continuousRecoveryReasonWorkerShutdown
			}
			if recoveryReason != "" {
				_, err := createRecoveryExecution(tx, task, execution, recoveryReason, executionSessionStartedAt(execution, claim.Lease.ClaimedAt), now, r.recoveryPolicy)
				return err
			}
		}
		return tx.Model(&task).Updates(map[string]interface{}{
			"status": taskStatus, "last_execution_status": status,
		}).Error
	})
}

func (r *RuntimeLeaseRepository) RecordProgress(ctx context.Context, claim RuntimeLeaseClaim, progress ContinuousProgress) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var leaseCount int64
		if err := tx.Model(&models.RuntimeLease{}).
			Where("task_id = ? AND owner_instance_id = ? AND fencing_token = ? AND lease_until > CURRENT_TIMESTAMP", claim.Task.ID, claim.Lease.OwnerInstanceID, claim.Lease.FencingToken).
			Where("EXISTS (SELECT 1 FROM transfer.transfer_tasks t WHERE t.id = ? AND t.desired_state = ?)", claim.Task.ID, models.TaskDesiredStateRunning).
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
		if progress.LastEventAt != nil {
			continuousMeta["last_event_at"] = *progress.LastEventAt
		}
		metadata["continuous"] = continuousMeta
		metadata["recovery_consecutive_failures"] = 0
		metadata["recovery_circuit_state"] = recoveryCircuitClosed
		return tx.Model(&execution).Updates(map[string]interface{}{
			"records_read": progress.RecordsRead, "records_written": progress.RecordsWritten,
			"metadata": metadata, "updated_at": progress.CommittedAt,
		}).Error
	})
}

func (r *RuntimeLeaseRepository) RecordDiagnostics(ctx context.Context, claim RuntimeLeaseClaim, diagnostics ContinuousDiagnostics) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var leaseCount int64
		if err := tx.Model(&models.RuntimeLease{}).
			Where("task_id = ? AND owner_instance_id = ? AND fencing_token = ? AND lease_until > CURRENT_TIMESTAMP", claim.Task.ID, claim.Lease.OwnerInstanceID, claim.Lease.FencingToken).
			Where("EXISTS (SELECT 1 FROM transfer.transfer_tasks t WHERE t.id = ? AND t.desired_state = ?)", claim.Task.ID, models.TaskDesiredStateRunning).
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
		continuousMeta["diagnostics"] = diagnostics
		metadata["continuous"] = continuousMeta
		return tx.Model(&execution).Updates(map[string]interface{}{
			"metadata": metadata, "updated_at": diagnostics.SampledAt,
		}).Error
	})
}

func (r *RuntimeLeaseRepository) RecordSchemaChange(ctx context.Context, claim RuntimeLeaseClaim, change ContinuousSchemaChange) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var leaseCount int64
		if err := tx.Model(&models.RuntimeLease{}).
			Where("task_id = ? AND owner_instance_id = ? AND fencing_token = ? AND lease_until > CURRENT_TIMESTAMP", claim.Task.ID, claim.Lease.OwnerInstanceID, claim.Lease.FencingToken).
			Where("EXISTS (SELECT 1 FROM transfer.transfer_tasks t WHERE t.id = ? AND t.desired_state = ?)", claim.Task.ID, models.TaskDesiredStateRunning).
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
		var resource models.CaptureResource
		if err := tx.Where("task_id = ? AND tenant_id = ?", claim.Task.ID, claim.Task.TenantID).
			Order("generation DESC").First(&resource).Error; err != nil {
			return fmt.Errorf("load capture generation for schema change: %w", err)
		}
		var pending models.SchemaChangeRequest
		err := tx.Where("capture_resource_id = ? AND status = ?", resource.ID, models.SchemaChangeRequestPending).
			First(&pending).Error
		if err == nil {
			if pending.ExecutionID == claim.Execution.ExecutionID &&
				pending.SourcePartition == change.SourcePartition && pending.SourceOffset == change.SourceOffset {
				return UpdateSchemaChangeExecutionProjectionTx(tx, &pending)
			}
			return ErrSchemaChangeRequestConflict
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		request := models.SchemaChangeRequest{
			TaskID: claim.Task.ID, TenantID: claim.Task.TenantID, CaptureResourceID: resource.ID,
			Generation: resource.Generation, ExecutionID: claim.Execution.ExecutionID,
			SourcePartition: change.SourcePartition, SourceOffset: change.SourceOffset,
			Scope: change.Scope, Diff: schemaChangeDiff(change), ApprovedMappings: models.JSONMap{},
			FromRevision: resource.SchemaRevision, ToRevision: resource.SchemaRevision + 1,
			Status: models.SchemaChangeRequestPending, DetectedAt: change.DetectedAt,
		}
		if err := tx.Create(&request).Error; err != nil {
			return err
		}
		return UpdateSchemaChangeExecutionProjectionTx(tx, &request)
	})
}

func (r *RuntimeLeaseRepository) DesiredState(ctx context.Context, taskID uint) (models.TaskDesiredState, error) {
	var task models.TransferTask
	if err := r.db.WithContext(ctx).Select("id", "desired_state").Where("id = ?", taskID).First(&task).Error; err != nil {
		return "", err
	}
	return task.DesiredState, nil
}
