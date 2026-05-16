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
	"github.com/addp/transfer/pkg/postprocessor"
)

// ExecutionEngineService 执行引擎服务，负责任务执行的核心逻辑
type ExecutionEngineService struct {
	engine           *pipeline.ExecutionEngine
	parallelEngine   *pipeline.ParallelExecutionEngine
	registry         *pipeline.ConnectorRegistry
	stateManager     *pipeline.StateManager
	taskRepo         *repository.TaskRepository
	executionService *ExecutionService // 使用统一执行表服务
	mappingRepo      *repository.MappingRepository
	localEngineRepo  *repository.LocalEngineRepository
	systemClient     *commonClient.SystemClient
	metaClient       *commonClient.MetaClient
	cfg              *config.Config
	logger           *slog.Logger
}

// NewExecutionEngineService 创建执行引擎服务
func NewExecutionEngineService(
	engine *pipeline.ExecutionEngine,
	taskRepo *repository.TaskRepository,
	executionService *ExecutionService, // 改为使用 ExecutionService
	mappingRepo *repository.MappingRepository,
	localEngineRepo *repository.LocalEngineRepository,
	systemClient *commonClient.SystemClient,
	metaClient *commonClient.MetaClient,
	cfg *config.Config,
) *ExecutionEngineService {
	// 获取引擎内部组件以创建并行引擎
	// 注意：这里我们假设传入的 engine 已经被正确初始化
	// 我们需要从 cmd/worker/main.go 传入 registry 和 stateManager
	// 暂时使用普通引擎的组件创建并行引擎

	service := &ExecutionEngineService{
		engine:           engine,
		taskRepo:         taskRepo,
		executionService: executionService,
		mappingRepo:      mappingRepo,
		localEngineRepo:  localEngineRepo,
		systemClient:     systemClient,
		metaClient:       metaClient,
		cfg:              cfg,
		logger:           logger.With("component", "execution_engine_service"),
	}

	return service
}

// SetEngineComponents 设置引擎组件（用于创建并行引擎）
func (s *ExecutionEngineService) SetEngineComponents(
	registry *pipeline.ConnectorRegistry,
	stateManager *pipeline.StateManager,
) {
	s.registry = registry
	s.stateManager = stateManager

	// 创建并行引擎
	parallelConfig := &pipeline.ParallelEngineConfig{
		EngineConfig: pipeline.DefaultEngineConfig(),
		NumReaders:   1, // 单个 Reader
		NumWriters:   4, // 4 个并行 Writer（可根据任务配置调整）
	}
	s.parallelEngine = pipeline.NewParallelExecutionEngine(
		registry,
		stateManager,
		s.logger,
		parallelConfig,
	)
}

// ExecuteTask 执行任务（由 Worker 调用）
func (s *ExecutionEngineService) ExecuteTask(ctx context.Context, taskID, executionID uint) error {
	s.logger.Info("executing task", "task_id", taskID, "execution_id", executionID)

	// 获取任务
	task, err := s.taskRepo.GetByID(taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	// 获取字段映射
	mappings, err := s.mappingRepo.GetByTaskID(taskID)
	if err != nil {
		s.logger.Warn("failed to get mappings", "error", err)
	}

	// 构建执行任务
	execTask, err := s.buildExecutionTask(task, executionID, mappings)
	if err != nil {
		s.updateExecutionError(task, executionID, err)
		return err
	}

	// 更新执行状态为运行中
	if err := s.executionService.UpdateStatus(ctx, executionID, models.ExecutionStatusRunning); err != nil {
		s.logger.Warn("failed to update execution status", "error", err)
	}

	// 选择执行引擎：暂时禁用并行引擎（MaxParallelism 字段已删除）
	var executeErr error
	var metrics *pipeline.Metrics
	useParallel := false // 暂时禁用并行引擎

	if useParallel {
		// 并行引擎逻辑（暂时不使用）
		// 使用并行引擎执行
		executeErr = s.parallelEngine.Execute(ctx, execTask)

		// 并行引擎完成后，获取指标和日志（从基础引擎）
		if s.parallelEngine.ExecutionEngine != nil {
			metrics = s.parallelEngine.ExecutionEngine.GetMetrics()
			logs := s.parallelEngine.ExecutionEngine.GetLogs()
			s.updateExecutionMetricsWithLogs(executionID, logs, metrics)
		}
	} else {
		s.logger.Info("using serial execution engine", "task_id", taskID)

		// 设置进度回调以定期更新日志和指标
		s.engine.SetProgressCallback(func(logs string, metrics *pipeline.Metrics) {
			s.updateExecutionMetricsWithLogs(executionID, logs, metrics)
		})

		// ✅ 设置后处理回调（在 Writer Close 前执行）
		s.engine.SetPostProcessCallback(func(ctx context.Context, execTaskInner *pipeline.ExecutionTask, writer pipeline.Writer) error {
			return s.executePostProcessing(ctx, task, execTaskInner, executionID)
		})

		// 使用串行引擎执行
		executeErr = s.engine.Execute(ctx, execTask)

		// 获取执行指标并更新（不更新日志，因为日志已通过 AppendLog 实时追加）
		metrics = s.engine.GetMetrics()
		s.updateExecutionMetrics(executionID, metrics)
	}

	// 检查执行错误
	if executeErr != nil {
		s.logger.Error("task execution failed", "error", executeErr, "task_id", taskID)

		if useParallel && s.parallelEngine.ExecutionEngine != nil {
			s.updateExecutionMetricsWithLogs(executionID, s.parallelEngine.ExecutionEngine.GetLogs(), nil)
		} else {
			s.updateExecutionMetricsWithLogs(executionID, s.engine.GetLogs(), nil)
		}

		s.updateExecutionError(task, executionID, executeErr)
		return executeErr
	}

	// 完成执行
	if err := s.executionService.FinishExecution(ctx, executionID, models.ExecutionStatusSuccess, ""); err != nil {
		s.logger.Warn("failed to finish execution", "error", err)
	}

	// 任务执行完成，恢复为空闲状态
	if err := s.taskRepo.UpdateFields(taskID, map[string]interface{}{
		"status":   models.TaskStatusIdle,
		"progress": 100.0,
	}); err != nil {
		s.logger.Warn("failed to update task after successful execution", "error", err, "task_id", taskID)
	}

	s.logger.Info("task executed successfully", "task_id", taskID, "records", metrics.RecordsWritten)

	// 后处理已通过回调机制在 Execute() 内部执行（Writer.Flush() 之后、Writer.Close() 之前）
	// 不再需要在此处调用 executePostProcessing

	// 如果任务配置了自动扫描元数据，触发 Meta 模块扫描
	if task.AutoScanMetadata {
		s.triggerMetadataScan(task)
	}

	return nil
}

// buildExecutionTask 构建执行任务
func (s *ExecutionEngineService) buildExecutionTask(
	task *models.TransferTask,
	executionID uint,
	mappings []models.FieldMapping,
) (*pipeline.ExecutionTask, error) {
	// 解析配置
	config := task.Config

	// 获取 source 配置（支持从 System 获取资源配置）
	sourceConfig, err := s.resolveConnectorConfig(task, "source")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve source config: %w", err)
	}

	// 获取 target 配置（支持从 System 获取资源配置）
	targetConfig, err := s.resolveConnectorConfig(task, "target")
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

	// 性能优化：对于 PostgreSQL 目标，自动选择高性能 COPY Writer（性能提升10倍）
	if targetType == "jdbc" {
		if driver, ok := targetConfig["driver"].(string); ok && driver == "postgresql" {
			targetType = "postgres_copy"
			s.logger.Info("auto-selected PostgresCOPYWriter for performance optimization",
				"original_type", "jdbc",
				"new_type", "postgres_copy",
				"performance_gain", "10x faster than INSERT")
		}
	}

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
		ExecutionID: executionID,
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
		Mode:       pipeline.ModeBatch, // 统一使用批处理模式
	}, nil
}

// buildTransform 构建转换器
func (s *ExecutionEngineService) buildTransform(config map[string]interface{}) (pipeline.Transform, error) {
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
func (s *ExecutionEngineService) inferConnectorType(config map[string]interface{}) string {
	// 优先使用 engine_type（最准确的来源）
	if engineType, ok := config["engine_type"].(string); ok {
		switch engineType {
		case "spatialite", "sqlite":
			return "spatialite"
		case "postgresql", "mysql":
			return "jdbc"
		case "s3", "minio", "oss":
			return "s3"
		case "kafka":
			return "kafka"
		case "csv", "geojson", "shapefile", "geopackage":
			return "file"
		case "parquet":
			return "parquet"
		default:
			// 对于未知的 engine_type，直接使用它
			return engineType
		}
	}

	// 后备：使用显式指定的 type
	if connType, ok := config["type"].(string); ok && connType != "" {
		return connType
	}

	// 最后：根据配置字段推断
	if _, ok := config["driver"]; ok {
		return "jdbc"
	}
	if _, ok := config["bucket"]; ok {
		return "s3"
	}
	if _, ok := config["path"]; ok {
		return "file"
	}
	if _, ok := config["full_name"]; ok {
		return "file"
	}
	if _, ok := config["topic"]; ok {
		return "kafka"
	}

	return "unknown"
}

// updateExecutionError 更新执行错误
func (s *ExecutionEngineService) updateExecutionError(task *models.TransferTask, executionID uint, execErr error) {
	if execErr == nil {
		return
	}

	ctx := context.Background()
	// 更新execution记录为failed（关键操作，失败时记录ERROR）
	if err := s.executionService.FinishExecution(ctx, executionID, models.ExecutionStatusFailed, execErr.Error()); err != nil {
		s.logger.Error("CRITICAL: failed to mark execution as failed - status inconsistency may occur",
			"error", err,
			"execution_id", executionID,
			"task_id", task.ID)
		// 即使execution更新失败，也继续更新task状态，避免两边都失败
	} else {
		s.logger.Info("execution marked as failed", "execution_id", executionID)
	}

	// 任务执行失败，恢复为空闲状态
	updates := map[string]interface{}{
		"status":   models.TaskStatusIdle,
		"progress": 0,
	}

	if err := s.taskRepo.UpdateFields(task.ID, updates); err != nil {
		s.logger.Error("CRITICAL: failed to update task after execution error - status inconsistency may occur",
			"error", err,
			"task_id", task.ID,
			"execution_id", executionID)
	} else {
		s.logger.Info("task status updated after execution failure",
			"task_id", task.ID,
			"status", "idle")
	}
}

// updateExecutionMetricsWithLogs 更新执行指标和日志
func (s *ExecutionEngineService) updateExecutionMetricsWithLogs(executionID uint, logs string, metrics *pipeline.Metrics) {
	ctx := context.Background()
	metricsMap := map[string]interface{}{}

	// 日志存储到 error_details.logs
	if logs != "" {
		errorDetails := commonModels.JSONMap{"logs": logs}
		metricsMap["error_details"] = errorDetails
	}

	if metrics != nil {
		metricsMap["records_read"] = metrics.RecordsRead
		metricsMap["records_written"] = metrics.RecordsWritten
		metricsMap["bytes_read"] = metrics.BytesRead
		metricsMap["bytes_written"] = metrics.BytesWritten
	}

	if err := s.executionService.UpdateMetrics(ctx, executionID, metricsMap); err != nil {
		s.logger.Error("failed to update execution metrics", "error", err, "execution_id", executionID)
	}
}

// updateExecutionMetrics 仅更新执行指标（不更新日志）
// 用于执行完成后更新指标，避免覆盖通过 AppendLog 追加的日志
func (s *ExecutionEngineService) updateExecutionMetrics(executionID uint, metrics *pipeline.Metrics) {
	if metrics == nil {
		return
	}

	ctx := context.Background()
	metricsMap := map[string]interface{}{
		"records_read":    metrics.RecordsRead,
		"records_written": metrics.RecordsWritten,
		"bytes_read":      metrics.BytesRead,
		"bytes_written":   metrics.BytesWritten,
	}

	if err := s.executionService.UpdateMetrics(ctx, executionID, metricsMap); err != nil {
		s.logger.Error("failed to update execution metrics", "error", err, "execution_id", executionID)
	}
}

// GetResourceConfig 从 System 模块获取资源配置
func (s *ExecutionEngineService) GetResourceConfig(ctx context.Context, engineID uint) (*commonModels.Engine, error) {
	if s.systemClient == nil {
		return nil, fmt.Errorf("system client not available (integration disabled)")
	}

	s.logger.Info("fetching resource config from System", "engine_id", engineID)

	resource, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		s.logger.Error("failed to get resource from System", "engine_id", engineID, "error", err)
		return nil, fmt.Errorf("failed to get resource %d from System: %w", engineID, err)
	}

	s.logger.Info("resource config fetched successfully", "engine_id", engineID, "type", resource.EngineType)
	return resource, nil
}

// resolveConnectorConfig 解析连接器配置（从 config 中读取 engine_id）
func (s *ExecutionEngineService) resolveConnectorConfig(
	task *models.TransferTask,
	configKey string, // "source" 或 "target"
) (map[string]interface{}, error) {
	// 从 task.config 中读取 source/target 配置
	connectorConfig, ok := task.Config[configKey].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid %s config in task", configKey)
	}

	// 检查 scope 字段，判断是系统引擎还是本地引擎
	scope, _ := connectorConfig["scope"].(string)

	switch scope {
	case "system":
		// 从 System 模块获取引擎配置
		if engineIDFloat, ok := connectorConfig["engine_id"].(float64); ok {
			engineID := uint(engineIDFloat)
			return s.resolveSystemEngine(engineID, connectorConfig)
		}
		// 如果没有 engine_id，直接返回配置
		return connectorConfig, nil

	case "local":
		// 从 transfer.local_engines 获取本地引擎配置
		if engineIDFloat, ok := connectorConfig["engine_id"].(float64); ok {
			engineID := uint(engineIDFloat)
			return s.resolveLocalEngine(engineID, task.TenantID, connectorConfig)
		}
		// 如果没有 engine_id，直接返回配置
		return connectorConfig, nil

	default:
		// 没有 scope 或其他情况，直接使用配置
		return connectorConfig, nil
	}
}

// resolveSystemEngine 从 System 模块获取引擎配置
func (s *ExecutionEngineService) resolveSystemEngine(engineID uint, taskConfig map[string]interface{}) (map[string]interface{}, error) {
	resource, err := s.GetResourceConfig(context.Background(), engineID)
	if err != nil {
		s.logger.Warn("failed to get resource from System, using task config",
			"engine_id", engineID, "error", err)
		return taskConfig, nil
	}

	// 转换为连接器配置
	connectorConfig, err := s.resourceToConnectorConfig(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to convert resource to connector config: %w", err)
	}

	// 合并 task.config 中的额外配置（如 query, table 等）
	s.logger.Info("merging task config", "resource_type", resource.EngineType)
	for k, v := range taskConfig {
		// 不覆盖资源配置中的连接信息
		if k != "host" && k != "port" && k != "user" && k != "password" &&
			k != "database" && k != "driver" && k != "endpoint" &&
			k != "access_key" && k != "secret_key" && k != "bucket" &&
			k != "scope" && k != "engine_id" {

			// 特殊处理：将 output_path/output_format/output_file_name 映射到 S3Writer 期望的字段名
			if k == "output_path" && (resource.EngineType == "minio" || resource.EngineType == "s3") {
				// output_path 是目录前缀，映射到 prefix
				s.logger.Info("mapping output_path to prefix", "value", v)
				connectorConfig["prefix"] = v
			} else if k == "output_format" && (resource.EngineType == "minio" || resource.EngineType == "s3") {
				// output_format 映射到 file_type（S3Writer 期望的字段名）
				s.logger.Info("mapping output_format to file_type", "value", v)
				connectorConfig["file_type"] = v
			} else if k == "output_file_name" && (resource.EngineType == "minio" || resource.EngineType == "s3") {
				// output_file_name 映射到 file_name
				s.logger.Info("mapping output_file_name to file_name", "value", v)
				connectorConfig["file_name"] = v
			} else if k == "csv_delimiter" && (resource.EngineType == "minio" || resource.EngineType == "s3") {
				// csv_delimiter 映射到 delimiter（S3Writer/CSVWriter 期望的字段名）
				connectorConfig["delimiter"] = v
			} else if k == "path" && (resource.EngineType == "minio" || resource.EngineType == "s3") {
				s.logger.Info("mapping path to file_name", "value", v)
				connectorConfig["file_name"] = v
			} else if k == "format" && (resource.EngineType == "minio" || resource.EngineType == "s3") {
				// 将 format 映射到 file_type（S3Writer 期望的字段名）
				s.logger.Info("mapping format to file_type", "value", v)
				connectorConfig["file_type"] = v
			} else if k == "connector_type" && (resource.EngineType == "minio" || resource.EngineType == "s3") {
				// parquet 数据源：覆盖 type 为 parquet，让注册表路由到 ParquetReader
				if v == "parquet" {
					connectorConfig["type"] = "parquet"
					connectorConfig["engine_type"] = "parquet"
					s.logger.Info("parquet source: overriding connector type to parquet")
				}
			} else if k == "output_path" && resource.EngineType == "nfs" {
				// NFS: output_path 映射到 path（NFSWriter 期望的字段名）
				connectorConfig["path"] = v
			} else if k == "output_format" && resource.EngineType == "nfs" {
				connectorConfig["file_type"] = v
			} else if k == "output_file_name" && resource.EngineType == "nfs" {
				connectorConfig["file_name"] = v
			} else if k == "csv_delimiter" && resource.EngineType == "nfs" {
				connectorConfig["delimiter"] = v
			} else {
				s.logger.Debug("adding config", "key", k, "value", v)
				connectorConfig[k] = v
			}
		}
	}

	s.logger.Info("final connector config", "config", connectorConfig)

	// 当引擎未配置 bucket 时，从 prefix 第一段提取 bucket 名
	if resource.EngineType == "s3" || resource.EngineType == "minio" {
		if bucket, _ := connectorConfig["bucket"].(string); bucket == "" {
			if prefix, ok := connectorConfig["prefix"].(string); ok && prefix != "" {
				trimmed := strings.TrimPrefix(prefix, "/")
				parts := strings.SplitN(trimmed, "/", 2)
				if parts[0] != "" {
					connectorConfig["bucket"] = parts[0]
					if len(parts) > 1 {
						connectorConfig["prefix"] = parts[1]
					} else {
						connectorConfig["prefix"] = ""
					}
					s.logger.Info("extracted bucket from prefix",
						"bucket", parts[0],
						"remaining_prefix", connectorConfig["prefix"])
				}
			}
		}
	}

	return connectorConfig, nil
}

// resolveLocalEngine 从 transfer.local_engines 获取本地引擎配置
func (s *ExecutionEngineService) resolveLocalEngine(engineID uint, tenantID uint, taskConfig map[string]interface{}) (map[string]interface{}, error) {
	// 从数据库读取本地引擎配置
	localEngine, err := s.localEngineRepo.GetByID(engineID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to get local engine %d: %w", engineID, err)
	}

	// 获取引擎的连接信息
	connInfo := localEngine.ConnectionInfo
	if connInfo == nil {
		return nil, fmt.Errorf("local engine %d has no connection info", engineID)
	}

	// 合并连接信息到 taskConfig
	for k, v := range connInfo {
		taskConfig[k] = v
	}

	// 将 engine_type 映射到 type 字段（用于注册表查找）
	engineType := localEngine.EngineType
	switch engineType {
	case "spatialite", "sqlite":
		taskConfig["type"] = "spatialite"
	case "postgresql", "mysql":
		taskConfig["type"] = "jdbc"
		taskConfig["driver"] = engineType
	case "s3", "minio":
		taskConfig["type"] = "s3"
	default:
		// 其他类型直接使用 engine_type 作为 type
		taskConfig["type"] = engineType
	}

	taskConfig["engine_type"] = engineType

	s.logger.Info("mapped engine_type to connector type",
		"engine_id", engineID,
		"engine_type", engineType,
		"connector_type", taskConfig["type"])

	s.logger.Info("using local engine config", "engine_id", engineID, "config", taskConfig)
	return taskConfig, nil
}

// resourceToConnectorConfig 将 Resource 转换为连接器配置
func (s *ExecutionEngineService) resourceToConnectorConfig(resource *commonModels.Engine) (map[string]interface{}, error) {
	connInfo := resource.ConnectionInfo
	connectorConfig := make(map[string]interface{})

	// 根据资源类型转换配置
	switch resource.EngineType {
	case "postgresql", "mysql":
		// JDBC 连接器配置
		connectorConfig["type"] = "jdbc"
		connectorConfig["driver"] = resource.EngineType
		if host, ok := connInfo["host"].(string); ok {
			connectorConfig["host"] = host
		}
		if port, ok := connInfo["port"].(float64); ok {
			connectorConfig["port"] = int(port)
		}
		// 兼容两种字段名: username 和 user
		if username, ok := connInfo["username"].(string); ok {
			connectorConfig["username"] = username
		} else if user, ok := connInfo["user"].(string); ok {
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

	case "nfs":
		// NFS 连接器配置
		connectorConfig["type"] = "nfs"
		if server, ok := connInfo["server"].(string); ok {
			connectorConfig["server"] = server
		}
		if exportPath, ok := connInfo["export_path"].(string); ok {
			connectorConfig["export_path"] = exportPath
		}

	default:
		return nil, fmt.Errorf("unsupported resource type: %s", resource.EngineType)
	}

	s.logger.Info("converted resource to connector config",
		"engine_id", resource.ID,
		"resource_type", resource.EngineType,
		"connector_type", connectorConfig["type"])

	return connectorConfig, nil
}

// ========== 辅助函数 ==========

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

// triggerMetadataScan 触发元数据扫描
func (s *ExecutionEngineService) triggerMetadataScan(task *models.TransferTask) {
	// 如果 metaClient 未初始化，跳过
	if s.metaClient == nil {
		s.logger.Warn("meta client not available, skipping metadata scan", "task_id", task.ID)
		return
	}

	// 设置租户 ID（关键：Meta 模块需要 tenant_id 来插入 meta_node）
	s.metaClient.SetTenantID(&task.TenantID)

	// 从 task.Config.target 中提取 engine_id
	targetConfig, ok := task.Config["target"].(map[string]interface{})
	if !ok {
		s.logger.Warn("invalid target config, skipping metadata scan", "task_id", task.ID)
		return
	}

	// 获取 engine_id
	engineIDFloat, ok := targetConfig["engine_id"].(float64)
	if !ok {
		s.logger.Warn("no engine_id found in target config, skipping metadata scan", "task_id", task.ID)
		return
	}
	engineID := uint(engineIDFloat)

	// 检查 scope，本地引擎暂不支持元数据扫描（因为 Meta 模块目前只管理系统引擎）
	scope, _ := targetConfig["scope"].(string)
	if scope == "local" {
		s.logger.Info("local engine detected, skipping metadata scan", "task_id", task.ID, "engine_id", engineID)
		return
	}

	// 提取 catalog path（结构化引擎的 schema/database 也是顶层 catalog path）
	var catalogPaths []string
	if schema, ok := targetConfig["schema"].(string); ok && schema != "" {
		catalogPaths = []string{schema}
	} else if database, ok := targetConfig["database"].(string); ok && database != "" {
		catalogPaths = []string{database}
	}

	// 调用 Meta 模块的扫描 API
	s.logger.Info("triggering metadata scan",
		"task_id", task.ID,
		"engine_id", engineID,
		"catalog_paths", catalogPaths)

	if err := s.metaClient.TriggerScanEngine(engineID, catalogPaths); err != nil {
		// 元数据扫描失败不影响任务本身的成功状态
		s.logger.Error("failed to trigger metadata scan",
			"error", err,
			"task_id", task.ID,
			"engine_id", engineID)
	} else {
		s.logger.Info("metadata scan triggered successfully",
			"task_id", task.ID,
			"engine_id", engineID)
	}
}

// executePostProcessing 执行后处理任务
// 在数据传输完成后，根据目标引擎类型执行相应的优化任务（主键、索引、统计）
func (s *ExecutionEngineService) executePostProcessing(
	ctx context.Context,
	task *models.TransferTask,
	execTask *pipeline.ExecutionTask,
	executionID uint,
) error {
	s.logger.Debug("checking if post-processing is needed", "task_id", task.ID)

	// 记录日志到前端（添加空行分隔）
	s.logToExecution(executionID, "")
	s.logToExecution(executionID, "[后处理] 开始执行后处理任务...")

	// 1. 判断是否应该执行后处理（仅数据库 Writer）
	writerType := s.inferConnectorType(execTask.TargetConfig.Config)
	if !s.shouldRunPostProcessing(writerType) {
		s.logger.Debug("post-processing not needed for writer type", "writer_type", writerType)
		s.logToExecution(executionID, fmt.Sprintf("[后处理] 跳过后处理 (Writer类型: %s 不需要后处理)", writerType))
		return nil
	}

	// 2. 获取引擎类型
	engineType, err := s.getEngineType(task.Config)
	if err != nil {
		s.logger.Warn("failed to get engine type", "error", err)
		return fmt.Errorf("failed to get engine type: %w", err)
	}

	// 3. 检查是否有对应的后处理器
	if !postprocessor.GlobalRegistry().HasPostProcessor(engineType) {
		s.logger.Debug("no postprocessor for engine", "engine", engineType)
		s.logToExecution(executionID, fmt.Sprintf("[后处理] 引擎 %s 无后处理器，跳过", engineType))
		return nil
	}

	s.logger.Info("starting post-processing", "engine_type", engineType, "task_id", task.ID)
	s.logToExecution(executionID, fmt.Sprintf("[后处理] 引擎类型: %s", engineType))

	// ✅ 新增：获取最终的 Schema（包含检测到的主键信息）
	finalSchema := s.engine.GetFinalSchema()
	if finalSchema != nil && finalSchema.PrimaryKey != nil {
		s.logger.Info("detected primary key from source table",
			"columns", finalSchema.PrimaryKey.Columns,
			"task_id", task.ID)
		s.logToExecution(executionID, fmt.Sprintf("[后处理] 检测到主键: %v", finalSchema.PrimaryKey.Columns))
	}

	// 4. 构建后处理器配置
	ppConfig, err := s.buildPostProcessorConfig(task, execTask, engineType, finalSchema, executionID)
	if err != nil {
		s.logToExecution(executionID, fmt.Sprintf("[后处理] ❌ 配置构建失败: %v", err))
		return fmt.Errorf("failed to build postprocessor config: %w", err)
	}

	// 记录后处理任务信息
	s.logToExecution(executionID, fmt.Sprintf("[后处理] 目标表: %s", ppConfig.TableName))
	if ppConfig.CreatePrimaryKey {
		s.logToExecution(executionID, "[后处理] ✓ 主键创建: 启用")
	}
	if ppConfig.CreateSpatialIndex {
		s.logToExecution(executionID, fmt.Sprintf("[后处理] ✓ 空间索引创建: 启用 (发现 %d 个几何字段)", len(ppConfig.GeometryColumns)))
		for colName, meta := range ppConfig.GeometryColumns {
			s.logToExecution(executionID, fmt.Sprintf("[后处理]   - 字段: %s, SRID: %d, 类型: %s", colName, meta.SRID, meta.SpatialType))
		}
	}
	if ppConfig.UpdateStatistics {
		s.logToExecution(executionID, "[后处理] ✓ 统计信息更新: 启用")
	}

	// 5. 创建后处理器
	pp, err := postprocessor.GlobalRegistry().NewPostProcessor(ppConfig)
	if err != nil {
		s.logToExecution(executionID, fmt.Sprintf("[后处理] ❌ 后处理器创建失败: %v", err))
		return fmt.Errorf("failed to create postprocessor: %w", err)
	}
	defer pp.Close()

	// 6. 初始化后处理器
	if err := pp.Initialize(ctx, ppConfig); err != nil {
		s.logToExecution(executionID, fmt.Sprintf("[后处理] ❌ 后处理器初始化失败: %v", err))
		return fmt.Errorf("failed to initialize postprocessor: %w", err)
	}

	// 7. 执行后处理任务
	s.logToExecution(executionID, "[后处理] 开始执行优化任务...")
	if err := pp.Execute(ctx); err != nil {
		s.logToExecution(executionID, fmt.Sprintf("[后处理] ❌ 后处理执行失败: %v", err))
		return fmt.Errorf("post-processing failed: %w", err)
	}

	s.logger.Info("✅ [后处理] 后处理任务完成", "engine_type", engineType, "task_id", task.ID)
	s.logToExecution(executionID, "[后处理] ✅ 所有后处理任务已完成")
	return nil
}

// getEngineType 从任务配置中获取引擎类型
func (s *ExecutionEngineService) getEngineType(taskConfig map[string]interface{}) (string, error) {
	targetConfig, ok := taskConfig["target"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid target config")
	}

	// 优先使用直接指定的 engine_type（如果有）
	if engineType, ok := targetConfig["engine_type"].(string); ok && engineType != "" {
		return engineType, nil
	}

	// 否则通过 engine_id 查询 System 模块
	engineIDFloat, ok := targetConfig["engine_id"].(float64)
	if !ok {
		return "", fmt.Errorf("engine_id not found in target config")
	}

	engineInfo, err := s.systemClient.GetEngine(uint(engineIDFloat))
	if err != nil {
		return "", fmt.Errorf("failed to get engine info from System module: %w", err)
	}

	return engineInfo.EngineType, nil
}

// buildPostProcessorConfig 构建后处理器配置
func (s *ExecutionEngineService) buildPostProcessorConfig(
	task *models.TransferTask,
	execTask *pipeline.ExecutionTask,
	engineType string,
	schema *pipeline.Schema, // ✅ 新增参数：最终 Schema（包含检测到的主键）
	executionID uint,
) (postprocessor.PostProcessorConfig, error) {
	targetConfig := execTask.TargetConfig.Config

	// 提取表名
	tableName, ok := targetConfig["table"].(string)
	if !ok {
		return postprocessor.PostProcessorConfig{}, fmt.Errorf("table name not found in target config")
	}

	// ✅ 新增：优先使用 Writer 的数据库连接
	writerDB := s.engine.GetWriterDB()
	if writerDB != nil {
		s.logger.Debug("using writer's database connection for post-processing",
			"table", tableName)
	} else {
		s.logger.Debug("writer connection not available, will create new connection",
			"table", tableName)
	}

	config := postprocessor.PostProcessorConfig{
		EngineType:       engineType,
		TableName:        tableName,
		DB:               writerDB,     // ✅ 优先使用 Writer 连接
		ConnectionConfig: targetConfig, // 降级方案（如果 DB 为 nil）
		Schema:           schema,       // ✅ 新增：传递 Schema（供主键提取使用）

		// 默认启用所有任务
		CreatePrimaryKey:   true,
		CreateSpatialIndex: true,
		UpdateStatistics:   true,
	}

	// 从 Writer 配置提取开关
	if val, ok := targetConfig["create_primary_key"].(bool); ok {
		config.CreatePrimaryKey = val
	}
	if val, ok := targetConfig["create_spatial_index"].(bool); ok {
		config.CreateSpatialIndex = val
	}

	// 提取主键配置
	if forcePK, ok := targetConfig["force_primary_key"].([]interface{}); ok {
		config.ForcePrimaryKey = make([]string, 0, len(forcePK))
		for _, v := range forcePK {
			if pkCol, ok := v.(string); ok {
				config.ForcePrimaryKey = append(config.ForcePrimaryKey, pkCol)
			}
		}
	}
	if pkName, ok := targetConfig["primary_key_name"].(string); ok {
		config.PrimaryKeyName = pkName
	}

	// 提取索引配置
	if indexType, ok := targetConfig["index_type"].(string); ok {
		config.IndexType = indexType
	}
	if indexPrefix, ok := targetConfig["spatial_index_prefix"].(string); ok {
		config.SpatialIndexPrefix = indexPrefix
	}

	// 提取空间列配置（从 Schema 和配置中）
	config.GeometryColumns = s.extractGeometryColumns(targetConfig, schema)

	return config, nil
}

// shouldRunPostProcessing 判断是否应该执行后处理
func (s *ExecutionEngineService) shouldRunPostProcessing(writerType string) bool {
	// 数据库类型的 Writer 需要后处理
	dbWriterTypes := []string{
		"postgresql", "postgres", "postgres_copy",
		"mysql", "jdbc",
		"doris", "clickhouse",
	}

	writerTypeLower := strings.ToLower(writerType)
	for _, t := range dbWriterTypes {
		if writerTypeLower == t {
			return true
		}
	}

	return false
}

// extractGeometryColumns 从配置和 Schema 中提取空间列信息
func (s *ExecutionEngineService) extractGeometryColumns(config map[string]interface{}, schema *pipeline.Schema) map[string]postprocessor.GeometryColumnMetadata {
	result := make(map[string]postprocessor.GeometryColumnMetadata)

	// 默认 SRID
	defaultSRID := 4326
	if configSRID, ok := config["srid"].(float64); ok {
		defaultSRID = int(configSRID)
	} else if configSRID, ok := config["srid"].(int); ok {
		defaultSRID = configSRID
	}

	// ✅ 优先级 1: 从 Schema 的 Fields 中自动识别 geometry 类型字段
	if schema != nil && len(schema.Fields) > 0 {
		for _, field := range schema.Fields {
			if strings.EqualFold(field.Type, "geometry") {
				srid := field.SRID
				if srid == 0 {
					srid = defaultSRID
				}
				result[field.Name] = postprocessor.GeometryColumnMetadata{
					ColumnName:  field.Name,
					SRID:        srid,
					SpatialType: field.SpatialType,
				}
				s.logger.Debug("detected geometry field from schema",
					"field", field.Name,
					"srid", srid,
					"spatial_type", field.SpatialType)
			}
		}
	}

	// 优先级 2: 从 geometry_columns 配置提取（补充或覆盖）
	if geomCols, ok := config["geometry_columns"].([]interface{}); ok {
		for _, col := range geomCols {
			if colName, ok := col.(string); ok && colName != "" {
				// 如果 Schema 中已经存在，跳过（Schema 优先）
				if _, exists := result[colName]; exists {
					continue
				}

				result[colName] = postprocessor.GeometryColumnMetadata{
					ColumnName:  colName,
					SRID:        defaultSRID,
					SpatialType: "Geometry", // 默认类型
				}
			}
		}
	}

	// 优先级 3: 从 geometry_field 配置提取（单个几何字段）
	if geomField, ok := config["geometry_field"].(string); ok && geomField != "" {
		if _, exists := result[geomField]; !exists {
			result[geomField] = postprocessor.GeometryColumnMetadata{
				ColumnName:  geomField,
				SRID:        defaultSRID,
				SpatialType: "Geometry",
			}
		}
	}

	s.logger.Info("extracted geometry columns for post-processing",
		"count", len(result),
		"columns", getGeometryColumnNames(result))

	return result
}

// getGeometryColumnNames 辅助函数：提取几何列名称列表
func getGeometryColumnNames(geomCols map[string]postprocessor.GeometryColumnMetadata) []string {
	names := make([]string, 0, len(geomCols))
	for name := range geomCols {
		names = append(names, name)
	}
	return names
}

// logToExecution 将日志记录到执行记录中（前端可见）
func (s *ExecutionEngineService) logToExecution(executionID uint, message string) {
	ctx := context.Background()
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logLine := fmt.Sprintf("%s [INFO] %s", timestamp, message)
	if err := s.executionService.AppendLog(ctx, executionID, logLine); err != nil {
		s.logger.Warn("failed to append log to execution", "error", err, "execution_id", executionID)
	}
}
