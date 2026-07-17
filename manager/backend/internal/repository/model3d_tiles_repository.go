package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// Model3DTilesRepository 维护分块三维模型瓦片任务定义和受管结果。
type Model3DTilesRepository struct {
	db *gorm.DB
}

func (r *Model3DTilesRepository) GetTaskByItemFingerprintAndFormat(ctx context.Context, tenantID uint, itemFingerprint, targetFormat string) (*models.Model3DTilesTask, error) {
	var task models.Model3DTilesTask
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND config->'source'->>'item_fingerprint' = ? AND config->>'target_format' = ?", tenantID, strings.TrimSpace(itemFingerprint), strings.TrimSpace(targetFormat)).
		Order("updated_at DESC, id DESC").First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func NewModel3DTilesRepository(db *gorm.DB) *Model3DTilesRepository {
	return &Model3DTilesRepository{db: db}
}

func (r *Model3DTilesRepository) CreateResult(ctx context.Context, result *models.Model3DTiles) error {
	return r.db.WithContext(ctx).Create(result).Error
}

func (r *Model3DTilesRepository) GetResult(ctx context.Context, id, tenantID uint) (*models.Model3DTiles, error) {
	var result models.Model3DTiles
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *Model3DTilesRepository) GetCurrentResult(ctx context.Context, tenantID uint, itemFingerprint, targetFormat string) (*models.Model3DTiles, error) {
	var result models.Model3DTiles
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND target_format = ? AND status <> ?", tenantID, strings.TrimSpace(itemFingerprint), strings.TrimSpace(targetFormat), models.Model3DTilesStatusDeleted).
		Order("updated_at DESC, id DESC").First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *Model3DTilesRepository) ListCurrentResults(ctx context.Context, tenantID uint, itemFingerprint string) ([]*models.Model3DTiles, error) {
	var results []*models.Model3DTiles
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status <> ?", tenantID, strings.TrimSpace(itemFingerprint), models.Model3DTilesStatusDeleted).
		Order("target_format ASC, updated_at DESC").Find(&results).Error
	return results, err
}

type Model3DTilesFilter struct {
	TenantID        uint
	ItemID          uint
	ItemFingerprint string
	TaskID          uint
	TargetFormat    string
	Status          string
	Q               string
	Page            int
	PageSize        int
}

func (r *Model3DTilesRepository) ListResults(ctx context.Context, filter Model3DTilesFilter) ([]*models.Model3DTiles, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.Model3DTiles{}).Where("tenant_id = ?", filter.TenantID)
	if filter.ItemID > 0 {
		query = query.Where("item_id = ?", filter.ItemID)
	}
	if value := strings.TrimSpace(filter.ItemFingerprint); value != "" {
		query = query.Where("item_fingerprint = ?", value)
	}
	if filter.TaskID > 0 {
		query = query.Where("task_id = ?", filter.TaskID)
	}
	if value := strings.TrimSpace(filter.TargetFormat); value != "" {
		query = query.Where("target_format = ?", value)
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		query = query.Where("status = ?", value)
	}
	if value := strings.ToLower(strings.TrimSpace(filter.Q)); value != "" {
		pattern := "%" + value + "%"
		query = query.Where("LOWER(locator) LIKE ? OR LOWER(item_fingerprint) LIKE ? OR LOWER(manifest_ref) LIKE ?", pattern, pattern, pattern)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	var results []*models.Model3DTiles
	err := query.Order("updated_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&results).Error
	return results, total, err
}

func (r *Model3DTilesRepository) MarkOtherFingerprintResultsStale(ctx context.Context, tenantID uint, locator, currentFingerprint string) error {
	locator, currentFingerprint = strings.TrimSpace(locator), strings.TrimSpace(currentFingerprint)
	if locator == "" || currentFingerprint == "" {
		return nil
	}
	return r.db.WithContext(ctx).Model(&models.Model3DTiles{}).
		Where("tenant_id = ? AND locator = ? AND item_fingerprint <> ? AND status NOT IN ?", tenantID, locator, currentFingerprint, []string{models.Model3DTilesStatusStale, models.Model3DTilesStatusDeleted}).
		Updates(map[string]interface{}{"status": models.Model3DTilesStatusStale, "updated_at": time.Now()}).Error
}

func (r *Model3DTilesRepository) UpdateResultFields(ctx context.Context, id, tenantID uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).Model(&models.Model3DTiles{}).Where("id = ? AND tenant_id = ?", id, tenantID).Updates(fields).Error
}

func (r *Model3DTilesRepository) DeleteResult(ctx context.Context, id, tenantID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Model3DTiles{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			Update("status", models.Model3DTilesStatusDeleted).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.Model3DTiles{}).Error
	})
}

func (r *Model3DTilesRepository) CreateTask(ctx context.Context, task *models.Model3DTilesTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *Model3DTilesRepository) ClaimExecution(
	ctx context.Context, taskID, tenantID uint, execution *commonExecution.TaskExecution, confirmExistingResult bool,
) (*models.Model3DTilesTask, error) {
	var task models.Model3DTilesTask
	err := newTaskExecutionLifecycle(r.db).Claim(ctx, taskID, tenantID, execution, taskExecutionClaimSpec{
		TaskModel: &task, TaskType: commonExecution.TaskTypeModel3DTilesGeneration, TaskLabel: "model3d tiles",
		TaskName: func() string { return task.Name }, TaskConfig: func() commonModels.JSONMap { return task.Config },
		CurrentResultModel: &models.Model3DTiles{}, ConfirmExistingResult: confirmExistingResult,
	})
	if err != nil {
		return nil, err
	}
	task.LastExecutionID = &execution.ExecutionID
	status := commonExecution.ExecutionStatusPending
	task.LastExecutionStatus = &status
	return &task, nil
}

func (r *Model3DTilesRepository) StartExecution(ctx context.Context, taskID, tenantID uint, executionID string, startedAt time.Time) error {
	return newTaskExecutionLifecycle(r.db).Start(ctx, taskID, tenantID, executionID, startedAt, &models.Model3DTilesTask{}, "model3d tiles")
}

func (r *Model3DTilesRepository) CompleteExecution(
	ctx context.Context, taskID, tenantID uint, executionID string, resultID uint,
	resultFields, executionFields map[string]interface{}, completedAt time.Time,
) error {
	return newTaskExecutionLifecycle(r.db).Complete(ctx, taskID, tenantID, executionID, completedAt, taskExecutionCompletionSpec{
		TaskModel: &models.Model3DTilesTask{}, ResultModel: &models.Model3DTiles{}, ResultID: resultID,
		ResultFields: resultFields, ExecutionFields: executionFields,
	}, "model3d tiles")
}

func (r *Model3DTilesRepository) GetExecution(ctx context.Context, executionID string, tenantID uint) (*commonExecution.TaskExecution, error) {
	var execution commonExecution.TaskExecution
	err := r.db.WithContext(ctx).Where("execution_id = ? AND tenant_id = ?", executionID, int(tenantID)).First(&execution).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, commonAPI.ErrNotFound
	}
	return &execution, err
}

func (r *Model3DTilesRepository) GetTask(ctx context.Context, id uint, tenantID uint) (*models.Model3DTilesTask, error) {
	var task models.Model3DTilesTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *Model3DTilesRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.Model3DTilesTask, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.Model3DTilesTask{}).
		Where("tenant_id = ?", tenantID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.Model3DTilesTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

func (r *Model3DTilesRepository) UpdateTask(ctx context.Context, task *models.Model3DTilesTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *Model3DTilesRepository) DeleteTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.Model3DTilesTask{}).Error
}
