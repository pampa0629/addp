package service

import (
	"context"
	"errors"
	commonExecution "github.com/addp/common/execution"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

// ErrTaskNotFound 任务定义不存在
var ErrTaskNotFound = errors.New("task not found")

// EmbeddingTaskService 向量化任务定义管理服务
// 管理 EmbeddingTask 任务定义（CRUD），执行时写入 common.task_executions
type EmbeddingTaskService struct {
	embeddingRepo    *repository.EmbeddingRepository
	embeddingService *EmbeddingService
	taskExecRepo     *commonExecution.TaskExecutionRepository
}

// NewEmbeddingTaskService 创建服务
func NewEmbeddingTaskService(
	embeddingRepo *repository.EmbeddingRepository,
	embeddingService *EmbeddingService,
	taskExecRepo *commonExecution.TaskExecutionRepository,
) *EmbeddingTaskService {
	return &EmbeddingTaskService{
		embeddingRepo:    embeddingRepo,
		embeddingService: embeddingService,
		taskExecRepo:     taskExecRepo,
	}
}

// Create 创建任务定义
func (s *EmbeddingTaskService) Create(ctx context.Context, task *models.EmbeddingTask) error {
	return s.embeddingRepo.CreateEmbeddingTask(ctx, task)
}

// GetByID 查询任务定义
func (s *EmbeddingTaskService) GetByID(ctx context.Context, id uint, tenantID uint) (*models.EmbeddingTask, error) {
	return s.embeddingRepo.GetEmbeddingTask(ctx, id, tenantID)
}

// List 分页查询
func (s *EmbeddingTaskService) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.EmbeddingTask, int64, error) {
	return s.embeddingRepo.ListEmbeddingTasks(ctx, tenantID, page, pageSize)
}

// Update 更新任务定义
func (s *EmbeddingTaskService) Update(ctx context.Context, task *models.EmbeddingTask) error {
	return s.embeddingRepo.UpdateEmbeddingTask(ctx, task)
}

// Delete 软删除任务定义
func (s *EmbeddingTaskService) Delete(ctx context.Context, id uint, tenantID uint) error {
	return s.embeddingRepo.DeleteEmbeddingTask(ctx, id, tenantID)
}

// Execute 执行任务定义
// 1. 写入 common.task_executions (status=running)
// 2. 调用 EmbeddingService.EmbedDirectory
// 3. 完成后更新 common.task_executions 和回写任务定义
// 返回 executionID，供调用方轮询状态
func (s *EmbeddingTaskService) Execute(ctx context.Context, taskID uint, tenantID uint, triggerType string, parentExecutionID *string) (string, error) {
	task, err := s.embeddingRepo.GetEmbeddingTask(ctx, taskID, tenantID)
	if err != nil {
		return "", err
	}
	if task == nil {
		return "", ErrTaskNotFound
	}

	executionID := uuid.New().String()
	now := time.Now()

	exec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(tenantID),
		Module:            commonExecution.ModuleManager,
		TaskType:          "embedding",
		Source:            commonExecution.ModuleManager,
		SourceTaskID:      intPtr(int(taskID)),
		SourceTaskName:    &task.Name,
		ParentExecutionID: parentExecutionID,
		Status:            commonExecution.ExecutionStatusRunning,
		TriggerType:       triggerType,
		StartedAt:         &now,
	}
	if err := s.taskExecRepo.Create(ctx, exec); err != nil {
		return "", err
	}

	// 异步执行，不阻塞返回
	go func() {
		bgCtx := context.Background()
		result, execErr := s.embeddingService.EmbedDirectory(bgCtx, EmbedDirectoryRequest{
			EngineID:  task.EngineID,
			Bucket:    task.Bucket,
			Prefix:    task.Prefix,
			Recursive: task.Recursive,
			TenantID:  uintPtr(tenantID),
		})

		completedAt := time.Now()
		durationMs := completedAt.Sub(now).Milliseconds()
		status := commonExecution.ExecutionStatusSuccess
		var errDetails commonModels.JSONMap
		var metadata commonModels.JSONMap

		if execErr != nil {
			status = commonExecution.ExecutionStatusFailed
			errDetails = commonModels.JSONMap{"message": execErr.Error()}
		} else if result != nil {
			if result.Failed > 0 && result.Vectorized == 0 {
				status = commonExecution.ExecutionStatusFailed
			}
			metadata = commonModels.JSONMap{
				"total":      result.Total,
				"vectorized": result.Vectorized,
				"skipped":    result.Skipped,
				"failed":     result.Failed,
			}
			if len(result.Errors) > 0 {
				metadata["error_samples"] = result.Errors
			}
		}

		fields := map[string]interface{}{
			"status":            status,
			"completed_at":      completedAt,
			"execution_time_ms": durationMs,
		}
		if errDetails != nil {
			fields["error_details"] = errDetails
		}
		if metadata != nil {
			fields["metadata"] = metadata
		}
		s.taskExecRepo.UpdateFields(bgCtx, executionID, int(tenantID), fields)

		// 回写任务定义
		s.embeddingRepo.UpdateEmbeddingTaskLastExecution(bgCtx, taskID, executionID, status, completedAt)
	}()

	return executionID, nil
}

func intPtr(v int) *int    { return &v }
func uintPtr(v uint) *uint { return &v }
