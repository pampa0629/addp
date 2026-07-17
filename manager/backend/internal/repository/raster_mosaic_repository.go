package repository

import (
	"context"
	"errors"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

// RasterMosaicRepository 维护栅格 mosaic 生成任务定义。
type RasterMosaicRepository struct {
	db *gorm.DB
}

func NewRasterMosaicRepository(db *gorm.DB) *RasterMosaicRepository {
	return &RasterMosaicRepository{db: db}
}

func (r *RasterMosaicRepository) CreateTask(ctx context.Context, task *models.RasterMosaicTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

func (r *RasterMosaicRepository) GetTask(ctx context.Context, id uint, tenantID uint) (*models.RasterMosaicTask, error) {
	var task models.RasterMosaicTask
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}

func (r *RasterMosaicRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.RasterMosaicTask, int64, error) {
	query := r.db.WithContext(ctx).
		Model(&models.RasterMosaicTask{}).
		Where("tenant_id = ?", tenantID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.RasterMosaicTask
	err := query.
		Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&tasks).Error
	return tasks, total, err
}

func (r *RasterMosaicRepository) UpdateTask(ctx context.Context, task *models.RasterMosaicTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}

func (r *RasterMosaicRepository) DeleteTask(ctx context.Context, id uint, tenantID uint) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Delete(&models.RasterMosaicTask{}).Error
}

func (r *RasterMosaicRepository) ClaimExecution(
	ctx context.Context, taskID, tenantID uint, execution *commonExecution.TaskExecution,
) (*models.RasterMosaicTask, error) {
	var task models.RasterMosaicTask
	err := newTaskExecutionLifecycle(r.db).Claim(ctx, taskID, tenantID, execution, taskExecutionClaimSpec{
		TaskModel: &task,
		TaskType:  commonExecution.TaskTypeRasterMosaicGeneration,
		TaskLabel: "raster mosaic",
		TaskName:  func() string { return task.Name },
		TaskConfig: func() commonModels.JSONMap {
			return task.Config
		},
	})
	if err != nil {
		return nil, err
	}
	task.LastExecutionID = &execution.ExecutionID
	status := commonExecution.ExecutionStatusPending
	task.LastExecutionStatus = &status
	return &task, nil
}

func (r *RasterMosaicRepository) StartExecution(
	ctx context.Context, taskID, tenantID uint, executionID string, startedAt time.Time,
) error {
	return newTaskExecutionLifecycle(r.db).Start(
		ctx, taskID, tenantID, executionID, startedAt, &models.RasterMosaicTask{}, "raster mosaic",
	)
}

func (r *RasterMosaicRepository) CompleteExecution(
	ctx context.Context,
	taskID, tenantID uint,
	executionID string,
	executionFields map[string]interface{},
	completedAt time.Time,
) error {
	return newTaskExecutionLifecycle(r.db).Complete(ctx, taskID, tenantID, executionID, completedAt, taskExecutionCompletionSpec{
		TaskModel:       &models.RasterMosaicTask{},
		ExecutionFields: executionFields,
	}, "raster mosaic")
}
