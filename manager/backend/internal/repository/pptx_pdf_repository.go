package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

type PPTXPDFRepository struct{ db *gorm.DB }

func NewPPTXPDFRepository(db *gorm.DB) *PPTXPDFRepository { return &PPTXPDFRepository{db: db} }

func (r *PPTXPDFRepository) CreateTask(ctx context.Context, task *models.PPTXPDFTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *PPTXPDFRepository) GetTask(ctx context.Context, id, tenantID uint) (*models.PPTXPDFTask, error) {
	var task models.PPTXPDFTask
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *PPTXPDFRepository) GetTaskByFingerprint(ctx context.Context, tenantID uint, fingerprint string) (*models.PPTXPDFTask, error) {
	var task models.PPTXPDFTask
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND artifact_variant = ?", tenantID, strings.TrimSpace(fingerprint), models.PPTXPDFArtifactVariant).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *PPTXPDFRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.PPTXPDFTask, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.PPTXPDFTask{}).Where("tenant_id = ?", tenantID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.PPTXPDFTask
	err := query.Order("updated_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&tasks).Error
	return tasks, total, err
}

func (r *PPTXPDFRepository) SaveTask(ctx context.Context, task *models.PPTXPDFTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *PPTXPDFRepository) DeleteTask(ctx context.Context, id, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.PPTXPDFTask{}).Error
}

func (r *PPTXPDFRepository) ClaimExecution(ctx context.Context, taskID, tenantID uint, execution *commonExecution.TaskExecution, overwrite bool) (*models.PPTXPDFTask, error) {
	var task models.PPTXPDFTask
	err := newTaskExecutionLifecycle(r.db).Claim(ctx, taskID, tenantID, execution, taskExecutionClaimSpec{
		TaskModel: &task, TaskType: commonExecution.TaskTypePPTXPDFGeneration, TaskLabel: "PPTX PDF",
		TaskName: func() string { return task.Name }, TaskConfig: func() commonModels.JSONMap { return task.Config },
		CurrentResultModel: &models.PPTXPDF{}, ExcludedResultStatuses: []string{models.PPTXPDFStatusDeleted},
		OverwriteExistingResult: overwrite,
	})
	if err != nil {
		return nil, err
	}
	status := commonExecution.ExecutionStatusPending
	task.LastExecutionID = &execution.ExecutionID
	task.LastExecutionStatus = &status
	return &task, nil
}

func (r *PPTXPDFRepository) ClaimPendingExecution(ctx context.Context, workerID string, now time.Time, leaseDuration time.Duration) (*commonExecution.TaskExecution, *commonExecution.Lease, *models.PPTXPDFTask, error) {
	var execution *commonExecution.TaskExecution
	var lease *commonExecution.Lease
	var task models.PPTXPDFTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		execution, lease, err = commonExecution.ClaimNext(ctx, tx, commonExecution.ClaimOptions{
			Module: commonExecution.ModuleManager, TaskType: commonExecution.TaskTypePPTXPDFGeneration,
			WorkerID: workerID, Now: now, LeaseDuration: leaseDuration,
		})
		if err != nil || execution == nil {
			return err
		}
		taskID, err := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
		if err != nil {
			return fmt.Errorf("PPTX PDF execution %s source task: %w", execution.ExecutionID, err)
		}
		if err := tx.Where("id = ? AND tenant_id = ?", taskID, execution.TenantID).First(&task).Error; err != nil {
			return err
		}
		result := tx.Model(&models.PPTXPDFTask{}).
			Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?", task.ID, task.TenantID, execution.ExecutionID, commonExecution.ExecutionStatusPending).
			Updates(map[string]interface{}{"last_run_at": now.UTC(), "last_execution_status": commonExecution.ExecutionStatusRunning})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: PPTX PDF task %d summary no longer matches execution %s", commonAPI.ErrConflict, task.ID, execution.ExecutionID)
		}
		return nil
	})
	if err != nil || execution == nil {
		return nil, nil, nil, err
	}
	status := commonExecution.ExecutionStatusRunning
	task.LastExecutionStatus = &status
	task.LastRunAt = &now
	return execution, lease, &task, nil
}

func (r *PPTXPDFRepository) RenewExecutionLease(ctx context.Context, lease commonExecution.Lease, expiresAt time.Time) error {
	return commonExecution.RenewLease(ctx, r.db, lease, expiresAt)
}

func (r *PPTXPDFRepository) ExecutionAttemptIsTerminal(ctx context.Context, lease commonExecution.Lease) (bool, error) {
	return commonExecution.AttemptIsTerminal(ctx, r.db, lease)
}

func (r *PPTXPDFRepository) CompleteExecutionWithLease(ctx context.Context, taskID, tenantID uint, lease commonExecution.Lease, resultID uint, resultFields, executionFields map[string]interface{}, completedAt time.Time) error {
	status, _ := executionFields["status"].(string)
	fields := make(map[string]interface{}, len(executionFields))
	for key, value := range executionFields {
		fields[key] = value
	}
	delete(fields, "status")
	delete(fields, "completed_at")
	delete(fields, "updated_at")
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := commonExecution.CompleteWithLease(ctx, tx, lease, status, completedAt, fields); err != nil {
			return err
		}
		if resultID > 0 && len(resultFields) > 0 {
			result := tx.Model(&models.PPTXPDF{}).
				Where("id = ? AND tenant_id = ? AND last_execution_id = ?", resultID, tenantID, lease.ExecutionID).
				Updates(resultFields)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("%w: PPTX PDF result %d is not owned by execution %s", commonAPI.ErrConflict, resultID, lease.ExecutionID)
			}
		}
		result := tx.Model(&models.PPTXPDFTask{}).
			Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?", taskID, tenantID, lease.ExecutionID, commonExecution.ExecutionStatusRunning).
			Updates(map[string]interface{}{"last_execution_status": status, "updated_at": completedAt.UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("%w: PPTX PDF task %d is not owned by execution %s", commonAPI.ErrConflict, taskID, lease.ExecutionID)
		}
		return nil
	})
}

func (r *PPTXPDFRepository) RecoverExpiredExecutions(ctx context.Context, now time.Time, limit int) (int, error) {
	const errorCode = "manager.pptx_pdf.lease_expired"
	const errorMessage = "PPTX PDF worker lease expired; execution is not replayed automatically"
	recovered := 0
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		executions, err := commonExecution.FindExpiredForUpdate(ctx, tx, commonExecution.ExpiredOptions{
			Module: commonExecution.ModuleManager, TaskType: commonExecution.TaskTypePPTXPDFGeneration,
			Now: now, Limit: limit,
		})
		if err != nil {
			return err
		}
		for i := range executions {
			execution := executions[i]
			lease, err := commonExecution.LeaseFromExecution(execution)
			if err != nil {
				return err
			}
			fields := map[string]interface{}{"error_details": commonModels.JSONMap{"code": errorCode, "message": errorMessage}}
			if execution.StartedAt != nil {
				fields["execution_time_ms"] = now.Sub(*execution.StartedAt).Milliseconds()
			}
			if err := commonExecution.FailExpired(ctx, tx, lease, now, fields); err != nil {
				return err
			}
			taskID, err := commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
			if err != nil {
				return err
			}
			result := tx.Model(&models.PPTXPDFTask{}).
				Where("id = ? AND tenant_id = ? AND last_execution_id = ? AND last_execution_status = ?", taskID, execution.TenantID, execution.ExecutionID, commonExecution.ExecutionStatusRunning).
				Updates(map[string]interface{}{"last_execution_status": commonExecution.ExecutionStatusFailed, "updated_at": now.UTC()})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return fmt.Errorf("%w: PPTX PDF task %d summary no longer matches expired execution %s", commonAPI.ErrConflict, taskID, execution.ExecutionID)
			}
			if err := tx.Model(&models.PPTXPDF{}).
				Where("tenant_id = ? AND last_execution_id = ? AND status = ?", execution.TenantID, execution.ExecutionID, models.PPTXPDFStatusBuilding).
				Updates(map[string]interface{}{"status": models.PPTXPDFStatusFailed, "error_message": errorMessage, "updated_at": now.UTC()}).Error; err != nil {
				return err
			}
			recovered++
		}
		return nil
	})
	return recovered, err
}

func (r *PPTXPDFRepository) Current(ctx context.Context, tenantID uint, fingerprint string) (*models.PPTXPDF, error) {
	var result models.PPTXPDF
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND artifact_variant = ? AND status <> ?", tenantID, fingerprint, models.PPTXPDFArtifactVariant, models.PPTXPDFStatusDeleted).
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *PPTXPDFRepository) CreateResult(ctx context.Context, result *models.PPTXPDF) error {
	return r.db.WithContext(ctx).Create(result).Error
}

func (r *PPTXPDFRepository) UpdateResult(ctx context.Context, id, tenantID uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&models.PPTXPDF{}).Where("id = ? AND tenant_id = ?", id, tenantID).Updates(fields).Error
}

func (r *PPTXPDFRepository) GetResult(ctx context.Context, id, tenantID uint) (*models.PPTXPDF, error) {
	var result models.PPTXPDF
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *PPTXPDFRepository) DeleteResult(ctx context.Context, id, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.PPTXPDF{}).Error
}
