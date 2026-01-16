package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/config"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/repository"
	"github.com/addp/transfer/pkg/pipeline"
)

// ExecutionEngineService 执行引擎服务，负责任务执行的核心逻辑
type ExecutionEngineService struct {
	engine         *pipeline.ExecutionEngine
	parallelEngine *pipeline.ParallelExecutionEngine
	registry       *pipeline.ConnectorRegistry
	stateManager   *pipeline.StateManager
	taskRepo       *repository.TaskRepository
	execRepo       *repository.ExecutionRepository
	mappingRepo    *repository.MappingRepository
	systemClient   *commonClient.SystemClient
	cfg            *config.Config
	logger         *slog.Logger
}

// NewExecutionEngineService 创建执行引擎服务
func NewExecutionEngineService(
	engine *pipeline.ExecutionEngine,
	taskRepo *repository.TaskRepository,
	execRepo *repository.ExecutionRepository,
	mappingRepo *repository.MappingRepository,
	systemClient *commonClient.SystemClient,
	cfg *config.Config,
) *ExecutionEngineService {
	// 获取引擎内部组件以创建并行引擎
	// 注意：这里我们假设传入的 engine 已经被正确初始化
	// 我们需要从 cmd/worker/main.go 传入 registry 和 stateManager
	// 暂时使用普通引擎的组件创建并行引擎

	service := &ExecutionEngineService{
		engine:       engine,
		taskRepo:     taskRepo,
		execRepo:     execRepo,
		mappingRepo:  mappingRepo,
		systemClient: systemClient,
		cfg:          cfg,
		logger:       logger.With("component", "execution_engine_service"),
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
		NumReaders:   1,  // 单个 Reader
		NumWriters:   4,  // 4 个并行 Writer（可根据任务配置调整）
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

		// 使用串行引擎执行
		executeErr = s.engine.Execute(ctx, execTask)

		// 获取执行指标和日志并更新
		metrics = s.engine.GetMetrics()
		s.updateExecutionMetricsWithLogs(executionID, s.engine.GetLogs(), metrics)
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
	if err := s.execRepo.FinishExecution(executionID, models.ExecutionStatusSuccess, ""); err != nil {
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
	return nil
}

// buildExecutionTask 构建执行任务
func (s *ExecutionEngineService) buildExecutionTask(
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

// updateExecutionError 更新执行错误
func (s *ExecutionEngineService) updateExecutionError(task *models.Task, executionID uint, execErr error) {
	if execErr == nil {
		return
	}

	// 更新execution记录为failed（关键操作，失败时记录ERROR）
	if err := s.execRepo.FinishExecution(executionID, models.ExecutionStatusFailed, execErr.Error()); err != nil {
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
	metricsMap := map[string]interface{}{
		"logs": logs,
	}
	if metrics != nil {
		metricsMap["records_read"] = metrics.RecordsRead
		metricsMap["records_written"] = metrics.RecordsWritten
		metricsMap["bytes_read"] = metrics.BytesRead
		metricsMap["bytes_written"] = metrics.BytesWritten
	}

	if err := s.execRepo.UpdateMetrics(executionID, metricsMap); err != nil {
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

// resolveConnectorConfig 解析连接器配置（优先从 System 获取，否则使用 task.config）
func (s *ExecutionEngineService) resolveConnectorConfig(
	taskConfig models.JSONMap,
	configKey string, // "source" 或 "target"
	engineID *uint,
) (map[string]interface{}, error) {
	// 情况1：如果提供了 engine_id，从 System 获取资源配置
	if engineID != nil && *engineID > 0 {
		resource, err := s.GetResourceConfig(context.Background(), *engineID)
		if err != nil {
			s.logger.Warn("failed to get resource from System, falling back to task config",
				"engine_id", *engineID, "error", err)
			// 如果获取失败，尝试从 task.config 中读取
		} else {
			// 成功获取资源配置，转换为连接器配置
			connectorConfig, err := s.resourceToConnectorConfig(resource)
			if err != nil {
				return nil, fmt.Errorf("failed to convert resource to connector config: %w", err)
			}

			// 合并 task.config 中的额外配置（如 query, table 等）
			if taskConnectorConfig, ok := taskConfig[configKey].(map[string]interface{}); ok {
				s.logger.Info("merging task config", "configKey", configKey, "resource_type", resource.EngineType)
				for k, v := range taskConnectorConfig {
					// 不覆盖资源配置中的连接信息
					if k != "host" && k != "port" && k != "user" && k != "password" &&
						k != "database" && k != "driver" && k != "endpoint" &&
						k != "access_key" && k != "secret_key" && k != "bucket" {

						// 特殊处理：将 path/scope 映射到 file_name/prefix（用于 S3 Writer）
						if k == "path" && (resource.EngineType == "minio" || resource.EngineType == "s3") {
							s.logger.Info("mapping path to file_name", "value", v)
							connectorConfig["file_name"] = v
						} else if k == "scope" && (resource.EngineType == "minio" || resource.EngineType == "s3") {
							s.logger.Debug("ignoring scope for S3/MinIO target", "value", v)
							// scope对于对象存储不是目录，忽略
						} else if k == "format" && (resource.EngineType == "minio" || resource.EngineType == "s3") {
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
		if username, ok := connInfo["username"].(string); ok {
			connectorConfig["username"] = username
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
