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

// PointCloudCOPCRepository 维护点云 COPC 快显任务定义和结果。
type PointCloudCOPCRepository struct {
	db *gorm.DB
}

func NewPointCloudCOPCRepository(db *gorm.DB) *PointCloudCOPCRepository {
	return &PointCloudCOPCRepository{db: db}
}

func (r *PointCloudCOPCRepository) CreateTask(ctx context.Context, task *models.PointCloudCOPCTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *PointCloudCOPCRepository) GetTask(ctx context.Context, id uint, tenantID uint) (*models.PointCloudCOPCTask, error) {
	var task models.PointCloudCOPCTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *PointCloudCOPCRepository) GetTaskByItemFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.PointCloudCOPCTask, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var task models.PointCloudCOPCTask
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND config->'source'->>'item_fingerprint' = ?", tenantID, itemFingerprint).
		Order("updated_at DESC, id DESC").
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *PointCloudCOPCRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.PointCloudCOPCTask, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.PointCloudCOPCTask{}).
		Where("tenant_id = ?", tenantID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.PointCloudCOPCTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

func (r *PointCloudCOPCRepository) UpdateTask(ctx context.Context, task *models.PointCloudCOPCTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *PointCloudCOPCRepository) DeleteTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.PointCloudCOPCTask{}).Error
}

func (r *PointCloudCOPCRepository) ClaimExecution(
	ctx context.Context, taskID, tenantID uint, execution *commonExecution.TaskExecution, overwriteExistingResult bool,
) (*models.PointCloudCOPCTask, error) {
	var task models.PointCloudCOPCTask
	err := newTaskExecutionLifecycle(r.db).Claim(ctx, taskID, tenantID, execution, taskExecutionClaimSpec{
		TaskModel: &task,
		TaskType:  commonExecution.TaskTypePointCloudCOPCGeneration,
		TaskLabel: "point cloud COPC",
		TaskName:  func() string { return task.Name },
		TaskConfig: func() commonModels.JSONMap {
			return task.Config
		},
		CurrentResultModel:      &models.PointCloudCOPC{},
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

func (r *PointCloudCOPCRepository) StartExecution(
	ctx context.Context, taskID, tenantID uint, executionID string, startedAt time.Time,
) error {
	return newTaskExecutionLifecycle(r.db).Start(
		ctx, taskID, tenantID, executionID, startedAt, &models.PointCloudCOPCTask{}, "point cloud COPC",
	)
}

func (r *PointCloudCOPCRepository) CompleteExecution(
	ctx context.Context,
	taskID, tenantID uint,
	executionID string,
	resultID uint,
	resultFields map[string]interface{},
	executionFields map[string]interface{},
	completedAt time.Time,
) error {
	return newTaskExecutionLifecycle(r.db).Complete(ctx, taskID, tenantID, executionID, completedAt, taskExecutionCompletionSpec{
		TaskModel:       &models.PointCloudCOPCTask{},
		ResultModel:     &models.PointCloudCOPC{},
		ResultID:        resultID,
		ResultFields:    resultFields,
		ExecutionFields: executionFields,
	}, "point cloud COPC")
}

func (r *PointCloudCOPCRepository) GetExecution(
	ctx context.Context, tenantID uint, executionID string,
) (*commonExecution.TaskExecution, error) {
	var execution commonExecution.TaskExecution
	err := r.db.WithContext(ctx).
		Where("execution_id = ? AND tenant_id = ?", executionID, int(tenantID)).
		First(&execution).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, commonAPI.ErrNotFound
	}
	return &execution, err
}

func (r *PointCloudCOPCRepository) UpdateRunningExecutionProgress(
	ctx context.Context, tenantID uint, executionID string, fields map[string]interface{},
) error {
	return UpdateExecutionWithOwnership(ctx, r.db, executionID, int(tenantID), fields)
}

func (r *PointCloudCOPCRepository) Create(ctx context.Context, result *models.PointCloudCOPC) error {
	return r.db.WithContext(ctx).Create(result).Error
}

func (r *PointCloudCOPCRepository) GetByID(ctx context.Context, id uint, tenantID uint) (*models.PointCloudCOPC, error) {
	var result models.PointCloudCOPC
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

type PointCloudCOPCFilter struct {
	TenantID        uint
	ItemID          uint
	ItemFingerprint string
	TaskID          uint
	Status          string
	Q               string
	Page            int
	PageSize        int
}

func (r *PointCloudCOPCRepository) List(ctx context.Context, filter PointCloudCOPCFilter) ([]*models.PointCloudCOPC, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.PointCloudCOPC{}).
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
	var results []*models.PointCloudCOPC
	err := query.
		Order("updated_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&results).Error
	return results, total, err
}

func (r *PointCloudCOPCRepository) GetLatestReadyByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.PointCloudCOPC, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var result models.PointCloudCOPC
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status = ?", tenantID, itemFingerprint, models.PointCloudCOPCStatusReady).
		Order("updated_at DESC, id DESC").
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *PointCloudCOPCRepository) GetCurrentByFingerprint(ctx context.Context, tenantID uint, itemFingerprint string) (*models.PointCloudCOPC, error) {
	itemFingerprint = strings.TrimSpace(itemFingerprint)
	if itemFingerprint == "" {
		return nil, nil
	}
	var result models.PointCloudCOPC
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND item_fingerprint = ? AND status <> ?", tenantID, itemFingerprint, models.PointCloudCOPCStatusDeleted).
		Order("updated_at DESC, id DESC").
		First(&result).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &result, err
}

func (r *PointCloudCOPCRepository) UpdateFields(ctx context.Context, id uint, tenantID uint, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).
		Model(&models.PointCloudCOPC{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(fields).Error
}

func (r *PointCloudCOPCRepository) Delete(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.PointCloudCOPC{}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			Update("status", models.PointCloudCOPCStatusDeleted).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND tenant_id = ?", id, tenantID).
			Delete(&models.PointCloudCOPC{}).Error
	})
}
