package repository

import (
	"context"
	"time"

	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"gorm.io/gorm"
)

type ImageSegmentationTaskRepository struct {
	*TaskDefinitionRepository
	db *gorm.DB
}

func NewImageSegmentationTaskRepository(db *gorm.DB) *ImageSegmentationTaskRepository {
	return &ImageSegmentationTaskRepository{NewTaskDefinitionRepository(db), db}
}

func (r *ImageSegmentationTaskRepository) Create(ctx context.Context, task *models.TaskDefinition) error {
	return createTaskDefinition(ctx, r.db, execution.TaskTypeImageSegmentation, task)
}

func (r *ImageSegmentationTaskRepository) Update(ctx context.Context, task *models.TaskDefinition) error {
	return updateTaskDefinition(ctx, r.db, execution.TaskTypeImageSegmentation, task)
}

func (r *ImageSegmentationTaskRepository) Enqueue(ctx context.Context, taskID, tenantID uint, run *execution.TaskExecution) error {
	var task models.TaskDefinition
	return newTaskExecutionLifecycle(r.db).Claim(ctx, taskID, tenantID, run, taskExecutionClaimSpec{
		TaskModel: &task, TaskType: execution.TaskTypeImageSegmentation, TaskTypeColumn: true, TaskLabel: "image segmentation",
		TaskName: func() string { return task.Name }, TaskConfig: func() commonmodels.JSONMap { return task.Config },
	})
}

func (r *ImageSegmentationTaskRepository) Complete(ctx context.Context, taskID, tenantID uint, runID string, fields map[string]any) error {
	return newTaskExecutionLifecycle(r.db).Complete(ctx, taskID, tenantID, runID, time.Now().UTC(),
		taskExecutionCompletionSpec{TaskModel: &models.TaskDefinition{}, ExecutionFields: fields}, "image segmentation")
}
