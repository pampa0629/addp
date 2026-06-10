package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	commonScheduler "github.com/addp/common/scheduler"

	"github.com/addp/manager/internal/config"
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
	currentModel     string
	currentDimension int
	currentMaxFileMB int
}

// NewEmbeddingTaskService 创建服务
func NewEmbeddingTaskService(
	embeddingRepo *repository.EmbeddingRepository,
	embeddingService *EmbeddingService,
	taskExecRepo *commonExecution.TaskExecutionRepository,
	cfg *config.Config,
) *EmbeddingTaskService {
	svc := &EmbeddingTaskService{
		embeddingRepo:    embeddingRepo,
		embeddingService: embeddingService,
		taskExecRepo:     taskExecRepo,
	}
	if cfg != nil {
		cfgSvc := &EmbeddingService{cfg: cfg}
		svc.currentModel = cfgSvc.currentEmbeddingModel()
		svc.currentDimension = cfgSvc.currentEmbeddingDimension()
		svc.currentMaxFileMB = cfg.VectorConfig.MaxFileSizeMB
	} else if embeddingService != nil {
		svc.currentModel = embeddingService.currentEmbeddingModel()
		svc.currentDimension = embeddingService.currentEmbeddingDimension()
		if embeddingService.cfg != nil {
			svc.currentMaxFileMB = embeddingService.cfg.VectorConfig.MaxFileSizeMB
		}
	}
	return svc
}

// Create 创建任务定义
func (s *EmbeddingTaskService) Create(ctx context.Context, task *models.EmbeddingTask) error {
	if err := s.prepareEmbeddingTaskDefinition(task); err != nil {
		return err
	}
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
	if err := s.prepareEmbeddingTaskDefinition(task); err != nil {
		return err
	}
	return s.embeddingRepo.UpdateEmbeddingTask(ctx, task)
}

// Delete 软删除任务定义
func (s *EmbeddingTaskService) Delete(ctx context.Context, id uint, tenantID uint) error {
	return s.embeddingRepo.DeleteEmbeddingTask(ctx, id, tenantID)
}

// Execute 执行任务定义，执行配置必须来自 task.Config 快照。
func (s *EmbeddingTaskService) Execute(ctx context.Context, taskID uint, tenantID uint, triggerType string, source string, parentExecutionID *string) (string, error) {
	task, err := s.embeddingRepo.GetEmbeddingTask(ctx, taskID, tenantID)
	if err != nil {
		return "", err
	}
	if task == nil {
		return "", ErrTaskNotFound
	}
	if s.taskExecRepo == nil {
		return "", errors.New("task execution repository is required")
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

	executionConfig, req, err := s.embeddingTaskExecutionConfig(task)
	if err != nil {
		if createErr := s.createFailedEmbeddingTaskExecution(ctx, task, executionID, normalizedTriggerType, normalizedSource, parentExecutionID, now, err); createErr != nil {
			return "", createErr
		}
		return executionID, nil
	}

	exec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(tenantID),
		Module:            commonExecution.ModuleManager,
		TaskType:          commonExecution.TaskTypeEmbedding,
		Source:            normalizedSource,
		SourceTaskID:      commonExecution.NewSourceTaskIDFromUint(taskID),
		SourceTaskName:    &task.Name,
		ParentExecutionID: parentExecutionID,
		Status:            commonExecution.ExecutionStatusRunning,
		TriggerType:       normalizedTriggerType,
		ExecutionConfig:   executionConfig,
		StartedAt:         &now,
	}
	if err := s.taskExecRepo.Create(ctx, exec); err != nil {
		return "", err
	}
	if s.embeddingService == nil {
		failedAt := time.Now()
		errDetails := commonModels.JSONMap{"message": models.EmbeddingReasonEmbeddingServiceNil}
		if err := s.taskExecRepo.UpdateFields(ctx, executionID, int(tenantID), map[string]interface{}{
			"status":        commonExecution.ExecutionStatusFailed,
			"error_details": errDetails,
			"completed_at":  failedAt,
			"updated_at":    failedAt,
		}); err != nil {
			return "", err
		}
		if err := s.embeddingRepo.UpdateEmbeddingTaskLastExecution(ctx, taskID, executionID, commonExecution.ExecutionStatusFailed, failedAt); err != nil {
			return "", err
		}
		return executionID, nil
	}

	// 异步执行，不阻塞返回
	go func() {
		bgCtx := context.Background()
		result, execErr := s.embeddingService.RunEmbeddingExecution(bgCtx, tenantID, req, &EmbeddingExecutionContext{
			ExecutionID: executionID,
			TenantID:    int(tenantID),
			StartedAt:   now,
			Config:      executionConfig,
		})

		status := commonExecution.ExecutionStatusSuccess
		var errDetails commonModels.JSONMap

		if execErr != nil {
			status = commonExecution.ExecutionStatusFailed
			errDetails = commonModels.JSONMap{"message": execErr.Error()}
		}

		s.embeddingService.finishExecution(bgCtx, executionID, int(tenantID), status, now, errDetails, statsToJSONMap(result))

		// 回写任务定义
		completedAt := time.Now()
		s.embeddingRepo.UpdateEmbeddingTaskLastExecution(bgCtx, taskID, executionID, status, completedAt)
	}()

	return executionID, nil
}

func (s *EmbeddingTaskService) createFailedEmbeddingTaskExecution(
	ctx context.Context,
	task *models.EmbeddingTask,
	executionID string,
	triggerType string,
	source string,
	parentExecutionID *string,
	startedAt time.Time,
	execErr error,
) error {
	if task == nil {
		return errors.New("embedding task is nil")
	}
	if s == nil || s.taskExecRepo == nil {
		return errors.New("task execution repository is required")
	}
	completedAt := time.Now()
	durationMs := completedAt.Sub(startedAt).Milliseconds()
	executionConfig := task.Config.Clone()
	if executionConfig == nil {
		executionConfig = commonModels.JSONMap{}
	}

	exec := &commonExecution.TaskExecution{
		ExecutionID:       executionID,
		TenantID:          int(task.TenantID),
		Module:            commonExecution.ModuleManager,
		TaskType:          commonExecution.TaskTypeEmbedding,
		Source:            source,
		SourceTaskID:      commonExecution.NewSourceTaskIDFromUint(task.ID),
		SourceTaskName:    &task.Name,
		ParentExecutionID: parentExecutionID,
		Status:            commonExecution.ExecutionStatusFailed,
		TriggerType:       triggerType,
		ExecutionConfig:   executionConfig,
		ErrorDetails:      commonModels.JSONMap{"message": execErr.Error()},
		ExecutionTimeMs:   &durationMs,
		StartedAt:         &startedAt,
		CompletedAt:       &completedAt,
	}
	if err := s.taskExecRepo.Create(ctx, exec); err != nil {
		return err
	}
	return s.embeddingRepo.UpdateEmbeddingTaskLastExecution(ctx, task.ID, executionID, commonExecution.ExecutionStatusFailed, completedAt)
}

func (s *EmbeddingTaskService) embeddingTaskExecutionConfig(task *models.EmbeddingTask) (commonModels.JSONMap, EmbeddingExecutionRequest, error) {
	if task == nil {
		return nil, EmbeddingExecutionRequest{}, errors.New("embedding task is nil")
	}
	if err := s.prepareEmbeddingTaskDefinition(task); err != nil {
		return nil, EmbeddingExecutionRequest{}, err
	}
	target, _ := embeddingTaskTargetConfig(task.Config)
	scope := stringFromConfig(target["scope"])
	req := EmbeddingExecutionRequest{
		Scope: EmbeddingExecutionScope(scope),
		Target: EmbeddingExecutionTarget{
			EngineID:  uintFromConfig(target["engine_id"]),
			ItemID:    uintFromConfig(target["item_id"]),
			NodeID:    uintFromConfig(target["node_id"]),
			Locator:   stringFromConfig(target["locator"]),
			Recursive: boolFromConfig(target["recursive"], true),
		},
		Config: task.Config.Clone(),
	}
	return task.Config.Clone(), req, nil
}

func (s *EmbeddingTaskService) prepareEmbeddingTaskDefinition(task *models.EmbeddingTask) error {
	if task == nil {
		return errors.New("embedding task is nil")
	}
	task.Name = strings.TrimSpace(task.Name)
	task.Description = strings.TrimSpace(task.Description)
	task.Schedule = strings.TrimSpace(task.Schedule)
	if task.Name == "" {
		return errors.New("embedding task name is required")
	}
	if len(task.Config) == 0 {
		return errors.New("embedding task config is required")
	}
	target, err := embeddingTaskTargetConfig(task.Config)
	if err != nil {
		return err
	}
	scope := stringFromConfig(target["scope"])
	switch EmbeddingExecutionScope(scope) {
	case EmbeddingExecutionScopeItem:
		if uintFromConfig(target["item_id"]) == 0 {
			return errors.New("embedding task config.target.item_id is required")
		}
	case EmbeddingExecutionScopeNode:
		if uintFromConfig(target["node_id"]) == 0 {
			return errors.New("embedding task config.target.node_id is required")
		}
	default:
		return fmt.Errorf("embedding task config.target.scope must be item or node, got %q", scope)
	}
	if uintFromConfig(target["engine_id"]) == 0 {
		return errors.New("embedding task config.target.engine_id is required")
	}
	if err := s.normalizeEmbeddingTaskConfig(task); err != nil {
		return err
	}
	if task.Schedule == "" {
		task.NextRunAt = nil
		return nil
	}
	builder := commonScheduler.NewExpressionBuilder()
	if err := builder.Validate(task.Schedule); err != nil {
		return fmt.Errorf("invalid embedding task schedule: %w", err)
	}
	nextRunAt, err := builder.NextRunTime(task.Schedule, embeddingScheduleNow())
	if err != nil {
		return fmt.Errorf("calculate embedding task next_run_at: %w", err)
	}
	task.NextRunAt = &nextRunAt
	return nil
}

func (s *EmbeddingTaskService) normalizeEmbeddingTaskConfig(task *models.EmbeddingTask) error {
	if task.Config == nil {
		task.Config = commonModels.JSONMap{}
	}
	embeddingCfg, ok := asJSONMap(task.Config["embedding"])
	if !ok {
		embeddingCfg = commonModels.JSONMap{}
	}
	if s != nil && strings.TrimSpace(s.currentModel) != "" {
		if configured := stringFromConfig(embeddingCfg["model"]); configured != "" && configured != s.currentModel {
			return fmt.Errorf("embedding task config.embedding.model must match current manager embedding model %q", s.currentModel)
		}
		embeddingCfg["model"] = s.currentModel
	}
	if s != nil && s.currentDimension > 0 {
		if configured := intFromConfig(embeddingCfg["dimension"]); configured > 0 && configured != s.currentDimension {
			return fmt.Errorf("embedding task config.embedding.dimension must match current manager embedding dimension %d", s.currentDimension)
		}
		embeddingCfg["dimension"] = s.currentDimension
	}
	if len(embeddingCfg) > 0 {
		task.Config["embedding"] = embeddingCfg
	}
	filters, ok := asJSONMap(task.Config["filters"])
	if !ok {
		filters = commonModels.JSONMap{}
	}
	if s != nil && s.currentMaxFileMB > 0 && intFromConfig(filters["max_file_size_mb"]) <= 0 {
		filters["max_file_size_mb"] = s.currentMaxFileMB
	}
	if len(filters) > 0 {
		task.Config["filters"] = filters
	}
	return nil
}

func embeddingTaskTargetConfig(config commonModels.JSONMap) (commonModels.JSONMap, error) {
	target, ok := asJSONMap(config["target"])
	if !ok {
		return nil, errors.New("embedding task config.target is required")
	}
	return target, nil
}

func asJSONMap(value interface{}) (commonModels.JSONMap, bool) {
	switch v := value.(type) {
	case commonModels.JSONMap:
		return v, true
	case map[string]interface{}:
		return commonModels.JSONMap(v), true
	default:
		return nil, false
	}
}

func uintFromConfig(value interface{}) uint {
	switch v := value.(type) {
	case uint:
		return v
	case int:
		if v > 0 {
			return uint(v)
		}
	case int64:
		if v > 0 {
			return uint(v)
		}
	case float64:
		if v > 0 {
			return uint(v)
		}
	}
	return 0
}

func intFromConfig(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case uint:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

func stringFromConfig(value interface{}) string {
	if v, ok := value.(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func boolFromConfig(value interface{}, defaultValue bool) bool {
	if v, ok := value.(bool); ok {
		return v
	}
	return defaultValue
}

func embeddingScheduleNow() time.Time {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.Now()
	}
	return time.Now().In(loc)
}
