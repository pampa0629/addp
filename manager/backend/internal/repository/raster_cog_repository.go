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

// RasterCOGRepository 维护栅格快显 COG 生成结果。
type RasterCOGRepository struct {
	db *gorm.DB
}

func NewRasterCOGRepository(db *gorm.DB) *RasterCOGRepository {
	return &RasterCOGRepository{db: db}
}

func (r *RasterCOGRepository) CreateTask(ctx context.Context, task *models.RasterCOGTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *RasterCOGRepository) GetTask(ctx context.Context, id uint, tenantID uint) (*models.RasterCOGTask, error) {
	var task models.RasterCOGTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *RasterCOGRepository) GetTaskByItemFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.RasterCOGTask, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var task models.RasterCOGTask
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND config->'target'->>'item_fingerprint' = ?", tenantID, itemFingerprint).
		Order("updated_at DESC, id DESC").
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *RasterCOGRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.RasterCOGTask, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.RasterCOGTask{}).
		Where("tenant_id = ?", tenantID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.RasterCOGTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

func (r *RasterCOGRepository) UpdateTask(ctx context.Context, task *models.RasterCOGTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *RasterCOGRepository) DeleteTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.RasterCOGTask{}).Error
}

func (r *RasterCOGRepository) ClaimExecution(
	ctx context.Context, taskID, tenantID uint, execution *commonExecution.TaskExecution, overwriteExistingResult bool,
) (*models.RasterCOGTask, error) {
	var task models.RasterCOGTask
	err := newTaskExecutionLifecycle(r.db).Claim(ctx, taskID, tenantID, execution, taskExecutionClaimSpec{
		TaskModel: &task,
		TaskType:  commonExecution.TaskTypeRasterCOGGeneration,
		TaskLabel: "raster COG",
		TaskName:  func() string { return task.Name },
		TaskConfig: func() commonModels.JSONMap {
			return task.Config
		},
		CurrentResultModel:      &models.RasterCOG{},
		OverwriteExistingResult: overwriteExistingResult,
	})
	if err != nil {
		return nil, err
	}
	task.LastExecutionID = &execution.ExecutionID
	status := commonExecution.ExecutionStatusPending
	task.LastExecutionStatus = &status
	return &task, nil
}

func (r *RasterCOGRepository) StartExecution(
	ctx context.Context, taskID, tenantID uint, executionID string, startedAt time.Time,
) error {
	return newTaskExecutionLifecycle(r.db).Start(
		ctx, taskID, tenantID, executionID, startedAt, &models.RasterCOGTask{}, "raster COG",
	)
}

func (r *RasterCOGRepository) CompleteExecution(
	ctx context.Context,
	taskID, tenantID uint,
	executionID string,
	resultID uint,
	resultFields map[string]interface{},
	executionFields map[string]interface{},
	completedAt time.Time,
) error {
	return newTaskExecutionLifecycle(r.db).Complete(ctx, taskID, tenantID, executionID, completedAt, taskExecutionCompletionSpec{
		TaskModel:       &models.RasterCOGTask{},
		ResultModel:     &models.RasterCOG{},
		ResultID:        resultID,
		ResultFields:    resultFields,
		ExecutionFields: executionFields,
	}, "raster COG")
}

func (r *RasterCOGRepository) Create(ctx context.Context, result *models.RasterCOG) error {
	return r.db.WithContext(ctx).Create(result).Error
}

func (r *RasterCOGRepository) GetByID(ctx context.Context, id uint, tenantID uint) (*models.RasterCOG, error) {
	var result models.RasterCOG
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

type RasterCOGFilter struct {
	TenantID        uint
	ItemID          uint
	ItemFingerprint string
	TaskID          uint
	Status          string
	Q               string
	Page            int
	PageSize        int
}

func (r *RasterCOGRepository) List(ctx context.Context, filter RasterCOGFilter) ([]*models.RasterCOG, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.RasterCOG{}).
		Where("tenant_id = ?", filter.TenantID)
	if filter.ItemID > 0 {
		query = query.Where("item_id = ?", filter.ItemID)
	}
	if itemFingerprint := strings.TrimSpace(filter.ItemFingerprint); itemFingerprint != "" {
		query = query.Where("item_fingerprint = ?", itemFingerprint)
	}
	if filter.TaskID > 0 {
		query = query.Where("task_id = ?", filter.TaskID)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		query = query.Where("status = ?", status)
	}
	if q := strings.TrimSpace(filter.Q); q != "" {
		like := "%" + q + "%"
		query = query.Where(
			"locator ILIKE ? OR item_fingerprint ILIKE ? OR file_name ILIKE ? OR error_message ILIKE ?",
			like, like, like, like,
		)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize := normalizePage(filter.Page, filter.PageSize)
	var results []*models.RasterCOG
	err := query.
		Order("updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&results).Error
	return results, total, err
}

func (r *RasterCOGRepository) GetCurrentByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.RasterCOG, error) {
	var result models.RasterCOG
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status <> ?", tenantID, strings.TrimSpace(itemFingerprint), models.RasterCOGStatusDeleted).
		Order("updated_at DESC, id DESC").
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *RasterCOGRepository) GetLatestReadyByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.RasterCOG, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var result models.RasterCOG
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status = ?", tenantID, itemFingerprint, models.RasterCOGStatusReady).
		Order("updated_at DESC, id DESC").
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *RasterCOGRepository) UpdateFields(ctx context.Context, id uint, tenantID uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).
		Model(&models.RasterCOG{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(fields).Error
}

func (r *RasterCOGRepository) MarkStale(ctx context.Context, id uint, tenantID uint, reason string) error {
	return r.db.WithContext(ctx).
		Model(&models.RasterCOG{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(map[string]interface{}{
			"status":        models.RasterCOGStatusStale,
			"error_message": strings.TrimSpace(reason),
		}).Error
}

func (r *RasterCOGRepository) Delete(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.RasterCOG{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			Update("status", models.RasterCOGStatusDeleted).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND tenant_id = ?", id, tenantID).
			Delete(&models.RasterCOG{}).Error
	})
}
