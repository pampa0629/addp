package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/pkg/pipeline"
	"gorm.io/gorm"
)

// TaskQueue 任务队列接口（避免循环依赖）
type TaskQueue interface {
	EnqueueExecuteTask(ctx context.Context, taskID, executionID, tenantID uint) error
	Close() error
}

// TaskService 任务服务
type TaskService struct {
	taskRepo     *repository.TaskRepository
	execRepo     *repository.ExecutionRepository
	mappingRepo  *repository.MappingRepository
	engine       *pipeline.ExecutionEngine
	cfg          *config.Config
	systemClient *commonClient.SystemClient
	taskQueue    TaskQueue
	logger       *slog.Logger
}

// NewTaskService 创建任务服务
func NewTaskService(
	db *gorm.DB,
	engine *pipeline.ExecutionEngine,
	cfg *config.Config,
	taskQueue TaskQueue,
) *TaskService {
	// 创建 SystemClient（使用内部 API Key 进行服务间调用）
	var systemClient *commonClient.SystemClient
	if cfg.EnableIntegration && cfg.SystemServiceURL != "" {
		if cfg.InternalAPIKey != "" {
			// 使用内部 API Key（推荐方式）
			systemClient = commonClient.NewSystemClientWithInternalKey(
				cfg.SystemServiceURL,
				cfg.InternalAPIKey,
			)
			slog.Info("SystemClient initialized with internal API key",
				"system_url", cfg.SystemServiceURL)
		} else {
			// Fallback：无认证（仅开发环境）
			systemClient = commonClient.NewSystemClient(cfg.SystemServiceURL, "")
			slog.Warn("SystemClient initialized without authentication - not recommended for production",
				"system_url", cfg.SystemServiceURL)
		}
	}

	return &TaskService{
		taskRepo:     repository.NewTaskRepository(db),
		execRepo:     repository.NewExecutionRepository(db),
		mappingRepo:  repository.NewMappingRepository(db),
		engine:       engine,
		taskQueue:    taskQueue,
		cfg:          cfg,
		systemClient: systemClient,
		logger:       logger.With("component", "task_service"),
	}
}

// CreateTask 创建任务
func (s *TaskService) CreateTask(ctx context.Context, req *models.CreateTaskRequest, tenantID, userID uint) (*models.Task, error) {
	s.logger.Info("creating task", "name", req.Name, "type", req.Type, "tenant_id", tenantID)

	// 构建任务对象
	task := &models.Task{
		Name:           req.Name,
		Description:    req.Description,
		Type:           req.Type,
		Mode:           req.Mode,
		SourceID:       req.SourceID,
		TargetID:       req.TargetID,
		Config:         req.Config,
		Schedule:       req.Schedule,
		BatchSize:      req.BatchSize,
		MaxParallelism: req.MaxParallelism,
		Status:         models.TaskStatusPending,
		Progress:       0,
		TenantID:       tenantID,
		CreatedBy:      &userID,
	}

	// 设置默认值
	if task.Mode == "" {
		task.Mode = models.TaskModeBatch
	}
	if task.BatchSize == 0 {
		task.BatchSize = 1000
	}
	if task.MaxParallelism == 0 {
		task.MaxParallelism = 1
	}

	// 创建任务
	if err := s.taskRepo.Create(task); err != nil {
		s.logger.Error("failed to create task", "error", err)
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	// 创建字段映射
	if len(req.Mappings) > 0 {
		for i := range req.Mappings {
			req.Mappings[i].TaskID = task.ID
		}
		if err := s.mappingRepo.CreateBatch(req.Mappings); err != nil {
			s.logger.Warn("failed to create mappings", "error", err)
		}
	}

	s.logger.Info("task created successfully", "task_id", task.ID)
	return task, nil
}

// GetTask 获取任务
func (s *TaskService) GetTask(ctx context.Context, id, tenantID uint) (*models.Task, error) {
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
func (s *TaskService) UpdateTask(ctx context.Context, id, tenantID uint, req *models.UpdateTaskRequest) (*models.Task, error) {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}

	// 运行中的任务需要先停止才能更新
	if task.Status == models.TaskStatusRunning {
		return nil, fmt.Errorf("cannot update task in %s status", task.Status)
	}

	// 更新字段
	if req.Name != nil {
		task.Name = *req.Name
	}
	if req.Description != nil {
		task.Description = *req.Description
	}
	if req.Config != nil {
		task.Config = req.Config
	}
	if req.Schedule != nil {
		task.Schedule = *req.Schedule
	}
	if req.BatchSize != nil {
		task.BatchSize = *req.BatchSize
	}
	if req.MaxParallelism != nil {
		task.MaxParallelism = *req.MaxParallelism
	}
	if req.Status != nil {
		task.Status = *req.Status
	}

	if err := s.taskRepo.Update(task); err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	s.logger.Info("task updated", "task_id", id)
	return task, nil
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

	// 删除字段映射
	if err := s.mappingRepo.DeleteByTaskID(id); err != nil {
		s.logger.Warn("failed to delete mappings", "error", err)
	}

	// 删除任务
	if err := s.taskRepo.Delete(id); err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}

	s.logger.Info("task deleted", "task_id", id)
	return nil
}

// ListTasks 列出任务
func (s *TaskService) ListTasks(ctx context.Context, tenantID uint, req *models.ListTasksRequest) ([]models.Task, int64, error) {
	if req.Page == 0 {
		req.Page = 1
	}
	if req.PageSize == 0 {
		req.PageSize = 20
	}

	filters := make(map[string]interface{})
	if req.Type != nil {
		filters["type"] = *req.Type
	}
	if req.Status != nil {
		filters["status"] = *req.Status
	}

	return s.taskRepo.List(tenantID, filters, req.Page, req.PageSize)
}

// GetTaskStatistics 获取任务统计
func (s *TaskService) GetTaskStatistics(ctx context.Context, tenantID uint) (*models.TaskStatistics, error) {
	return s.taskRepo.GetStatistics(tenantID)
}

// StartTask 启动任务（立即执行）
func (s *TaskService) StartTask(ctx context.Context, id, tenantID, userID uint) (*models.TaskExecution, error) {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return nil, err
	}

	// 检查任务状态
	if task.Status == models.TaskStatusRunning {
		return nil, fmt.Errorf("task is already running")
	}

	s.logger.Info("starting task", "task_id", id)

	now := time.Now()

	// 创建执行记录
	execution := &models.TaskExecution{
		TaskID:      id,
		Status:      models.ExecutionStatusPending,
		StartTime:   models.LocalTime{Time: now}, // 使用 LocalTime 包装
		TriggerType: "manual",
		TriggerBy:   &userID,
	}

	if err := s.execRepo.Create(execution); err != nil {
		return nil, fmt.Errorf("failed to create execution: %w", err)
	}

	task.Status = models.TaskStatusRunning
	task.LastExecutionID = &execution.ID
	task.LastExecutionStatus = models.ExecutionStatusRunning
	task.LastExecutionStartedAt = &now
	task.LastExecutionFinishedAt = nil

	if err := s.taskRepo.UpdateFields(id, map[string]interface{}{
		"status":                     models.TaskStatusRunning,
		"progress":                   0,
		"last_execution_id":          execution.ID,
		"last_execution_status":      models.ExecutionStatusRunning,
		"last_execution_started_at":  now,
		"last_execution_finished_at": nil,
	}); err != nil {
		s.logger.Warn("failed to update task state", "error", err, "task_id", id)
	}

	// 将任务提交到队列（Asynq）
	if s.taskQueue != nil {
		if err := s.taskQueue.EnqueueExecuteTask(ctx, id, execution.ID, tenantID); err != nil {
			s.logger.Error("failed to enqueue task", "error", err, "task_id", id)
			finalStatus := models.TaskStatusPending
			if task.Schedule != "" {
				finalStatus = models.TaskStatusScheduled
			}
			finishedAt := time.Now()
			s.execRepo.FinishExecution(execution.ID, models.ExecutionStatusFailed, err.Error())
			if err := s.taskRepo.UpdateFields(id, map[string]interface{}{
				"status":                     finalStatus,
				"last_execution_status":      models.ExecutionStatusFailed,
				"last_execution_finished_at": finishedAt,
				"progress":                   0,
			}); err != nil {
				s.logger.Warn("failed to rollback task state after enqueue failure", "error", err, "task_id", id)
			}
			return nil, fmt.Errorf("failed to enqueue task: %w", err)
		}
		s.logger.Info("task enqueued to worker", "task_id", id, "execution_id", execution.ID)
	} else {
		s.logger.Warn("task queue not available, task will not be executed", "task_id", id)
	}

	s.logger.Info("task started", "task_id", id, "execution_id", execution.ID)
	return execution, nil
}

// StopTask 停止任务
func (s *TaskService) StopTask(ctx context.Context, id, tenantID uint) error {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return err
	}

	if task.Status != models.TaskStatusRunning {
		return fmt.Errorf("task is not running")
	}

	now := time.Now()

	if task.LastExecutionID != nil {
		if err := s.execRepo.FinishExecution(*task.LastExecutionID, models.ExecutionStatusFailed, "execution stopped by user"); err != nil {
			s.logger.Warn("failed to finish execution when stopping task", "error", err, "task_id", id)
		}
	}

	updates := map[string]interface{}{
		"status":                     models.TaskStatusStopped,
		"last_execution_status":      models.ExecutionStatusFailed,
		"last_execution_finished_at": now,
		"progress":                   0,
	}

	if err := s.taskRepo.UpdateFields(id, updates); err != nil {
		return fmt.Errorf("failed to stop task: %w", err)
	}

	s.logger.Info("task stopped", "task_id", id)
	return nil
}

// PauseTask 暂停任务
func (s *TaskService) PauseTask(ctx context.Context, id, tenantID uint) error {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return err
	}

	if task.Schedule == "" {
		return fmt.Errorf("manual tasks cannot be paused")
	}

	updates := map[string]interface{}{
		"status":   models.TaskStatusPaused,
		"progress": 0,
	}

	if task.Status == models.TaskStatusRunning && task.LastExecutionID != nil {
		now := time.Now()
		if err := s.execRepo.FinishExecution(*task.LastExecutionID, models.ExecutionStatusFailed, "task paused by user"); err != nil {
			s.logger.Warn("failed to finish execution when pausing task", "error", err, "task_id", id)
		}
		updates["last_execution_status"] = models.ExecutionStatusFailed
		updates["last_execution_finished_at"] = now
	}

	if err := s.taskRepo.UpdateFields(id, updates); err != nil {
		return fmt.Errorf("failed to pause task: %w", err)
	}

	s.logger.Info("task paused", "task_id", id)
	return nil
}

// ResumeTask 恢复任务
func (s *TaskService) ResumeTask(ctx context.Context, id, tenantID uint) error {
	task, err := s.GetTask(ctx, id, tenantID)
	if err != nil {
		return err
	}

	if task.Schedule == "" {
		return fmt.Errorf("manual tasks do not support resume")
	}

	if task.Status != models.TaskStatusPaused && task.Status != models.TaskStatusPending {
		return fmt.Errorf("task cannot be resumed from current status")
	}

	updates := map[string]interface{}{
		"status":   models.TaskStatusScheduled,
		"progress": 0,
	}

	if err := s.taskRepo.UpdateFields(id, updates); err != nil {
		return fmt.Errorf("failed to resume task: %w", err)
	}

	s.logger.Info("task resumed", "task_id", id)
	return nil
}

// ExecuteTask 执行任务（由 Worker 调用）
func (s *TaskService) ExecuteTask(ctx context.Context, taskID, executionID uint) error {
	s.logger.Info("executing task", "task_id", taskID, "execution_id", executionID)

	// 获取任务
	task, err := s.taskRepo.GetByID(taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// 获取执行记录
	execution, err := s.execRepo.GetByID(executionID)
	if err != nil {
		return fmt.Errorf("failed to get execution: %w", err)
	}

	// 获取字段映射
	mappings, err := s.mappingRepo.GetByTaskID(taskID)
	if err != nil {
		s.logger.Warn("failed to get mappings", "error", err)
	}

	// 构建执行任务
	execTask, err := s.buildExecutionTask(task, execution, mappings)
	if err != nil {
		s.updateExecutionError(task, executionID, err)
		return err
	}

	// 更新执行状态为运行中
	if err := s.execRepo.UpdateStatus(executionID, models.ExecutionStatusRunning); err != nil {
		s.logger.Warn("failed to update execution status", "error", err)
	}

	// 设置进度回调以定期更新日志和指标
	s.engine.SetProgressCallback(func(logs string, metrics *pipeline.Metrics) {
		if err := s.execRepo.UpdateMetrics(executionID, map[string]interface{}{
			"logs":            logs,
			"records_read":    metrics.RecordsRead,
			"records_written": metrics.RecordsWritten,
			"bytes_read":      metrics.BytesRead,
			"bytes_written":   metrics.BytesWritten,
		}); err != nil {
			s.logger.Error("failed to update execution metrics", "error", err, "execution_id", executionID)
		}
	})

	// 执行任务
	if err := s.engine.Execute(ctx, execTask); err != nil {
		s.logger.Error("task execution failed", "error", err, "task_id", taskID)

		// 获取执行日志（包括错误日志）
		executionLogs := s.engine.GetLogs()

		// 更新执行记录，包含日志
		s.execRepo.UpdateMetrics(executionID, map[string]interface{}{
			"logs": executionLogs,
		})

		s.updateExecutionError(task, executionID, err)
		return err
	}

	// 获取执行指标和日志
	metrics := s.engine.GetMetrics()
	executionLogs := s.engine.GetLogs()

	// 更新执行指标和日志
	s.execRepo.UpdateMetrics(executionID, map[string]interface{}{
		"records_read":    metrics.RecordsRead,
		"records_written": metrics.RecordsWritten,
		"bytes_read":      metrics.BytesRead,
		"bytes_written":   metrics.BytesWritten,
		"logs":            executionLogs,
	})

	// 完成执行
	if err := s.execRepo.FinishExecution(executionID, models.ExecutionStatusSuccess, ""); err != nil {
		s.logger.Warn("failed to finish execution", "error", err)
	}

	finishedAt := time.Now()
	finalStatus := models.TaskStatusCompleted
	if task.Schedule != "" {
		finalStatus = models.TaskStatusScheduled
	}

	if err := s.taskRepo.UpdateFields(taskID, map[string]interface{}{
		"status":                     finalStatus,
		"progress":                   100.0,
		"last_execution_id":          executionID,
		"last_execution_status":      models.ExecutionStatusSuccess,
		"last_execution_finished_at": finishedAt,
	}); err != nil {
		s.logger.Warn("failed to update task after successful execution", "error", err, "task_id", taskID)
	}

	s.logger.Info("task executed successfully", "task_id", taskID, "records", metrics.RecordsWritten)
	return nil
}

// buildExecutionTask 构建执行任务
func (s *TaskService) buildExecutionTask(
	task *models.Task,
	execution *models.TaskExecution,
	mappings []models.DataMapping,
) (*pipeline.ExecutionTask, error) {
	// 解析配置
	config := task.Config

	// 获取 source 配置（支持从 System 获取资源配置）
	sourceConfig, err := s.resolveConnectorConfig(config, "source", task.SourceID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve source config: %w", err)
	}

	// 获取 target 配置（支持从 System 获取资源配置）
	targetConfig, err := s.resolveConnectorConfig(config, "target", task.TargetID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve target config: %w", err)
	}

	geometryFields := extractGeometryFields(targetConfig)
	if len(geometryFields) == 0 {
		geometryFields = extractGeometryFields(sourceConfig)
	}

	// 构建转换器
	transforms := make([]pipeline.Transform, 0)

	// 添加字段映射转换
	if len(mappings) > 0 {
		fieldMappings := make([]pipeline.FieldMapping, len(mappings))
		for i, m := range mappings {
			fieldMappings[i] = pipeline.FieldMapping{
				Source:       m.SourceField,
				Target:       m.TargetField,
				Type:         m.FieldType,
				Format:       m.Format,
				DefaultValue: m.DefaultValue,
			}
		}
		ensureGeometryMappings(&fieldMappings, geometryFields)
		transforms = append(transforms, pipeline.NewFieldMappingTransform(fieldMappings))
	}

	// 从配置中添加其他转换
	if transformsConfig, ok := config["transforms"].([]interface{}); ok {
		for _, tc := range transformsConfig {
			transformConfig, ok := tc.(map[string]interface{})
			if !ok {
				continue
			}

			transform, err := s.buildTransform(transformConfig)
			if err != nil {
				s.logger.Warn("failed to build transform", "error", err)
				continue
			}
			transforms = append(transforms, transform)
		}
	}

	// 确定连接器类型
	sourceType := s.inferConnectorType(sourceConfig)
	targetType := s.inferConnectorType(targetConfig)

	// 自动注入空间转换，适配对象存储的空间格式要求
	if targetType == "s3" && !hasSpatialTransform(transforms) {
		if len(geometryFields) > 0 {
			spatialFormat := normalizeSpatialFormat(targetConfig)
			spatialConfig := map[string]interface{}{
				"geometry_fields": stringSliceToInterface(geometryFields),
			}

			// 从 targetConfig 中读取 source_format，默认为 wkb
			if sourceFormat, ok := targetConfig["source_format"].(string); ok && sourceFormat != "" {
				spatialConfig["source_format"] = sourceFormat
				s.logger.Info("using custom source_format from targetConfig", "source_format", sourceFormat)
			} else {
				spatialConfig["source_format"] = "wkb"
				s.logger.Info("using default source_format", "source_format", "wkb")
			}

			if spatialFormat != "" {
				spatialConfig["target_format"] = spatialFormat
			}

			s.logger.Info("building spatial transform", "config", spatialConfig)

			transform, err := pipeline.NewTransformByName("spatial", spatialConfig)
			if err != nil {
				s.logger.Warn("failed to build spatial transform", "error", err, "geometry_fields", geometryFields, "target_format", spatialFormat)
			} else {
				s.logger.Info("auto append spatial transform for object storage target",
					"geometry_fields", geometryFields,
					"source_format", spatialConfig["source_format"],
					"target_format", spatialFormat)
				transforms = append(transforms, transform)
			}
		}
	}

	// 构建执行任务
	return &pipeline.ExecutionTask{
		TaskID:      task.ID,
		ExecutionID: execution.ID,
		SourceConfig: pipeline.ConnectorConfig{
			Type:      sourceType,
			Config:    sourceConfig,
			BatchSize: task.BatchSize,
		},
		TargetConfig: pipeline.ConnectorConfig{
			Type:   targetType,
			Config: targetConfig,
		},
		Transforms: transforms,
		Mode:       s.mapTaskMode(task.Mode),
	}, nil
}

// buildTransform 构建转换器
func (s *TaskService) buildTransform(config map[string]interface{}) (pipeline.Transform, error) {
	transformType, ok := config["type"].(string)
	if !ok {
		return nil, fmt.Errorf("transform type not specified")
	}

	normalized := make(map[string]interface{})
	for k, v := range config {
		if k == "type" {
			continue
		}
		normalized[k] = v
	}

	if pipeline.HasTransformRegistered(transformType) {
		return pipeline.NewTransformByName(transformType, normalized)
	}

	switch transformType {
	case "filter":
		conditionsData, _ := json.Marshal(config["conditions"])
		var conditions []pipeline.FilterCondition
		if err := json.Unmarshal(conditionsData, &conditions); err != nil {
			return nil, err
		}
		mode, _ := config["mode"].(string)
		return pipeline.NewFilterTransform(conditions, mode), nil

	case "rename":
		mappings, ok := config["mappings"].(map[string]string)
		if !ok {
			return nil, fmt.Errorf("invalid rename mappings")
		}
		return pipeline.NewRenameFieldsTransform(mappings), nil

	case "select":
		fieldsData, _ := json.Marshal(config["fields"])
		var fields []string
		if err := json.Unmarshal(fieldsData, &fields); err != nil {
			return nil, err
		}
		return pipeline.NewSelectFieldsTransform(fields), nil
	}

	return nil, fmt.Errorf("unknown transform type: %s", transformType)
}

// inferConnectorType 推断连接器类型
func (s *TaskService) inferConnectorType(config map[string]interface{}) string {
	// 优先使用显式指定的类型
	if connType, ok := config["type"].(string); ok {
		return connType
	}

	// 根据配置推断
	if _, ok := config["driver"]; ok {
		return "jdbc"
	}
	if _, ok := config["bucket"]; ok {
		return "s3"
	}
	if _, ok := config["path"]; ok {
		return "file"
	}
	if _, ok := config["topic"]; ok {
		return "kafka"
	}

	return "unknown"
}

// mapTaskMode 映射任务模式
func (s *TaskService) mapTaskMode(mode models.TaskMode) pipeline.ReaderMode {
	switch mode {
	case models.TaskModeStream:
		return pipeline.ModeStream
	case models.TaskModeMicroBatch:
		return pipeline.ModeMicroBatch
	default:
		return pipeline.ModeBatch
	}
}

func hasSpatialTransform(transforms []pipeline.Transform) bool {
	for _, t := range transforms {
		if strings.EqualFold(t.Name(), "SpatialTransform") {
			return true
		}
	}
	return false
}

func extractGeometryFields(config map[string]interface{}) []string {
	if config == nil {
		return nil
	}

	if fields, ok := config["geometry_fields"]; ok {
		return interfaceToStringSlice(fields)
	}

	if field, ok := config["geometry_field"].(string); ok {
		field = strings.TrimSpace(field)
		if field != "" {
			return []string{field}
		}
	}

	return nil
}

func normalizeSpatialFormat(config map[string]interface{}) string {
	if config == nil {
		return ""
	}

	if value, ok := config["spatial_format"].(string); ok && value != "" {
		return strings.ToLower(value)
	}

	if value, ok := config["file_type"].(string); ok && value != "" {
		return mapFileTypeToSpatialFormat(strings.ToLower(value))
	}

	if value, ok := config["format"].(string); ok && value != "" {
		return mapFileTypeToSpatialFormat(strings.ToLower(value))
	}

	return ""
}

func mapFileTypeToSpatialFormat(fileType string) string {
	switch fileType {
	case "geojson":
		return "geojson"
	case "csv-wkt":
		return "wkt"
	case "shapefile":
		return "wkb"
	case "ewkb", "ewkt", "hexwkb", "wkb", "wkt":
		return fileType
	default:
		return ""
	}
}

func interfaceToStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case []string:
		return filterEmptyStrings(v)
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			switch val := item.(type) {
			case string:
				if trimmed := strings.TrimSpace(val); trimmed != "" {
					result = append(result, trimmed)
				}
			case fmt.Stringer:
				if trimmed := strings.TrimSpace(val.String()); trimmed != "" {
					result = append(result, trimmed)
				}
			default:
				str := strings.TrimSpace(fmt.Sprintf("%v", val))
				if str != "" {
					result = append(result, str)
				}
			}
		}
		return result
	case string:
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return []string{trimmed}
		}
		return nil
	default:
		return nil
	}
}

func stringSliceToInterface(values []string) []interface{} {
	if len(values) == 0 {
		return nil
	}
	result := make([]interface{}, 0, len(values))
	for _, val := range values {
		if trimmed := strings.TrimSpace(val); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func filterEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, val := range values {
		if trimmed := strings.TrimSpace(val); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func ensureGeometryMappings(fieldMappings *[]pipeline.FieldMapping, geometryFields []string) {
	if fieldMappings == nil || len(*fieldMappings) == 0 || len(geometryFields) == 0 {
		return
	}

	existing := make(map[string]struct{}, len(*fieldMappings))
	for _, m := range *fieldMappings {
		existing[strings.ToLower(m.Target)] = struct{}{}
	}

	for _, field := range geometryFields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		lower := strings.ToLower(field)
		if _, ok := existing[lower]; ok {
			continue
		}
		*fieldMappings = append(*fieldMappings, pipeline.FieldMapping{
			Source: field,
			Target: field,
		})
		existing[lower] = struct{}{}
	}
}

// updateExecutionError 更新执行错误
func (s *TaskService) updateExecutionError(task *models.Task, executionID uint, execErr error) {
	if execErr == nil {
		return
	}

	if err := s.execRepo.FinishExecution(executionID, models.ExecutionStatusFailed, execErr.Error()); err != nil {
		s.logger.Warn("failed to mark execution as failed", "error", err, "execution_id", executionID)
	}

	finishedAt := time.Now()
	finalStatus := models.TaskStatusStopped
	if task.Schedule != "" {
		finalStatus = models.TaskStatusScheduled
	}

	updates := map[string]interface{}{
		"status":                     finalStatus,
		"last_execution_id":          executionID,
		"last_execution_status":      models.ExecutionStatusFailed,
		"last_execution_finished_at": finishedAt,
		"progress":                   0,
	}

	if err := s.taskRepo.UpdateFields(task.ID, updates); err != nil {
		s.logger.Warn("failed to update task after execution error", "error", err, "task_id", task.ID)
	}
}

// GetResourceConfig 从 System 模块获取资源配置
func (s *TaskService) GetResourceConfig(ctx context.Context, resourceID uint) (*commonModels.Resource, error) {
	if s.systemClient == nil {
		return nil, fmt.Errorf("system client not available (integration disabled)")
	}

	s.logger.Info("fetching resource config from System", "resource_id", resourceID)

	resource, err := s.systemClient.GetResource(resourceID)
	if err != nil {
		s.logger.Error("failed to get resource from System", "resource_id", resourceID, "error", err)
		return nil, fmt.Errorf("failed to get resource %d from System: %w", resourceID, err)
	}

	s.logger.Info("resource config fetched successfully", "resource_id", resourceID, "type", resource.ResourceType)
	return resource, nil
}

// resolveConnectorConfig 解析连接器配置（优先从 System 获取，否则使用 task.config）
func (s *TaskService) resolveConnectorConfig(
	taskConfig models.JSONMap,
	configKey string, // "source" 或 "target"
	resourceID *uint,
) (map[string]interface{}, error) {
	// 情况1：如果提供了 resource_id，从 System 获取资源配置
	if resourceID != nil && *resourceID > 0 {
		resource, err := s.GetResourceConfig(context.Background(), *resourceID)
		if err != nil {
			s.logger.Warn("failed to get resource from System, falling back to task config",
				"resource_id", *resourceID, "error", err)
			// 如果获取失败，尝试从 task.config 中读取
		} else {
			// 成功获取资源配置，转换为连接器配置
			connectorConfig, err := s.resourceToConnectorConfig(resource)
			if err != nil {
				return nil, fmt.Errorf("failed to convert resource to connector config: %w", err)
			}

			// 合并 task.config 中的额外配置（如 query, table 等）
			if taskConnectorConfig, ok := taskConfig[configKey].(map[string]interface{}); ok {
				s.logger.Info("merging task config", "configKey", configKey, "resource_type", resource.ResourceType)
				for k, v := range taskConnectorConfig {
					// 不覆盖资源配置中的连接信息
					if k != "host" && k != "port" && k != "user" && k != "password" &&
						k != "database" && k != "driver" && k != "endpoint" &&
						k != "access_key" && k != "secret_key" && k != "bucket" {

						// 特殊处理：将 path/scope 映射到 file_name/prefix（用于 S3 Writer）
						if k == "path" && (resource.ResourceType == "minio" || resource.ResourceType == "s3") {
							s.logger.Info("mapping path to file_name", "value", v)
							connectorConfig["file_name"] = v
						} else if k == "scope" && (resource.ResourceType == "minio" || resource.ResourceType == "s3") {
							s.logger.Debug("ignoring scope for S3/MinIO target", "value", v)
							// scope对于对象存储不是目录，忽略
						} else if k == "format" && (resource.ResourceType == "minio" || resource.ResourceType == "s3") {
							// 将 format 映射到 file_type（S3Writer 期望的字段名）
							s.logger.Info("mapping format to file_type", "value", v)
							connectorConfig["file_type"] = v
						} else {
							s.logger.Debug("adding config", "key", k, "value", v)
							connectorConfig[k] = v
						}
					}
				}
			}

			s.logger.Info("final connector config", "config", connectorConfig)
			return connectorConfig, nil
		}
	}

	// 情况2：从 task.config 中读取配置（传统方式）
	connectorConfig, ok := taskConfig[configKey].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid %s config in task", configKey)
	}

	return connectorConfig, nil
}

// resourceToConnectorConfig 将 Resource 转换为连接器配置
func (s *TaskService) resourceToConnectorConfig(resource *commonModels.Resource) (map[string]interface{}, error) {
	connInfo := resource.ConnectionInfo
	connectorConfig := make(map[string]interface{})

	// 根据资源类型转换配置
	switch resource.ResourceType {
	case "postgresql", "mysql":
		// JDBC 连接器配置
		connectorConfig["type"] = "jdbc"
		connectorConfig["driver"] = resource.ResourceType
		if host, ok := connInfo["host"].(string); ok {
			connectorConfig["host"] = host
		}
		if port, ok := connInfo["port"].(float64); ok {
			connectorConfig["port"] = int(port)
		}
		// 注意：JDBCConfig 使用 "username" 而不是 "user"
		if user, ok := connInfo["user"].(string); ok {
			connectorConfig["username"] = user
		}
		if password, ok := connInfo["password"].(string); ok {
			connectorConfig["password"] = password
		}
		if database, ok := connInfo["database"].(string); ok {
			connectorConfig["database"] = database
		}
		if sslmode, ok := connInfo["sslmode"].(string); ok {
			connectorConfig["ssl_mode"] = sslmode
		}

	case "s3", "minio":
		// S3 连接器配置
		connectorConfig["type"] = "s3"
		if endpoint, ok := connInfo["endpoint"].(string); ok {
			connectorConfig["endpoint"] = endpoint
		}
		if accessKey, ok := connInfo["access_key"].(string); ok {
			connectorConfig["access_key"] = accessKey
		}
		if secretKey, ok := connInfo["secret_key"].(string); ok {
			connectorConfig["secret_key"] = secretKey
		}
		if bucket, ok := connInfo["bucket"].(string); ok {
			connectorConfig["bucket"] = bucket
		}
		if region, ok := connInfo["region"].(string); ok {
			connectorConfig["region"] = region
		}
		// 处理 use_ssl 字段（默认为 false，适用于本地 MinIO）
		if useSSL, ok := connInfo["use_ssl"].(bool); ok {
			connectorConfig["use_ssl"] = useSSL
		} else {
			connectorConfig["use_ssl"] = false
		}

	default:
		return nil, fmt.Errorf("unsupported resource type: %s", resource.ResourceType)
	}

	s.logger.Info("converted resource to connector config",
		"resource_id", resource.ID,
		"resource_type", resource.ResourceType,
		"connector_type", connectorConfig["type"])

	return connectorConfig, nil
}

// GetStatistics 获取任务统计信息
func (s *TaskService) GetStatistics(ctx context.Context, tenantID uint) (*models.TaskStatistics, error) {
	return s.taskRepo.GetStatistics(tenantID)
}

// CreateMapping 创建数据映射
func (s *TaskService) CreateMapping(ctx context.Context, taskID uint, req *models.CreateDataMappingRequest, tenantID, userID uint) (*models.DataMapping, error) {
	// 验证任务存在且属于该租户
	task, err := s.taskRepo.GetByID(taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}
	if task.TenantID != tenantID {
		return nil, fmt.Errorf("task not found or access denied")
	}

	mapping := models.DataMapping{
		TaskID:       taskID,
		SourceField:  req.SourceField,
		TargetField:  req.TargetField,
		Transform:    req.Transform,
		DefaultValue: req.DefaultValue,
		FieldType:    req.FieldType,
		Format:       req.Format,
		Nullable:     req.Nullable,
	}

	// 使用 CreateBatch 创建单个映射
	if err := s.mappingRepo.CreateBatch([]models.DataMapping{mapping}); err != nil {
		return nil, fmt.Errorf("failed to create mapping: %w", err)
	}

	return &mapping, nil
}

// GetTaskMappings 获取任务的所有映射
func (s *TaskService) GetTaskMappings(ctx context.Context, taskID, tenantID uint) ([]*models.DataMapping, error) {
	// 验证任务存在且属于该租户
	task, err := s.taskRepo.GetByID(taskID)
	if err != nil {
		return nil, fmt.Errorf("task not found: %w", err)
	}
	if task.TenantID != tenantID {
		return nil, fmt.Errorf("task not found or access denied")
	}

	mappings, err := s.mappingRepo.GetByTaskID(taskID)
	if err != nil {
		return nil, err
	}

	// 转换为指针数组
	result := make([]*models.DataMapping, len(mappings))
	for i := range mappings {
		result[i] = &mappings[i]
	}

	return result, nil
}

// DeleteMapping 删除数据映射
func (s *TaskService) DeleteMapping(ctx context.Context, mappingID, tenantID uint) error {
	// TODO: 需要在 MappingRepository 中添加 Delete 方法
	return fmt.Errorf("DeleteMapping not yet implemented")
}
