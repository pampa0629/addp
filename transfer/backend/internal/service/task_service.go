package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
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
var ErrDeadLetterNotFound = errors.New("transfer dead-letter record not found")
var ErrTaskDeleteRequiresStopped = errors.New("transfer task must be stopped before deletion")
var ErrTaskDeleteCleanupFailed = errors.New("transfer task-owned resource cleanup failed")

// TaskQueue 任务队列接口（避免循环依赖）
type TaskQueue interface {
	EnqueueExecuteTask(ctx context.Context, taskID, executionID, tenantID uint) error
	Close() error
}

type CaptureControl interface {
	Start(ctx context.Context, task *models.TransferTask) (*models.CaptureResource, error)
	Pause(ctx context.Context, task *models.TransferTask) error
	Resume(ctx context.Context, task *models.TransferTask) error
	Stop(ctx context.Context, task *models.TransferTask) error
	Get(ctx context.Context, taskID, tenantID uint) (*models.CaptureResource, error)
	HasStopInitiatedGeneration(ctx context.Context, taskID, tenantID uint) (bool, error)
}

type TaskExecutionEngine interface {
	ExecuteTask(ctx context.Context, taskID, executionID uint) error
	PrepareReplayExecution(ctx context.Context, taskConfig map[string]interface{}, request ReplayExecutionRequest, executionApplyIdentity string) (*ReplayExecutionPreparation, error)
}

// TaskService 任务服务
type TaskService struct {
	db               *gorm.DB
	taskRepo         *repository.TaskRepository
	deadLetterRepo   *repository.DeadLetterRepository
	executionService *ExecutionService // 使用统一执行服务
	executionEngine  TaskExecutionEngine
	cfg              *config.Config
	taskQueue        TaskQueue
	captureControl   CaptureControl
	taskCleanup      TaskOwnedResourceCleanup
	engineResolver   planner.EngineResolver
	logger           *slog.Logger
}

func (s *TaskService) SetCaptureControl(control CaptureControl) {
	s.captureControl = control
}

func (s *TaskService) SetTaskOwnedResourceCleanup(cleanup TaskOwnedResourceCleanup) {
	s.taskCleanup = cleanup
}

func (s *TaskService) SetEngineResolver(resolver planner.EngineResolver) {
	s.engineResolver = resolver
}

// NewTaskService 创建任务服务
func NewTaskService(
	db *gorm.DB,
	executionEngine TaskExecutionEngine,
	cfg *config.Config,
	taskQueue TaskQueue,
) *TaskService {
	return &TaskService{
		db:              db,
		taskRepo:        repository.NewTaskRepository(db),
		deadLetterRepo:  repository.NewDeadLetterRepository(db),
		executionEngine: executionEngine,
		taskQueue:       taskQueue,
		cfg:             cfg,
		logger:          logger.With("component", "task_service"),
	}
}

// ListDeadLetters 返回认证租户下 owner task 的安全 DLQ 控制索引。
func (s *TaskService) ListDeadLetters(ctx context.Context, taskID, tenantID uint, request models.DeadLetterListRequest) ([]models.DeadLetterView, int64, error) {
	if _, err := s.GetTask(ctx, taskID, tenantID); err != nil {
		return nil, 0, err
	}
	deadLetters, total, err := s.deadLetterRepo.ListByTask(ctx, tenantID, taskID, request)
	if err != nil {
		return nil, 0, err
	}
	views := make([]models.DeadLetterView, 0, len(deadLetters))
	for _, deadLetter := range deadLetters {
		views = append(views, models.NewDeadLetterView(deadLetter))
	}
	return views, total, nil
}

// GetDeadLetter 返回 owner task 下单条安全 DLQ 控制索引。
func (s *TaskService) GetDeadLetter(ctx context.Context, taskID, tenantID uint, identity string) (*models.DeadLetterView, error) {
	if _, err := s.GetTask(ctx, taskID, tenantID); err != nil {
		return nil, err
	}
	deadLetter, err := s.deadLetterRepo.GetByTask(ctx, tenantID, taskID, identity)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeadLetterNotFound
		}
		return nil, err
	}
	view := models.NewDeadLetterView(*deadLetter)
	return &view, nil
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
	if err := s.validateTaskConfig(ctx, req.Config, batchSize); err != nil {
		return nil, err
	}
	boundary, _ := planner.TaskRuntimeBoundary(req.Config)
	schedule := strings.TrimSpace(req.Schedule)
	enabled := false
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if schedule == "" {
		enabled = false
	}
	if boundary == planner.RuntimeBoundaryContinuous && (schedule != "" || enabled) {
		return nil, fmt.Errorf("%w: continuous tasks do not support task-owned schedules", ErrInvalidTaskConfig)
	}
	nextRunAt, err := transferTaskNextRunAt(schedule, enabled, time.Now())
	if err != nil {
		return nil, err
	}

	// 构建任务对象
	task := &models.TransferTask{
		Name:         req.Name,
		Description:  req.Description,
		TaskType:     req.TaskType,
		Config:       req.Config,
		Schedule:     schedule,
		BatchSize:    batchSize,
		Status:       models.TaskStatusIdle,
		DesiredState: models.TaskDesiredStateStopped,
		Progress:     0,
		Enabled:      enabled,
		NextRunAt:    nextRunAt,
		TenantID:     tenantID,
		CreatedBy:    &userID,
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
		return nil, commonAPI.ErrNotFound
	}
	s.attachCaptureSummary(ctx, task)

	return task, nil
}

func (s *TaskService) attachCaptureSummary(ctx context.Context, task *models.TransferTask) {
	if task == nil || s.captureControl == nil || !planner.IsDatabaseCDCTaskConfig(task.Config) {
		return
	}
	resource, err := s.captureControl.Get(ctx, task.ID, task.TenantID)
	if err == nil {
		task.Capture = models.NewCaptureSummary(resource)
	}
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
	if s.captureControl != nil {
		terminal, err := s.captureControl.HasStopInitiatedGeneration(ctx, id, tenantID)
		if err != nil {
			return nil, err
		}
		if terminal {
			return nil, repository.ErrCaptureTerminal
		}
		if req.Config != nil && planner.IsDatabaseCDCTaskConfig(task.Config) {
			if _, err := s.captureControl.Get(ctx, id, tenantID); err == nil {
				return nil, fmt.Errorf("database CDC config is immutable after capture generation creation")
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
		}
	}

	effectiveConfig := task.Config
	if req.Config != nil {
		effectiveConfig = req.Config
	}
	effectiveBatchSize := task.BatchSize
	if req.BatchSize != nil {
		effectiveBatchSize = *req.BatchSize
	}
	if err := s.validateTaskConfig(ctx, effectiveConfig, effectiveBatchSize); err != nil {
		return nil, err
	}
	effectiveBoundary, _ := planner.TaskRuntimeBoundary(effectiveConfig)

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
	if effectiveBoundary == planner.RuntimeBoundaryContinuous && (task.Schedule != "" || task.Enabled) {
		return nil, fmt.Errorf("%w: continuous tasks do not support task-owned schedules", ErrInvalidTaskConfig)
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
	boundary, boundaryErr := planner.TaskRuntimeBoundary(config)
	if boundaryErr != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTaskConfig, boundaryErr)
	}
	if boundary == planner.RuntimeBoundaryContinuous {
		if _, cdcErr := planner.ParseDatabaseCDCTaskSpec(config); cdcErr == nil {
			return nil
		} else if _, kafkaErr := planner.ParseContinuousTaskSpec(config); kafkaErr == nil {
			return nil
		} else {
			return fmt.Errorf("%w: continuous_kafka=%v; database_cdc=%v", ErrInvalidTaskConfig, kafkaErr, cdcErr)
		}
	}
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

func (s *TaskService) validateTaskConfig(_ context.Context, config map[string]interface{}, batchSize int) error {
	if err := validateNewTaskConfig(config, batchSize); err != nil {
		return err
	}
	spec, err := planner.ParseDatabaseCDCTaskSpec(config)
	if err != nil {
		return nil
	}
	if _, err := planner.ResolveDatabaseCDCBindings(spec, s.engineResolver); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTaskConfig, err)
	}
	return nil
}

// DeleteTask 删除任务
func (s *TaskService) DeleteTask(ctx context.Context, id, tenantID uint) error {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return err
	}

	// 只有非运行中的任务才能删除
	if task.Status == models.TaskStatusRunning {
		return ErrTaskDeleteRequiresStopped
	}
	if s.taskCleanup == nil {
		return ErrTaskDeleteCleanupFailed
	}
	if _, err := s.taskCleanup.DeleteTaskAndOwnedResources(ctx, task, repository.TaskDefinitionDeleteSoft); err != nil {
		if errors.Is(err, repository.ErrTaskDeletionRuntimeActive) {
			return ErrTaskDeleteRequiresStopped
		}
		return fmt.Errorf("%w: %v", ErrTaskDeleteCleanupFailed, err)
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
	if req.RuntimeBoundary != "" {
		if req.RuntimeBoundary != "bounded" && req.RuntimeBoundary != planner.RuntimeBoundaryContinuous {
			return nil, 0, fmt.Errorf("%w: unsupported runtime_boundary %q", ErrInvalidTaskConfig, req.RuntimeBoundary)
		}
		filters["runtime_boundary"] = req.RuntimeBoundary
	}

	tasks, total, err := s.taskRepo.List(tenantID, filters, req.Page, req.PageSize)
	if err != nil {
		return nil, 0, err
	}
	for index := range tasks {
		s.attachCaptureSummary(ctx, &tasks[index])
	}
	return tasks, total, nil
}

// GetTaskStatistics 获取任务统计
func (s *TaskService) GetTaskStatistics(ctx context.Context, tenantID uint) (*models.TaskStatistics, error) {
	return s.taskRepo.GetStatistics(tenantID)
}

// StartTask 启动任务（立即执行）
func (s *TaskService) StartTask(ctx context.Context, id, tenantID, userID uint) (*models.TaskExecution, error) {
	return s.StartTaskWithContext(ctx, id, tenantID, userID, commonExecution.TriggerTypeManual, commonExecution.ModuleTransfer, nil)
}

// ReplayTask 为业务 Kafka continuous owner task 创建独立 bounded replay execution。
// 它不 claim owner task、不修改 desired_state/status/last_execution，也不创建 sync state。
func (s *TaskService) ReplayTask(ctx context.Context, id, tenantID, userID uint, req models.ReplayTaskRequest) (*models.TaskExecution, error) {
	if s.executionEngine == nil || s.executionService == nil || s.taskQueue == nil {
		return nil, ErrReplayRuntimeUnavailable
	}
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	if err := normalizeTransferTaskType(&task.TaskType); err != nil {
		return nil, err
	}
	ranges := make([]planner.ReplayOffsetRange, len(req.Ranges))
	for i, replayRange := range req.Ranges {
		ranges[i] = planner.ReplayOffsetRange{
			Partition: replayRange.Partition, StartOffset: replayRange.StartOffset, EndOffset: replayRange.EndOffset,
		}
	}
	applyIdentity := uuid.NewString()
	for applyIdentity == task.ApplyIdentity {
		applyIdentity = uuid.NewString()
	}
	preparation, err := s.executionEngine.PrepareReplayExecution(ctx, task.Config, ReplayExecutionRequest{
		Ranges: ranges,
		Target: planner.ReplayTargetSpec{ParentLocator: req.Target.ParentLocator, Name: req.Target.Name},
	}, applyIdentity)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	triggeredBy := int(userID)
	executionRecord := &commonExecution.TaskExecution{
		TenantID: int(task.TenantID), ExecutionID: uuid.NewString(), Module: commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync, Source: commonExecution.ModuleTransfer,
		SourceTaskID: commonExecution.NewSourceTaskIDFromUint(task.ID), SourceTaskName: &task.Name,
		Status: commonExecution.ExecutionStatusPending, TriggerType: commonExecution.TriggerTypeManual,
		TriggeredBy: &triggeredBy, ExecutionConfig: preparation.ExecutionConfig, Metadata: preparation.Metadata,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.executionService.taskExecutionRepo.Create(ctx, executionRecord); err != nil {
		return nil, fmt.Errorf("create bounded replay execution: %w", err)
	}
	execution := s.executionService.convertToTransferExecution(executionRecord)
	if err := s.taskQueue.EnqueueExecuteTask(ctx, task.ID, execution.ID, tenantID); err != nil {
		if finishErr := s.executionService.FinishExecution(ctx, execution.ID, models.ExecutionStatusFailed, err.Error()); finishErr != nil {
			s.logger.Warn("failed to mark replay execution failed after enqueue failure", "error", finishErr, "execution_id", execution.ID)
		}
		return nil, fmt.Errorf("enqueue bounded replay execution: %w", err)
	}
	return execution, nil
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
	isDatabaseCDC := planner.IsDatabaseCDCTaskConfig(task.Config)
	if isDatabaseCDC && task.Status == models.TaskStatusBlocked {
		return nil, ErrCDCSchemaChangeBlocked
	}
	if isDatabaseCDC && s.captureControl != nil {
		terminal, err := s.captureControl.HasStopInitiatedGeneration(ctx, id, tenantID)
		if err != nil {
			return nil, err
		}
		if terminal {
			return nil, repository.ErrCaptureTerminal
		}
	}
	if err := s.validateTaskConfig(ctx, task.Config, task.BatchSize); err != nil {
		return nil, err
	}

	boundary, err := planner.TaskRuntimeBoundary(task.Config)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTaskConfig, err)
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
		CreatedAt: now, UpdatedAt: now,
	}
	if boundary == planner.RuntimeBoundaryContinuous {
		if task.Schedule != "" || task.Enabled {
			return nil, fmt.Errorf("continuous tasks do not support task-owned schedules")
		}
		if isDatabaseCDC {
			if err := s.ensureDatabaseCDCCapture(ctx, task); err != nil {
				return nil, err
			}
		}
		_, err = s.taskRepo.StartContinuousExecution(ctx, id, tenantID, executionRecord)
		if err != nil {
			if errors.Is(err, repository.ErrContinuousTaskBlocked) {
				return nil, ErrCDCSchemaChangeBlocked
			}
			if errors.Is(err, repository.ErrContinuousTaskAlreadyRunning) {
				return nil, fmt.Errorf("continuous task is already running")
			}
			return nil, fmt.Errorf("start continuous execution: %w", err)
		}
		return s.executionService.convertToTransferExecution(executionRecord), nil
	}
	if s.taskQueue == nil {
		return nil, fmt.Errorf("task queue is not available")
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

func (s *TaskService) ensureDatabaseCDCCapture(ctx context.Context, task *models.TransferTask) error {
	if s.captureControl == nil {
		return ErrCDCCaptureControlUnavailable
	}
	resource, err := s.captureControl.Get(ctx, task.ID, task.TenantID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if _, err := s.captureControl.Start(ctx, task); err != nil {
			return fmt.Errorf("start database CDC capture: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("load database CDC capture: %w", err)
	}
	if resource.Status == models.CaptureStatusStopped {
		return repository.ErrCaptureTerminal
	}
	if err := s.captureControl.Resume(ctx, task); err != nil {
		return fmt.Errorf("resume database CDC capture: %w", err)
	}
	return nil
}

// PauseTask 暂停任务（暂停定时调度）
func (s *TaskService) PauseTask(ctx context.Context, id, tenantID uint) error {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return err
	}

	boundary, err := planner.TaskRuntimeBoundary(task.Config)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTaskConfig, err)
	}
	if boundary == planner.RuntimeBoundaryContinuous {
		isCDC := planner.IsDatabaseCDCTaskConfig(task.Config)
		if isCDC && task.Status == models.TaskStatusBlocked {
			return ErrCDCSchemaChangeBlocked
		}
		if isCDC && s.captureControl == nil {
			return ErrCDCCaptureControlUnavailable
		}
		if err := s.taskRepo.SetContinuousDesiredState(ctx, id, tenantID, models.TaskDesiredStatePaused, "paused"); err != nil {
			return fmt.Errorf("failed to pause continuous task: %w", err)
		}
		if isCDC {
			if err := s.captureControl.Pause(ctx, task); err != nil {
				return fmt.Errorf("observe database CDC capture while pausing target apply: %w", err)
			}
		}
		s.logger.Info("continuous task pause requested", "task_id", id)
		return nil
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
func (s *TaskService) ResumeTask(ctx context.Context, id, tenantID, userID uint) (*models.TaskExecution, error) {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}
	boundary, err := planner.TaskRuntimeBoundary(task.Config)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTaskConfig, err)
	}
	if boundary == planner.RuntimeBoundaryContinuous {
		return s.StartTask(ctx, id, tenantID, userID)
	}

	if task.Schedule == "" {
		return nil, fmt.Errorf("manual tasks do not support resume")
	}

	if task.Enabled {
		return nil, fmt.Errorf("task is already enabled")
	}

	nextRunAt, err := transferTaskNextRunAt(task.Schedule, true, time.Now())
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"enabled":     true,
		"next_run_at": nextRunAt,
	}

	if err := s.taskRepo.UpdateFields(id, updates); err != nil {
		return nil, fmt.Errorf("failed to resume task: %w", err)
	}

	s.logger.Info("task resumed", "task_id", id)
	return nil, nil
}

// StopTask 将 continuous task 收敛到 stopped；数据库 CDC stop 是不可逆 capture 终态。
func (s *TaskService) StopTask(ctx context.Context, id, tenantID uint, req models.StopTaskRequest) error {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return err
	}
	boundary, err := planner.TaskRuntimeBoundary(task.Config)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTaskConfig, err)
	}
	if boundary != planner.RuntimeBoundaryContinuous {
		return fmt.Errorf("bounded tasks do not support stop; pause their schedule instead")
	}
	isCDC := planner.IsDatabaseCDCTaskConfig(task.Config)
	if isCDC && (!req.Confirmed || req.ConfirmationText != task.Name) {
		return ErrCDCStopConfirmationRequired
	}
	if isCDC && s.captureControl == nil {
		return ErrCDCCaptureControlUnavailable
	}
	if err := s.taskRepo.SetContinuousDesiredState(ctx, id, tenantID, models.TaskDesiredStateStopped, "stopped"); err != nil {
		return fmt.Errorf("failed to stop continuous task: %w", err)
	}
	if isCDC {
		if s.cfg != nil {
			if err := s.taskRepo.WaitContinuousRuntimeStopped(ctx, id, s.cfg.ContinuousRuntimeStopTimeout, s.cfg.ContinuousRuntimeStopPollInterval); err != nil {
				return fmt.Errorf("wait for database CDC target apply runtime to stop: %w", err)
			}
		}
		if err := s.captureControl.Stop(ctx, task); err != nil {
			return fmt.Errorf("cleanup database CDC capture: %w", err)
		}
	}
	s.logger.Info("continuous task stop requested", "task_id", id)
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
