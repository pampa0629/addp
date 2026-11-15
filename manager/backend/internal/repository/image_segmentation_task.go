package repository

import (
	"context"
	"encoding/json"
	"path"
	"strings"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
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
		BeforeCreate: func(*gorm.DB) error {
			// The locked task stores the base name; only this execution owns the
			// concrete target subsequently checked by System and published by Manager.
			raw, err := json.Marshal(task.Config)
			if err != nil {
				return err
			}
			var cfg execution.ImageSegmentationConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return err
			}
			target, err := resourcetree.ParseURI(cfg.Target.Locator)
			if err != nil {
				return err
			}
			last := len(target.Path) - 1
			base := target.Path[last]
			target.Path[last] = strings.TrimSuffix(base, path.Ext(base)) + "-" + run.ExecutionID + ".geojson"
			cfg.Target.Locator = target.ToURI()
			cfg.Target.Path = plugin.ObjectItemPath(target.EngineID, target.Path[0], strings.Join(target.Path[1:], "/"))
			raw, err = json.Marshal(cfg)
			if err != nil {
				return err
			}
			return json.Unmarshal(raw, &run.ExecutionConfig)
		},
	})
}

func (r *ImageSegmentationTaskRepository) Complete(ctx context.Context, taskID, tenantID uint, runID string, fields map[string]any) error {
	return newTaskExecutionLifecycle(r.db).Complete(ctx, taskID, tenantID, runID, time.Now().UTC(),
		taskExecutionCompletionSpec{TaskModel: &models.TaskDefinition{}, ExecutionFields: fields}, "image segmentation")
}
