package repository

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DataProfileExecutionRepository struct {
	db *gorm.DB
}

func NewDataProfileExecutionRepository(db *gorm.DB) *DataProfileExecutionRepository {
	return &DataProfileExecutionRepository{db: db}
}

func (r *DataProfileExecutionRepository) CreateOrReuseActive(
	ctx context.Context,
	targetKey string,
	execution *commonExecution.TaskExecution,
) (*commonExecution.TaskExecution, bool, error) {
	if execution == nil {
		return nil, false, errors.New("execution is required")
	}
	var result *commonExecution.TaskExecution
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", dataProfileLockID(execution.TenantID, targetKey)).Error; err != nil {
				return fmt.Errorf("lock data profile execution target: %w", err)
			}
		}
		active, err := findActiveDataProfileExecution(tx, execution.TenantID, targetKey)
		if err != nil {
			return err
		}
		if active != nil {
			result = active
			return nil
		}
		if err := tx.Create(execution).Error; err != nil {
			return err
		}
		result = execution
		created = true
		return nil
	})
	return result, created, err
}

func (r *DataProfileExecutionRepository) GetActive(
	ctx context.Context,
	tenantID int,
	targetKey string,
) (*commonExecution.TaskExecution, error) {
	return findActiveDataProfileExecution(r.db.WithContext(ctx), tenantID, targetKey)
}

func (r *DataProfileExecutionRepository) GetLatest(
	ctx context.Context,
	tenantID int,
	targetKey string,
) (*commonExecution.TaskExecution, error) {
	var execution commonExecution.TaskExecution
	query := r.db.WithContext(ctx).
		Where(
			"tenant_id = ? AND module = ? AND task_type = ?",
			tenantID,
			commonExecution.ModuleManager,
			commonExecution.TaskTypeDataProfiling,
		)
	if r.db.Dialector.Name() == "postgres" {
		query = query.Where("execution_config ->> 'target_key' = ?", targetKey)
	} else {
		query = query.Where("json_extract(execution_config, '$.target_key') = ?", targetKey)
	}
	err := query.Order("created_at DESC").First(&execution).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &execution, err
}

func (r *DataProfileExecutionRepository) GetByExecutionID(
	ctx context.Context,
	tenantID int,
	executionID string,
) (*commonExecution.TaskExecution, error) {
	var execution commonExecution.TaskExecution
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND execution_id = ?", tenantID, executionID).
		First(&execution).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &execution, err
}

func (r *DataProfileExecutionRepository) Start(
	ctx context.Context,
	tenantID int,
	executionID string,
	startedAt time.Time,
) error {
	result := r.db.WithContext(ctx).
		Model(&commonExecution.TaskExecution{}).
		Where("tenant_id = ? AND execution_id = ? AND status = ?", tenantID, executionID, commonExecution.ExecutionStatusPending).
		Updates(map[string]interface{}{
			"status":     commonExecution.ExecutionStatusRunning,
			"started_at": startedAt,
			"updated_at": startedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("data profile execution is not pending")
	}
	return nil
}

func (r *DataProfileExecutionRepository) Complete(
	ctx context.Context,
	tenantID int,
	executionID string,
	startedAt time.Time,
	rowsRead int64,
	metadata map[string]interface{},
) error {
	completedAt := time.Now().UTC()
	return r.updateTerminal(ctx, tenantID, executionID, commonExecution.ExecutionStatusSuccess, map[string]interface{}{
		"completed_at":      completedAt,
		"updated_at":        completedAt,
		"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(),
		"records_read":      rowsRead,
		"metadata":          commonModels.JSONMap(metadata),
		"progress":          100,
	})
}

func (r *DataProfileExecutionRepository) Fail(
	ctx context.Context,
	tenantID int,
	executionID string,
	startedAt time.Time,
	errorCode string,
	errorMessage string,
) error {
	return r.failWithStatus(ctx, tenantID, executionID, startedAt, commonExecution.ExecutionStatusFailed, errorCode, errorMessage)
}

func (r *DataProfileExecutionRepository) Timeout(
	ctx context.Context,
	tenantID int,
	executionID string,
	startedAt time.Time,
	errorCode string,
	errorMessage string,
) error {
	return r.failWithStatus(ctx, tenantID, executionID, startedAt, commonExecution.ExecutionStatusTimeout, errorCode, errorMessage)
}

func (r *DataProfileExecutionRepository) failWithStatus(
	ctx context.Context,
	tenantID int,
	executionID string,
	startedAt time.Time,
	status string,
	errorCode string,
	errorMessage string,
) error {
	completedAt := time.Now().UTC()
	return r.updateTerminal(ctx, tenantID, executionID, status, map[string]interface{}{
		"completed_at":      completedAt,
		"updated_at":        completedAt,
		"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(),
		"error_details": commonModels.JSONMap{
			"code":    errorCode,
			"message": errorMessage,
		},
	})
}

func (r *DataProfileExecutionRepository) updateTerminal(
	ctx context.Context,
	tenantID int,
	executionID string,
	status string,
	fields map[string]interface{},
) error {
	fields["status"] = status
	result := r.db.WithContext(ctx).
		Model(&commonExecution.TaskExecution{}).
		Where(
			"tenant_id = ? AND execution_id = ? AND status IN ?",
			tenantID,
			executionID,
			[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning},
		).
		Updates(fields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("data profile execution is not active")
	}
	return nil
}

func findActiveDataProfileExecution(db *gorm.DB, tenantID int, targetKey string) (*commonExecution.TaskExecution, error) {
	var execution commonExecution.TaskExecution
	query := db.
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(
			"tenant_id = ? AND module = ? AND task_type = ? AND status IN ?",
			tenantID,
			commonExecution.ModuleManager,
			commonExecution.TaskTypeDataProfiling,
			[]string{commonExecution.ExecutionStatusPending, commonExecution.ExecutionStatusRunning},
		)
	if db.Dialector.Name() == "postgres" {
		query = query.Where("execution_config ->> 'target_key' = ?", targetKey)
	} else {
		query = query.Where("json_extract(execution_config, '$.target_key') = ?", targetKey)
	}
	err := query.Order("created_at DESC").First(&execution).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &execution, err
}

func dataProfileLockID(tenantID int, targetKey string) int64 {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", tenantID, targetKey)))
	return int64(binary.BigEndian.Uint64(hash[:8]))
}
