package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
	"github.com/addp/transfer/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrInvalidTaskConfig = errors.New("invalid transfer task config")
var ErrUnsupportedTaskType = errors.New("unsupported transfer task_type")

// TaskQueue 任务队列接口（避免循环依赖）
type TaskQueue interface {
	EnqueueExecuteTask(ctx context.Context, taskID, executionID, tenantID uint) error
	Close() error
}

// TaskService 任务服务
type TaskService struct {
	db               *gorm.DB
	taskRepo         *repository.TaskRepository
	executionService *ExecutionService // 使用统一执行服务
	executionEngine  *ExecutionEngineService
	cfg              *config.Config
	taskQueue        TaskQueue
	logger           *slog.Logger
}

// NewTaskService 创建任务服务
func NewTaskService(
	db *gorm.DB,
	executionEngine *ExecutionEngineService,
	cfg *config.Config,
	taskQueue TaskQueue,
) *TaskService {
	return &TaskService{
		db:              db,
		taskRepo:        repository.NewTaskRepository(db),
		executionEngine: executionEngine,
		taskQueue:       taskQueue,
		cfg:             cfg,
		logger:          logger.With("component", "task_service"),
	}
}

// SetExecutionService 设置执行服务（在创建后注入，避免循环依赖）
func (s *TaskService) SetExecutionService(executionService *ExecutionService) {
	s.executionService = executionService
}

// CreateTask 创建任务
func (s *TaskService) CreateTask(ctx context.Context, req *models.CreateTaskRequest, tenantID, userID uint) (*models.TransferTask, error) {
	s.logger.Info("creating task", "name", req.Name, "tenant_id", tenantID)

	batchSize := req.BatchSize
	if batchSize == 0 {
		batchSize = 1000
	}
	if err := validateNewTaskConfig(req.Config, batchSize); err != nil {
		return nil, err
	}
	schedule := strings.TrimSpace(req.Schedule)
	enabled := false
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if schedule == "" {
		enabled = false
	}
	nextRunAt, err := transferTaskNextRunAt(schedule, enabled, time.Now())
	if err != nil {
		return nil, err
	}

	// 构建任务对象
	task := &models.TransferTask{
		Name:        req.Name,
		Description: req.Description,
		TaskType:    req.TaskType,
		Config:      req.Config,
		Schedule:    schedule,
		BatchSize:   batchSize,
		Status:      models.TaskStatusIdle,
		Progress:    0,
		Enabled:     enabled,
		NextRunAt:   nextRunAt,
		TenantID:    tenantID,
		CreatedBy:   &userID,
	}

	// 处理 auto_scan_metadata 字段
	if req.AutoScanMetadata != nil {
		task.AutoScanMetadata = *req.AutoScanMetadata
	} else {
		task.AutoScanMetadata = true // 默认为 true
	}

	if err := normalizeTransferTaskType(&task.TaskType); err != nil {
		return nil, err
	}

	// 创建任务
	if err := s.taskRepo.Create(task); err != nil {
		s.logger.Error("failed to create task", "error", err)
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	s.logger.Info("task created successfully", "task_id", task.ID)
	return task, nil
}

// GetTask 获取任务
func (s *TaskService) GetTask(ctx context.Context, id, tenantID uint) (*models.TransferTask, error) {
	task, err := s.taskRepo.GetByID(id)
	if err != nil {
		return nil, err
	}

	// 检查租户权限
	if task.TenantID != tenantID {
		return nil, fmt.Errorf("task not found or access denied")
	}

	return task, nil
}

// UpdateTask 更新任务
func (s *TaskService) UpdateTask(ctx context.Context, id, tenantID uint, req *models.UpdateTaskRequest) (*models.TransferTask, error) {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}

	// 运行中的任务需要先停止才能更新
	if task.Status == models.TaskStatusRunning {
		return nil, fmt.Errorf("cannot update task in %s status", task.Status)
	}

	effectiveConfig := task.Config
	if req.Config != nil {
		effectiveConfig = req.Config
	}
	effectiveBatchSize := task.BatchSize
	if req.BatchSize != nil {
		effectiveBatchSize = *req.BatchSize
	}
	if err := validateNewTaskConfig(effectiveConfig, effectiveBatchSize); err != nil {
		return nil, err
	}

	// 更新字段
	if req.Name != nil {
		task.Name = *req.Name
	}
	if req.Description != nil {
		task.Description = *req.Description
	}
	if req.TaskType != nil {
		task.TaskType = *req.TaskType
		if err := normalizeTransferTaskType(&task.TaskType); err != nil {
			return nil, err
		}
	}
	if req.Config != nil {
		task.Config = req.Config
	}
	if req.Schedule != nil {
		task.Schedule = strings.TrimSpace(*req.Schedule)
	}
	if req.BatchSize != nil {
		task.BatchSize = *req.BatchSize
	}
	if req.Enabled != nil {
		task.Enabled = *req.Enabled
	}
	if task.Schedule == "" {
		task.Enabled = false
	}
	if req.AutoScanMetadata != nil {
		task.AutoScanMetadata = *req.AutoScanMetadata
	}
	nextRunAt, err := transferTaskNextRunAt(task.Schedule, task.Enabled, time.Now())
	if err != nil {
		return nil, err
	}
	task.NextRunAt = nextRunAt

	if err := s.taskRepo.Update(task); err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	s.logger.Info("task updated", "task_id", id)
	return task, nil
}

func normalizeTransferTaskType(taskType *string) error {
	if taskType == nil || strings.TrimSpace(*taskType) == "" {
		return fmt.Errorf("%w: task_type must be sync", ErrUnsupportedTaskType)
	}
	*taskType = strings.TrimSpace(*taskType)
	if *taskType != commonExecution.TaskTypeSync {
		return fmt.Errorf("%w: %s", ErrUnsupportedTaskType, *taskType)
	}
	return nil
}

func validateNewTaskConfig(config map[string]interface{}, batchSize int) error {
	if _, err := planner.ParseRawCopyTaskSpec(config); err == nil {
		return nil
	} else {
		rawErr := err
		if _, tableErr := planner.ParseTableExportTaskSpec(config, batchSize); tableErr == nil {
			return nil
		} else {
			return fmt.Errorf("%w: table=%v; raw_copy=%v", ErrInvalidTaskConfig, tableErr, rawErr)
		}
	}
}

// DeleteTask 删除任务
func (s *TaskService) DeleteTask(ctx context.Context, id, tenantID uint) error {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return err
	}

	// 只有非运行中的任务才能删除
	if task.Status == models.TaskStatusRunning {
		return fmt.Errorf("cannot delete running task")
	}

	// 删除任务
	if err := s.taskRepo.Delete(id); err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}

	s.logger.Info("task deleted", "task_id", id)
	return nil
}

// ListTasks 列出任务
func (s *TaskService) ListTasks(ctx context.Context, tenantID uint, req *models.ListTasksRequest) ([]models.TransferTask, int64, error) {
	if req.Page == 0 {
		req.Page = 1
	}
	if req.PageSize == 0 {
		req.PageSize = 20
	}

	filters := make(map[string]interface{})
	if req.Status != nil {
		filters["status"] = *req.Status
	}
	if req.TaskType != "" {
		if req.TaskType != commonExecution.TaskTypeSync {
			return nil, 0, fmt.Errorf("%w: %s", ErrUnsupportedTaskType, req.TaskType)
		}
		filters["task_type"] = req.TaskType
	}

	return s.taskRepo.List(tenantID, filters, req.Page, req.PageSize)
}

// GetTaskStatistics 获取任务统计
func (s *TaskService) GetTaskStatistics(ctx context.Context, tenantID uint) (*models.TaskStatistics, error) {
	return s.taskRepo.GetStatistics(tenantID)
}

// StartTask 启动任务（立即执行）
func (s *TaskService) StartTask(ctx context.Context, id, tenantID, userID uint) (*models.TaskExecution, error) {
	return s.StartTaskWithContext(ctx, id, tenantID, userID, commonExecution.TriggerTypeManual, commonExecution.ModuleTransfer, nil)
}

// StartTaskWithContext 启动任务并记录统一任务体系上下文。
func (s *TaskService) StartTaskWithContext(ctx context.Context, id, tenantID, userID uint, triggerType string, source string, parentExecutionID *string) (*models.TaskExecution, error) {
	s.logger.Info("starting task", "task_id", id)

	// 1. 检查任务存在性和权限
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}

	// 2. 启动前再次校验，阻止历史旧 JSON 任务进入 worker。
	if err := normalizeTransferTaskType(&task.TaskType); err != nil {
		return nil, err
	}
	if err := validateNewTaskConfig(task.Config, task.BatchSize); err != nil {
		return nil, err
	}

	if s.taskQueue == nil {
		return nil, fmt.Errorf("task queue is not available")
	}
	normalizedTriggerType, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(source) == "" {
		source = commonExecution.ModuleTransfer
	}
	now := time.Now()
	triggeredBy := int(userID)
	executionRecord := &commonExecution.TaskExecution{
		TenantID: int(task.TenantID), ExecutionID: uuid.New().String(), Module: commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync, Source: source, SourceTaskID: commonExecution.NewSourceTaskIDFromUint(id),
		SourceTaskName: &task.Name, ParentExecutionID: parentExecutionID, Status: commonExecution.ExecutionStatusPending,
		TriggerType: normalizedTriggerType, TriggeredBy: &triggeredBy, ExecutionConfig: task.Config,
		StartedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	_, _, err = s.taskRepo.ClaimExecution(ctx, id, tenantID, executionRecord, incrementalSourceIdentity(task))
	if err != nil {
		if errors.Is(err, repository.ErrTaskAlreadyRunning) {
			return nil, fmt.Errorf("task is already running")
		}
		return nil, fmt.Errorf("claim task execution: %w", err)
	}
	execution := s.executionService.convertToTransferExecution(executionRecord)

	if err := s.taskQueue.EnqueueExecuteTask(ctx, id, execution.ID, tenantID); err != nil {
		s.logger.Error("failed to enqueue task", "error", err, "task_id", id)

		// 回滚：标记执行失败
		s.executionService.FinishExecution(ctx, execution.ID, models.ExecutionStatusFailed, err.Error())

		// 回滚：恢复任务状态为空闲
		if err := s.taskRepo.UpdateFields(id, map[string]interface{}{
			"status":   models.TaskStatusIdle,
			"progress": 0,
		}); err != nil {
			s.logger.Warn("failed to rollback task state after enqueue failure", "error", err, "task_id", id)
		}

		return nil, fmt.Errorf("failed to enqueue task: %w", err)
	}
	s.logger.Info("task enqueued to worker", "task_id", id, "execution_id", execution.ID)

	s.logger.Info("task started", "task_id", id, "execution_id", execution.ID)
	return execution, nil
}

// PauseTask 暂停任务（暂停定时调度）
func (s *TaskService) PauseTask(ctx context.Context, id, tenantID uint) error {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return err
	}

	if task.Schedule == "" {
		return fmt.Errorf("manual tasks cannot be paused")
	}

	updates := map[string]interface{}{
		"enabled":     false,
		"next_run_at": nil,
	}

	if err := s.taskRepo.UpdateFields(id, updates); err != nil {
		return fmt.Errorf("failed to pause task: %w", err)
	}

	s.logger.Info("task paused", "task_id", id)
	return nil
}

func incrementalSourceIdentity(task *models.TransferTask) string {
	if task == nil {
		return ""
	}
	spec, err := planner.ParseTableExportTaskSpec(task.Config, task.BatchSize)
	if err != nil || !planner.IsWatermarkIncrementalSpec(spec) {
		return ""
	}
	return strings.TrimSpace(spec.Source.Locator)
}

func IncrementalSourceIdentityForTask(task *models.TransferTask) string {
	return incrementalSourceIdentity(task)
}

// ResumeTask 恢复任务（恢复定时调度）
func (s *TaskService) ResumeTask(ctx context.Context, id, tenantID uint) error {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return err
	}

	if task.Schedule == "" {
		return fmt.Errorf("manual tasks do not support resume")
	}

	if task.Enabled {
		return fmt.Errorf("task is already enabled")
	}

	nextRunAt, err := transferTaskNextRunAt(task.Schedule, true, time.Now())
	if err != nil {
		return err
	}
	updates := map[string]interface{}{
		"enabled":     true,
		"next_run_at": nextRunAt,
	}

	if err := s.taskRepo.UpdateFields(id, updates); err != nil {
		return fmt.Errorf("failed to resume task: %w", err)
	}

	s.logger.Info("task resumed", "task_id", id)
	return nil
}

func transferTaskNextRunAt(schedule string, enabled bool, now time.Time) (*time.Time, error) {
	if strings.TrimSpace(schedule) == "" || !enabled {
		return nil, nil
	}
	return nextTransferRunAt(schedule, now)
}

// ExecuteTask 执行任务（由 Worker 调用，委托给 ExecutionEngineService）
func (s *TaskService) ExecuteTask(ctx context.Context, taskID, executionID uint) error {
	return s.executionEngine.ExecuteTask(ctx, taskID, executionID)
}

// GetStatistics 获取任务统计信息
func (s *TaskService) GetStatistics(ctx context.Context, tenantID uint) (*models.TaskStatistics, error) {
	return s.taskRepo.GetStatistics(tenantID)
}
