package service

import (
	"context"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"

	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

// MvtTaskService MVT 瓦片生成任务定义管理服务
// 管理 MvtTask 任务定义（CRUD），执行时复用 QuickViewService 逻辑
// 并将执行记录写入 common.task_executions
type MvtTaskService struct {
	mvtTaskRepo  *repository.MvtTaskRepository
	quickViewSvc *QuickViewService
	taskExecRepo *commonExecution.TaskExecutionRepository
}

// NewMvtTaskService 创建服务
func NewMvtTaskService(
	mvtTaskRepo *repository.MvtTaskRepository,
	quickViewSvc *QuickViewService,
	taskExecRepo *commonExecution.TaskExecutionRepository,
) *MvtTaskService {
	return &MvtTaskService{
		mvtTaskRepo:  mvtTaskRepo,
		quickViewSvc: quickViewSvc,
		taskExecRepo: taskExecRepo,
	}
}

// Create 创建任务定义
func (s *MvtTaskService) Create(ctx context.Context, task *models.MvtTask) error {
	return s.mvtTaskRepo.Create(ctx, task)
}

// GetByID 查询任务定义
func (s *MvtTaskService) GetByID(ctx context.Context, id uint, tenantID uint) (*models.MvtTask, error) {
	return s.mvtTaskRepo.GetByID(ctx, id, tenantID)
}

// List 分页查询
func (s *MvtTaskService) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.MvtTask, int64, error) {
	return s.mvtTaskRepo.List(ctx, tenantID, page, pageSize)
}

// Update 更新任务定义
func (s *MvtTaskService) Update(ctx context.Context, task *models.MvtTask) error {
	return s.mvtTaskRepo.Update(ctx, task)
}

// Delete 软删除任务定义
func (s *MvtTaskService) Delete(ctx context.Context, id uint, tenantID uint) error {
	return s.mvtTaskRepo.Delete(ctx, id, tenantID)
}

// Execute 执行 MVT 瓦片生成任务
// 流程：写入 common.task_executions (running) → 调用 QuickView 流程 → 更新执行记录 → 回写任务定义
// 返回 executionID，供调用方轮询状态
func (s *MvtTaskService) Execute(ctx context.Context, taskID uint, tenantID uint, triggerType string, source string, parentExecutionID *string) (string, error) {
	task, err := s.mvtTaskRepo.GetByID(ctx, taskID, tenantID)
	if err != nil {
		return "", err
	}
	if task == nil {
		return "", ErrTaskNotFound
	}
	normalizedTriggerType, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil {
		return "", err
	}
	normalizedSource := strings.TrimSpace(source)
	if normalizedSource == "" {
		normalizedSource = commonExecution.ModuleManager
	}

	executionID := uuid.New().String()
	now := time.Now()

	exec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(tenantID),
		Module:            commonExecution.ModuleManager,
		TaskType:          commonExecution.TaskTypeMvtGeneration,
		Source:            normalizedSource,
		SourceTaskID:      commonExecution.NewSourceTaskIDFromUint(taskID),
		SourceTaskName:    &task.Name,
		ParentExecutionID: parentExecutionID,
		Status:            commonExecution.ExecutionStatusRunning,
		TriggerType:       normalizedTriggerType,
		ExecutionConfig: commonModels.JSONMap{
			"engine_id":   task.EngineID,
			"schema_name": task.SchemaName,
			"table_name":  task.Table,
			"min_zoom":    task.MinZoom,
			"max_zoom":    task.MaxZoom,
			"source":      "mvt_task",
			"source_task": taskID,
		},
		StartedAt: &now,
	}
	if err := s.taskExecRepo.Create(ctx, exec); err != nil {
		return "", err
	}

	// 异步执行
	go func() {
		bgCtx := context.Background()
		execErr := s.runMvtGeneration(bgCtx, task, tenantID)

		completedAt := time.Now()
		durationMs := completedAt.Sub(now).Milliseconds()
		status := commonExecution.ExecutionStatusSuccess
		var errDetails commonModels.JSONMap

		if execErr != nil {
			status = commonExecution.ExecutionStatusFailed
			errDetails = commonModels.JSONMap{"message": execErr.Error()}
		}

		fields := map[string]interface{}{
			"status":            status,
			"completed_at":      completedAt,
			"execution_time_ms": durationMs,
		}
		if errDetails != nil {
			fields["error_details"] = errDetails
		}
		s.taskExecRepo.UpdateFields(bgCtx, executionID, int(tenantID), fields)

		// 回写任务定义
		s.mvtTaskRepo.UpdateLastExecution(bgCtx, taskID, executionID, status, completedAt)
	}()

	return executionID, nil
}

// runMvtGeneration 执行 MVT 瓦片生成（复用 QuickViewService 逻辑）
func (s *MvtTaskService) runMvtGeneration(ctx context.Context, task *models.MvtTask, tenantID uint) error {
	minZoom := task.MinZoom

	// 步骤一：准备（创建物化视图、空间索引）
	if _, err := s.quickViewSvc.PrepareForCreateMVT(ctx, tenantID, task.EngineID, task.SchemaName, task.Table); err != nil {
		return err
	}

	// 步骤二：触发预缓存
	params := TriggerQuickViewParams{
		TenantID:   tenantID,
		EngineID:   task.EngineID,
		SchemaName: task.SchemaName,
		TableName:  task.Table,
		MinZoom:    &minZoom,
		MaxZoom:    task.MaxZoom,
		Priority:   "default",
	}
	return s.quickViewSvc.TriggerQuickView(ctx, params)
}
