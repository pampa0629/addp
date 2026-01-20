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
	engine            *pipeline.ExecutionEngine
	parallelEngine    *pipeline.ParallelExecutionEngine
	registry          *pipeline.ConnectorRegistry
	stateManager      *pipeline.StateManager
	taskRepo          *repository.TaskRepository
	execRepo          *repository.ExecutionRepository
	mappingRepo       *repository.MappingRepository
	localEngineRepo   *repository.LocalEngineRepository
	systemClient      *commonClient.SystemClient
	metaClient        *commonClient.MetaClient
	cfg               *config.Config
	logger            *slog.Logger
}

// NewExecutionEngineService 创建执行引擎服务
func NewExecutionEngineService(
	engine *pipeline.ExecutionEngine,
	taskRepo *repository.TaskRepository,
	execRepo *repository.ExecutionRepository,
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
		engine:          engine,
		taskRepo:        taskRepo,
		execRepo:        execRepo,
		mappingRepo:     mappingRepo,
		localEngineRepo: localEngineRepo,
		systemClient:    systemClient,
		metaClient:      metaClient,
		cfg:             cfg,
		logger:          logger.With("component", "execution_engine_service"),
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

	// 如果任务配置了自动扫描元数据，触发 Meta 模块扫描
	if task.AutoScanMetadata {
		s.triggerMetadataScan(task)
	}

	return nil
}

// buildExecutionTask 构建执行任务
func (s *ExecutionEngineService) buildExecutionTask(
	task *models.Task,
	execution *models.TaskExecution,
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

// resolveConnectorConfig 解析连接器配置（从 config 中读取 engine_id）
func (s *ExecutionEngineService) resolveConnectorConfig(
	task *models.Task,
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

			// 特殊处理：将 path/scope 映射到 file_name/prefix（用于 S3 Writer）
			if k == "path" && (resource.EngineType == "minio" || resource.EngineType == "s3") {
				s.logger.Info("mapping path to file_name", "value", v)
				connectorConfig["file_name"] = v
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

	s.logger.Info("final connector config", "config", connectorConfig)
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

// triggerMetadataScan 触发元数据扫描
func (s *ExecutionEngineService) triggerMetadataScan(task *models.Task) {
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

	// 提取 schema_names（如果有的话）
	var schemaNames []string
	if schema, ok := targetConfig["schema"].(string); ok && schema != "" {
		schemaNames = []string{schema}
	} else if database, ok := targetConfig["database"].(string); ok && database != "" {
		// 对于某些数据库，database 字段等同于 schema
		schemaNames = []string{database}
	}

	// 调用 Meta 模块的扫描 API
	s.logger.Info("triggering metadata scan",
		"task_id", task.ID,
		"engine_id", engineID,
		"schema_names", schemaNames)

	if err := s.metaClient.TriggerScanEngine(engineID, schemaNames); err != nil {
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
