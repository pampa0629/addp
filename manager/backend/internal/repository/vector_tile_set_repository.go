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

type VectorTileSetRepository struct{ db *gorm.DB }

func NewVectorTileSetRepository(db *gorm.DB) *VectorTileSetRepository {
	return &VectorTileSetRepository{db: db}
}
func (r *VectorTileSetRepository) CreateTask(ctx context.Context, task *models.VectorTileSetTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}
func (r *VectorTileSetRepository) GetTaskBySemanticHash(ctx context.Context, tenantID uint, semanticHash string, excludeTaskID uint) (*models.VectorTileSetTask, error) {
	semanticHash = strings.TrimSpace(semanticHash)
	if semanticHash == "" {
		return nil, nil
	}
	query := r.db.WithContext(ctx).
		Where("tenant_id = ? AND config ->> 'semantic_hash' = ?", tenantID, semanticHash)
	if excludeTaskID > 0 {
		query = query.Where("id <> ?", excludeTaskID)
	}
	var task models.VectorTileSetTask
	err := query.Order("updated_at DESC, id DESC").First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}
func (r *VectorTileSetRepository) UpdateTask(ctx context.Context, task *models.VectorTileSetTask) error {
	return r.db.WithContext(ctx).Save(task).Error
}
func (r *VectorTileSetRepository) DeleteTask(ctx context.Context, id, tenantID uint) error {
	return r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).Delete(&models.VectorTileSetTask{}).Error
}
func (r *VectorTileSetRepository) GetTask(ctx context.Context, id, tenantID uint) (*models.VectorTileSetTask, error) {
	var task models.VectorTileSetTask
	err := r.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &task, err
}
func (r *VectorTileSetRepository) ListTasks(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.VectorTileSetTask, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.VectorTileSetTask{}).Where("tenant_id = ?", tenantID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, pageSize = normalizePage(page, pageSize)
	var tasks []*models.VectorTileSetTask
	err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&tasks).Error
	return tasks, total, err
}
func (r *VectorTileSetRepository) ClaimExecution(ctx context.Context, taskID, tenantID uint, execution *commonExecution.TaskExecution) (*models.VectorTileSetTask, error) {
	var task models.VectorTileSetTask
	err := newTaskExecutionLifecycle(r.db).Claim(ctx, taskID, tenantID, execution, taskExecutionClaimSpec{
		TaskModel: &task, TaskType: commonExecution.TaskTypeVectorTileSetGeneration, TaskLabel: "vector tile set",
		TaskName: func() string { return task.Name }, TaskConfig: func() commonModels.JSONMap { return task.Config },
	})
	if err != nil {
		return nil, err
	}
	return &task, nil
}
func (r *VectorTileSetRepository) StartExecution(ctx context.Context, taskID, tenantID uint, executionID string, startedAt time.Time) error {
	return newTaskExecutionLifecycle(r.db).Start(ctx, taskID, tenantID, executionID, startedAt, &models.VectorTileSetTask{}, "vector tile set")
}
func (r *VectorTileSetRepository) CompleteExecution(ctx context.Context, taskID, tenantID uint, executionID string, fields map[string]interface{}, completedAt time.Time) error {
	return newTaskExecutionLifecycle(r.db).Complete(ctx, taskID, tenantID, executionID, completedAt, taskExecutionCompletionSpec{TaskModel: &models.VectorTileSetTask{}, ExecutionFields: fields}, "vector tile set")
}
