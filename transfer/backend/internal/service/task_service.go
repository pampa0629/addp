package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonAuthorization "github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/taskprovider"
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
var ErrSchemaChangeNotFound = errors.New("schema change request not found")
var ErrSchemaChangeNotAdditive = errors.New("schema change is not eligible for additive migration")
var ErrSchemaChangeApprovalConflict = errors.New("schema change approval conflicts with current source or mapping")
var ErrSchemaChangeControlUnavailable = errors.New("schema change control is unavailable")

type CaptureControl interface {
	Start(ctx context.Context, task *models.TransferTask) (*models.CaptureResource, error)
	Pause(ctx context.Context, task *models.TransferTask) error
	Resume(ctx context.Context, task *models.TransferTask) error
	Stop(ctx context.Context, task *models.TransferTask) error
	Get(ctx context.Context, taskID, tenantID uint) (*models.CaptureResource, error)
	HasStopInitiatedGeneration(ctx context.Context, taskID, tenantID uint) (bool, error)
}

type SchemaChangeInspector interface {
	InspectAdditiveFields(ctx context.Context, task *models.TransferTask, sourceFields []string) ([]models.SchemaChangeField, error)
}

type TaskExecutionEngine interface {
	ExecuteExecution(ctx context.Context, executionID uint) error
	PrepareReplayExecution(ctx context.Context, tenantID uint, taskConfig map[string]interface{}, request ReplayExecutionRequest, executionApplyIdentity string) (*ReplayExecutionPreparation, error)
}

// TaskService 任务服务
type TaskService struct {
	db               *gorm.DB
	taskRepo         *repository.TaskRepository
	deadLetterRepo   *repository.DeadLetterRepository
	schemaChangeRepo *repository.SchemaChangeRequestRepository
	executionService *ExecutionService // 使用统一执行服务
	executionEngine  TaskExecutionEngine
	cfg              *config.Config
	captureControl   CaptureControl
	taskCleanup      TaskOwnedResourceCleanup
	engineResolver   planner.EngineResolver
	schemaInspector  SchemaChangeInspector
	metaClient       *commonClient.MetaClient
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

func (s *TaskService) SetSchemaChangeInspector(inspector SchemaChangeInspector) {
	s.schemaInspector = inspector
}

// NewTaskService 创建任务服务
func NewTaskService(
	db *gorm.DB,
	executionEngine TaskExecutionEngine,
	cfg *config.Config,
) *TaskService {
	service := &TaskService{
		db:               db,
		taskRepo:         repository.NewTaskRepository(db),
		deadLetterRepo:   repository.NewDeadLetterRepository(db),
		schemaChangeRepo: repository.NewSchemaChangeRequestRepository(db),
		executionEngine:  executionEngine,
		cfg:              cfg,
		logger:           logger.With("component", "task_service"),
	}
	if cfg != nil && strings.TrimSpace(cfg.MetaServiceURL) != "" && strings.TrimSpace(cfg.ServiceClientSecret) != "" {
		tokenSource, err := commonClient.NewOAuthServiceTokenSource(cfg.SystemServiceURL, "addp-transfer", cfg.ServiceClientSecret, nil)
		if err == nil {
			service.metaClient = commonClient.NewMetaClient(cfg.MetaServiceURL, tokenSource)
		} else {
			service.logger.Error("Service Token Source 初始化失败", "error", err)
		}
	}
	return service
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
	if err := s.validateTaskConfig(ctx, tenantID, req.Config, batchSize); err != nil {
		return nil, err
	}
	boundary, _ := planner.TaskRuntimeBoundary(req.Config)
	runtimeTarget := planner.IsRuntimeExistingTargetTaskConfig(req.Config)
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
	if runtimeTarget && (schedule != "" || enabled) {
		return nil, fmt.Errorf("%w: runtime-target tasks require execution parameters from Orchestrator", ErrInvalidTaskConfig)
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
	if runtimeTarget && req.AutoScanMetadata != nil && *req.AutoScanMetadata {
		return nil, fmt.Errorf("%w: runtime-target tasks do not trigger Transfer metadata scans", ErrInvalidTaskConfig)
	}
	if runtimeTarget {
		task.AutoScanMetadata = false
	} else if req.AutoScanMetadata != nil {
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
	if err := s.validateTaskConfig(ctx, tenantID, effectiveConfig, effectiveBatchSize); err != nil {
		return nil, err
	}
	effectiveBoundary, _ := planner.TaskRuntimeBoundary(effectiveConfig)
	runtimeTarget := planner.IsRuntimeExistingTargetTaskConfig(effectiveConfig)

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
	if runtimeTarget && (task.Schedule != "" || task.Enabled) {
		return nil, fmt.Errorf("%w: runtime-target tasks require execution parameters from Orchestrator", ErrInvalidTaskConfig)
	}
	if req.AutoScanMetadata != nil {
		if runtimeTarget && *req.AutoScanMetadata {
			return nil, fmt.Errorf("%w: runtime-target tasks do not trigger Transfer metadata scans", ErrInvalidTaskConfig)
		}
		task.AutoScanMetadata = *req.AutoScanMetadata
	}
	if runtimeTarget {
		task.AutoScanMetadata = false
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
		if _, recordErr := planner.ParseEncodedRecordExportTaskSpec(config, batchSize); recordErr == nil {
			return nil
		} else if _, tableErr := planner.ParseTableExportTaskSpec(config, batchSize); tableErr == nil {
			return nil
		} else {
			return fmt.Errorf("%w: table=%v; encoded_record_export=%v; raw_copy=%v", ErrInvalidTaskConfig, tableErr, recordErr, rawErr)
		}
	}
}

func (s *TaskService) validateTaskConfig(_ context.Context, tenantID uint, config map[string]interface{}, batchSize int) error {
	if err := validateNewTaskConfig(config, batchSize); err != nil {
		return err
	}
	spec, err := planner.ParseDatabaseCDCTaskSpec(config)
	if err != nil {
		return nil
	}
	resolver := planner.BindEngineResolver(s.engineResolver, tenantID)
	if _, err := planner.ResolveDatabaseCDCBindings(spec, resolver); err != nil {
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

// CreateAdHocExecution creates a bounded sync execution without a persistent
// Transfer task definition. The execution config is the complete frozen input.
func (s *TaskService) CreateAdHocExecution(
	ctx context.Context,
	req *models.CreateAdHocExecutionRequest,
	sourceModule string,
	tenantID, userID uint,
) (*models.CreateAdHocExecutionResponse, error) {
	if s == nil || s.executionService == nil || req == nil || tenantID == 0 {
		return nil, fmt.Errorf("%w: ad-hoc execution context is incomplete", ErrInvalidTaskConfig)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidTaskConfig)
	}
	source := strings.TrimSpace(sourceModule)
	if commonAuthorization.ValidateOwnerModuleName(source) != nil {
		return nil, fmt.Errorf("%w: source module is required", ErrInvalidTaskConfig)
	}
	configBytes, err := json.Marshal(req.Config)
	if err != nil {
		return nil, fmt.Errorf("%w: encode config: %v", ErrInvalidTaskConfig, err)
	}
	config := map[string]interface{}{}
	if err := json.Unmarshal(configBytes, &config); err != nil {
		return nil, fmt.Errorf("%w: decode config: %v", ErrInvalidTaskConfig, err)
	}
	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}
	if err := s.validateTaskConfig(ctx, tenantID, config, batchSize); err != nil {
		return nil, err
	}
	boundary, err := planner.TaskRuntimeBoundary(config)
	if err != nil || boundary != commonExecution.ExecutionBoundaryBounded {
		return nil, fmt.Errorf("%w: ad-hoc execution must be bounded", ErrInvalidTaskConfig)
	}
	now := time.Now()
	triggeredBy := int(userID)
	record := &commonExecution.TaskExecution{
		TenantID: int(tenantID), ExecutionID: uuid.NewString(), Module: commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync, Source: source,
		Status: commonExecution.ExecutionStatusPending, Progress: 0,
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded, MaxAttempts: 1,
		TriggerType: commonExecution.TriggerTypeManual, TriggeredBy: &triggeredBy,
		ExecutionConfig: config,
		Metadata: commonModels.JSONMap{"ad_hoc": commonModels.JSONMap{
			"name": name, "batch_size": batchSize, "auto_scan_metadata": req.AutoScanMetadata,
		}},
		CreatedAt: now, UpdatedAt: now,
	}
	if userID == 0 {
		record.TriggeredBy = nil
	}
	if err := s.executionService.taskExecutionRepo.Create(ctx, record); err != nil {
		return nil, fmt.Errorf("create ad-hoc transfer execution: %w", err)
	}
	return &models.CreateAdHocExecutionResponse{ExecutionID: record.ExecutionID, Status: record.Status}, nil
}

// ReplayTask 为业务 Kafka continuous owner task 创建独立 bounded replay execution。
// 它不 claim owner task、不修改 desired_state/status/last_execution，也不创建 sync state。
func (s *TaskService) ReplayTask(ctx context.Context, id, tenantID, userID uint, req models.ReplayTaskRequest) (*models.TaskExecution, error) {
	if s.executionEngine == nil || s.executionService == nil {
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
	preparation, err := s.executionEngine.PrepareReplayExecution(ctx, task.TenantID, task.Config, ReplayExecutionRequest{
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
		ExecutionBoundary: commonExecution.ExecutionBoundaryBounded,
		TriggeredBy:       &triggeredBy, ExecutionConfig: preparation.ExecutionConfig, Metadata: preparation.Metadata,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.executionService.taskExecutionRepo.Create(ctx, executionRecord); err != nil {
		return nil, fmt.Errorf("create bounded replay execution: %w", err)
	}
	return s.executionService.convertToTransferExecution(executionRecord), nil
}

// StartTaskWithContext 启动任务并记录统一任务体系上下文。
func (s *TaskService) StartTaskWithContext(ctx context.Context, id, tenantID, userID uint, triggerType string, source string, parentExecutionID *string) (*models.TaskExecution, error) {
	return s.startTaskWithContext(ctx, id, tenantID, userID, triggerType, source, parentExecutionID, nil)
}

// StartTaskWithExecutionParameters starts a TaskProvider execution after its
// runtime-only inputs have been resolved by Orchestrator.
func (s *TaskService) StartTaskWithExecutionParameters(
	ctx context.Context,
	id, tenantID, userID uint,
	triggerType, source string,
	parentExecutionID *string,
	parameters map[string]interface{},
) (*models.TaskExecution, error) {
	return s.startTaskWithContext(ctx, id, tenantID, userID, triggerType, source, parentExecutionID, parameters)
}

func (s *TaskService) startTaskWithContext(
	ctx context.Context,
	id, tenantID, userID uint,
	triggerType, source string,
	parentExecutionID *string,
	parameters map[string]interface{},
) (*models.TaskExecution, error) {
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
	if err := s.validateTaskConfig(ctx, tenantID, task.Config, task.BatchSize); err != nil {
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
	runtimeTarget := planner.IsRuntimeExistingTargetTaskConfig(task.Config)
	contract := TransferTaskExecutionContract(task.Config)
	if err := taskprovider.ValidateExecutionParameters(contract.InputSchema, parameters, taskprovider.ParameterValidationOptions{}); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTaskConfig, err)
	}
	if runtimeTarget &&
		(source != commonExecution.ModuleOrchestrator || parentExecutionID == nil || strings.TrimSpace(*parentExecutionID) == "") {
		return nil, fmt.Errorf("%w: runtime-target tasks require an Orchestrator parent execution", ErrInvalidTaskConfig)
	}
	now := time.Now()
	triggeredBy := int(userID)
	executionRecord := &commonExecution.TaskExecution{
		TenantID: int(task.TenantID), ExecutionID: uuid.New().String(), Module: commonExecution.ModuleTransfer,
		TaskType: commonExecution.TaskTypeSync, Source: source, SourceTaskID: commonExecution.NewSourceTaskIDFromUint(id),
		SourceTaskName: &task.Name, ParentExecutionID: parentExecutionID, Status: commonExecution.ExecutionStatusPending,
		TriggerType: normalizedTriggerType, TriggeredBy: &triggeredBy, ExecutionConfig: task.Config,
		ExecutionBoundary: boundary,
		CreatedAt:         now, UpdatedAt: now,
	}
	if runtimeTarget {
		executionRecord.MaxAttempts = 1
		executionRecord.Metadata = commonModels.JSONMap{"runtime_inputs": commonModels.JSONMap(parameters)}
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
	_, _, err = s.taskRepo.ClaimExecution(ctx, id, tenantID, executionRecord, incrementalSourceIdentity(task))
	if err != nil {
		if errors.Is(err, repository.ErrTaskAlreadyRunning) {
			return nil, fmt.Errorf("task is already running")
		}
		return nil, fmt.Errorf("claim task execution: %w", err)
	}
	execution := s.executionService.convertToTransferExecution(executionRecord)

	s.logger.Info("bounded execution created", "task_id", id, "execution_id", execution.ID)
	return execution, nil
}

func TransferTaskExecutionContract(config map[string]interface{}) taskprovider.ExecutionContract {
	contract := taskprovider.EmptyExecutionContract()
	contract.OutputSchema = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"execution_id":   map[string]interface{}{"type": "string"},
			"target_locator": map[string]interface{}{"type": "string"},
			"row_count":      map[string]interface{}{"type": "integer", "minimum": float64(0)},
		},
		"required":             []interface{}{"execution_id", "target_locator", "row_count"},
		"additionalProperties": false,
	}
	if planner.IsRuntimeExistingTargetTaskConfig(config) {
		contract.InputSchema = map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target_locator": map[string]interface{}{"type": "string", "minLength": float64(1)},
			},
			"required":             []interface{}{"target_locator"},
			"additionalProperties": false,
		}
	}
	return contract
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
		if err := s.taskRepo.FinalizeContinuousStop(ctx, id, tenantID, "stopped", time.Now()); err != nil {
			return fmt.Errorf("finalize database CDC target apply runtime stop: %w", err)
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

// ExecuteExecution 执行已领取的 bounded execution。
func (s *TaskService) ExecuteExecution(ctx context.Context, executionID uint) error {
	return s.executionEngine.ExecuteExecution(ctx, executionID)
}

// GetStatistics 获取任务统计信息
func (s *TaskService) GetStatistics(ctx context.Context, tenantID uint) (*models.TaskStatistics, error) {
	return s.taskRepo.GetStatistics(tenantID)
}
