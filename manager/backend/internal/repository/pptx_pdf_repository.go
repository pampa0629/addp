package repository

import (
	"context"
	"errors"
	"strings"
	"time"

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

func (r *PPTXPDFRepository) StartExecution(ctx context.Context, taskID, tenantID uint, executionID string, startedAt time.Time) error {
	return newTaskExecutionLifecycle(r.db).Start(ctx, taskID, tenantID, executionID, startedAt, &models.PPTXPDFTask{}, "PPTX PDF")
}

func (r *PPTXPDFRepository) CompleteExecution(ctx context.Context, taskID, tenantID uint, executionID string, resultID uint, resultFields, executionFields map[string]interface{}, completedAt time.Time) error {
	return newTaskExecutionLifecycle(r.db).Complete(ctx, taskID, tenantID, executionID, completedAt, taskExecutionCompletionSpec{
		TaskModel: &models.PPTXPDFTask{}, ResultModel: &models.PPTXPDF{}, ResultID: resultID,
		ResultFields: resultFields, ExecutionFields: executionFields,
	}, "PPTX PDF")
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
