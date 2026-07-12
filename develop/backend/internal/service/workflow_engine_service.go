package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path"
	"strconv"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/common/utils"
	"github.com/addp/develop/backend/internal/models"
)

// WorkflowEngineService 工作流执行引擎服务
// 通过 Common Engine 的 WorkflowRuntimeProvider 统一调用工作流运行时。
type WorkflowEngineService struct {
	systemClient *commonClient.SystemClient
}

var workflowRuntimeStatusPollInterval = 2 * time.Second

// NewWorkflowEngineService 创建工作流引擎服务
func NewWorkflowEngineService(systemClient *commonClient.SystemClient) *WorkflowEngineService {
	return &WorkflowEngineService{
		systemClient: systemClient,
	}
}

// WorkflowResponse 工作流执行响应
type WorkflowResponse struct {
	Status          string                   `json:"status"`
	ExecutionID     string                   `json:"execution_id"`
	FinalResult     string                   `json:"final_result,omitempty"` // GeoJSON 字符串
	AllResults      map[string]string        `json:"all_results,omitempty"`  // 所有中间结果
	ProducedTargets []WorkflowProducedTarget `json:"produced_targets,omitempty"`
	Error           string                   `json:"error,omitempty"`
	Logs            []map[string]interface{} `json:"logs,omitempty"`      // 执行日志
	Traceback       string                   `json:"traceback,omitempty"` // 详细堆栈信息
	ExecutionTimeMs *float64                 `json:"execution_time_ms,omitempty"`
	RuntimeStatus   map[string]interface{}   `json:"runtime_status,omitempty"`
}

type WorkflowProducedTarget struct {
	EngineID uint     `json:"engine_id"`
	Type     string   `json:"type"`
	Path     []string `json:"path"`
	Locator  string   `json:"locator"`
}

// ExecuteWorkflow 执行工作流（支持 JSONB 配置）
func (s *WorkflowEngineService) ExecuteWorkflow(
	ctx context.Context,
	workflowDef map[string]interface{},
	inputData map[string]interface{},
	executionConfig string,
) (*WorkflowResponse, error) {
	// 1. 解析执行配置
	var config models.WorkflowExecutionConfig
	if err := json.Unmarshal([]byte(executionConfig), &config); err != nil {
		return nil, fmt.Errorf("解析执行配置失败: %w", err)
	}

	// 2. 查询工作流引擎
	engine, err := s.systemClient.GetEngineByID(config.EngineID)
	if err != nil {
		return nil, fmt.Errorf("查询工作流引擎失败: %w", err)
	}

	// 3. 预处理数据源参数：将 locator / target_parent_locator 派生为 connection_info 和运行时路径。
	// Spark Workflow 的顶层运行时 engine_id 由 workflowRuntimeOptions 单独处理。
	preprocessedWorkflowDef, producedTargets, err := s.preprocessWorkflowParamsWithTargets(ctx, engine.EngineType, workflowDef)
	if err != nil {
		return nil, fmt.Errorf("预处理工作流参数失败: %w", err)
	}

	runtimeOptions, err := s.workflowRuntimeOptions(engine.EngineType, config)
	if err != nil {
		return nil, err
	}

	// 4. 通过 Common Engine 的 WorkflowRuntimeProvider 执行工作流
	workflowReq := plugin.WorkflowExecuteRequest{
		WorkflowDef: preprocessedWorkflowDef,
		InputData:   inputData,
		Runtime:     runtimeOptions,
	}
	result, err := dbbridge.ExecuteWorkflow(ctx, engine, workflowReq)
	if err != nil {
		return nil, fmt.Errorf("调用工作流引擎 %s 失败: %w", engine.Name, err)
	}

	resp := toWorkflowResponse(result)
	if strings.TrimSpace(resp.ExecutionID) == "" {
		return nil, fmt.Errorf("工作流运行时未返回 execution_id")
	}
	runtimeStatus, err := s.waitWorkflowRuntimeTerminalStatus(ctx, engine, resp.ExecutionID)
	if err != nil {
		return nil, err
	}
	applyWorkflowRuntimeStatus(resp, runtimeStatus)
	resp.ProducedTargets = producedTargets
	return resp, nil
}

func (s *WorkflowEngineService) waitWorkflowRuntimeTerminalStatus(
	ctx context.Context,
	engine *commonModels.Engine,
	runtimeExecutionID string,
) (*plugin.WorkflowExecutionStatus, error) {
	for {
		runtimeStatus, err := dbbridge.GetWorkflowExecutionStatus(ctx, engine, runtimeExecutionID)
		if err != nil {
			return nil, fmt.Errorf("查询工作流运行时执行状态失败: %w", err)
		}
		if runtimeStatus == nil {
			return nil, fmt.Errorf("工作流运行时未返回执行状态")
		}

		switch normalizeWorkflowRuntimeStatus(runtimeStatus.Status) {
		case "success":
			return runtimeStatus, nil
		case "failed", "error":
			return runtimeStatus, fmt.Errorf("工作流运行时执行失败: %s", workflowRuntimeStatusErrorMessage(runtimeStatus))
		case "cancelled", "canceled":
			return runtimeStatus, fmt.Errorf("工作流运行时执行已取消")
		case "running", "pending":
		default:
			return runtimeStatus, fmt.Errorf("工作流运行时返回未知状态: %s", runtimeStatus.Status)
		}

		timer := time.NewTimer(workflowRuntimeStatusPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return runtimeStatus, fmt.Errorf("等待工作流运行时执行完成超时: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func (s *WorkflowEngineService) workflowRuntimeOptions(engineType string, config models.WorkflowExecutionConfig) (map[string]interface{}, error) {
	sparkClusterID, hasSparkClusterID := config.EngineSpecific["spark_cluster_id"]
	if engineType != "spark_workflow" {
		if hasSparkClusterID {
			return nil, fmt.Errorf("只有 spark_workflow 可以配置 spark_cluster_id")
		}
		return nil, nil
	}

	if !hasSparkClusterID {
		return nil, fmt.Errorf("spark_workflow 执行必须配置 spark_cluster_id")
	}

	engineID, err := positiveUintFromInterface(sparkClusterID)
	if err != nil {
		return nil, fmt.Errorf("spark_cluster_id 必须是有效的 Spark 通用引擎资源 ID: %w", err)
	}

	sparkEngine, err := s.systemClient.GetEngineByID(engineID)
	if err != nil {
		return nil, fmt.Errorf("查询 Spark 通用引擎资源失败: %w", err)
	}
	if sparkEngine.EngineType != "spark" {
		return nil, fmt.Errorf("spark_cluster_id 必须指向 engine_type=spark 的通用引擎资源")
	}
	if !sparkEngine.IsActive {
		return nil, fmt.Errorf("spark_cluster_id 指向的 Spark 通用引擎资源未启用")
	}

	return map[string]interface{}{"engine_id": engineID}, nil
}

func positiveUintFromInterface(value interface{}) (uint, error) {
	switch v := value.(type) {
	case uint:
		if v == 0 {
			return 0, fmt.Errorf("值必须大于 0")
		}
		return v, nil
	case int:
		if v <= 0 {
			return 0, fmt.Errorf("值必须大于 0")
		}
		return uint(v), nil
	case float64:
		if v <= 0 || v != float64(uint(v)) {
			return 0, fmt.Errorf("值必须是正整数")
		}
		return uint(v), nil
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		if err != nil || parsed == 0 {
			return 0, fmt.Errorf("值必须是正整数")
		}
		return uint(parsed), nil
	default:
		return 0, fmt.Errorf("不支持的类型 %T", value)
	}
}

func toWorkflowResponse(result *plugin.WorkflowExecuteResult) *WorkflowResponse {
	if result == nil {
		return &WorkflowResponse{}
	}

	resp := &WorkflowResponse{
		Status:          result.Status,
		ExecutionID:     result.ExecutionID,
		Error:           result.Error,
		ExecutionTimeMs: result.ExecutionTimeMs,
	}
	if result.Result == nil {
		return resp
	}

	if finalResult, ok := result.Result["final_result"].(string); ok {
		resp.FinalResult = finalResult
	} else if finalResult, ok := result.Result["result"].(string); ok {
		resp.FinalResult = finalResult
	} else if finalResult, ok := result.Result["final_result"]; ok {
		if encoded, err := json.Marshal(finalResult); err == nil {
			resp.FinalResult = string(encoded)
		}
	}

	if rawResults, ok := result.Result["all_results"].(map[string]string); ok {
		resp.AllResults = rawResults
	} else if rawResults, ok := result.Result["all_results"].(map[string]interface{}); ok {
		resp.AllResults = make(map[string]string, len(rawResults))
		for key, value := range rawResults {
			switch v := value.(type) {
			case string:
				resp.AllResults[key] = v
			default:
				if encoded, err := json.Marshal(v); err == nil {
					resp.AllResults[key] = string(encoded)
				}
			}
		}
	}

	if logs, ok := result.Result["logs"].([]map[string]interface{}); ok {
		resp.Logs = logs
	}
	if traceback, ok := result.Result["traceback"].(string); ok {
		resp.Traceback = traceback
	}
	return resp
}

func applyWorkflowRuntimeStatus(resp *WorkflowResponse, status *plugin.WorkflowExecutionStatus) {
	if resp == nil || status == nil {
		return
	}
	if normalized := normalizeWorkflowRuntimeStatus(status.Status); normalized != "" {
		resp.Status = normalized
	}
	if status.ExecutionTimeMs != nil {
		resp.ExecutionTimeMs = status.ExecutionTimeMs
	}
	if status.Result != nil {
		resp.FinalResult = workflowRuntimeValueString(status.Result)
	}
	if len(status.AllResults) > 0 {
		resp.AllResults = workflowRuntimeResultMap(status.AllResults)
	}
	resp.RuntimeStatus = workflowRuntimeStatusSummary(status)
}

func workflowRuntimeStatusSummary(status *plugin.WorkflowExecutionStatus) map[string]interface{} {
	if status == nil {
		return nil
	}
	summary := map[string]interface{}{
		"status":               status.Status,
		"runtime_execution_id": status.ExecutionID,
		"progress":             status.Progress,
	}
	if status.Message != "" {
		summary["message"] = status.Message
	}
	if status.Error != "" {
		summary["error"] = status.Error
	}
	if status.ErrorCode != "" {
		summary["error_code"] = status.ErrorCode
	}
	if status.Details != "" {
		summary["details"] = status.Details
	}
	if status.StartedAt != "" {
		summary["started_at"] = status.StartedAt
	}
	if status.ExecutionTimeMs != nil {
		summary["execution_time_ms"] = *status.ExecutionTimeMs
	}
	if len(status.TaskOrder) > 0 {
		summary["task_order"] = status.TaskOrder
	}
	return summary
}

func normalizeWorkflowRuntimeStatus(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

func workflowRuntimeStatusErrorMessage(status *plugin.WorkflowExecutionStatus) string {
	if status == nil {
		return "未知错误"
	}
	parts := make([]string, 0, 3)
	if status.ErrorCode != "" {
		parts = append(parts, status.ErrorCode)
	}
	if status.Error != "" {
		parts = append(parts, status.Error)
	}
	if status.Details != "" {
		parts = append(parts, status.Details)
	}
	if len(parts) == 0 {
		return "未知错误"
	}
	return strings.Join(parts, ": ")
}

func workflowRuntimeValueString(value interface{}) string {
	if text, ok := value.(string); ok {
		return text
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func workflowRuntimeResultMap(values map[string]interface{}) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = workflowRuntimeValueString(value)
	}
	return result
}

// preprocessWorkflowParams 预处理工作流参数。
// 表/文件资源以 locator / target_parent_locator + target_name 作为存储契约。
// 执行前在 Develop 边界派生数据源 connection_info、schema/table 或 path。
// Spark Workflow 的 Spark 通用引擎资源绑定走请求顶层 runtime.engine_id，不与这里的数据源连接混用。
func (s *WorkflowEngineService) preprocessWorkflowParams(
	ctx context.Context,
	workflowEngineType string,
	workflowDef map[string]interface{},
) (map[string]interface{}, error) {
	result, _, err := s.preprocessWorkflowParamsWithTargets(ctx, workflowEngineType, workflowDef)
	return result, err
}

func (s *WorkflowEngineService) preprocessWorkflowParamsWithTargets(
	ctx context.Context,
	workflowEngineType string,
	workflowDef map[string]interface{},
) (map[string]interface{}, []WorkflowProducedTarget, error) {
	// 深拷贝 workflowDef，避免派生运行时参数时修改已保存的公开工作流定义。
	encodedWorkflow, err := json.Marshal(workflowDef)
	if err != nil {
		return nil, nil, fmt.Errorf("序列化工作流定义失败: %w", err)
	}
	result := make(map[string]interface{})
	if err := json.Unmarshal(encodedWorkflow, &result); err != nil {
		return nil, nil, fmt.Errorf("复制工作流定义失败: %w", err)
	}

	// 获取 tasks 数组
	tasksInterface, ok := result["tasks"]
	if !ok {
		return nil, nil, fmt.Errorf("工作流定义缺少 tasks 字段")
	}

	tasks, ok := tasksInterface.([]interface{})
	if !ok || len(tasks) == 0 {
		return nil, nil, fmt.Errorf("tasks 字段必须是非空数组")
	}

	producedTargets := make([]WorkflowProducedTarget, 0)
	// 遍历所有任务，预处理参数
	for i, taskInterface := range tasks {
		task, ok := taskInterface.(map[string]interface{})
		if !ok {
			return nil, nil, fmt.Errorf("任务 %d 必须是对象", i)
		}
		operatorID := strings.TrimSpace(stringParam(task, "operator"))
		if operatorID == "" {
			return nil, nil, fmt.Errorf("任务 %d 缺少 operator", i)
		}

		paramsInterface, ok := task["params"]
		if !ok {
			continue
		}

		params, ok := paramsInterface.(map[string]interface{})
		if !ok {
			return nil, nil, fmt.Errorf("任务 %d params 必须是对象", i)
		}
		adapterSpec, hasAdapterSpec := workflowOperatorAdapterSpecFor(workflowEngineType, operatorID)
		if err := rejectDirectWorkflowRuntimeParams(params, adapterSpec); err != nil {
			return nil, nil, fmt.Errorf("任务 %d 运行时参数校验失败: %w", i, err)
		}
		if !hasAdapterSpec {
			if err := rejectUndeclaredWorkflowResourceParams(workflowEngineType, operatorID, params); err != nil {
				return nil, nil, fmt.Errorf("任务 %d 资源参数校验失败: %w", i, err)
			}
			continue
		}
		targets, err := deriveWorkflowResourceParams(params, adapterSpec)
		if err != nil {
			return nil, nil, fmt.Errorf("任务 %d 资源参数派生失败: %w", i, err)
		}
		producedTargets = append(producedTargets, targets...)

		derivedResource, _ := params["__workflow_resource_derived"].(bool)

		// 检查是否有 engine_id 参数
		engineIDInterface, hasEngineID := params["engine_id"]
		if !hasEngineID {
			continue
		}
		if !derivedResource {
			return nil, nil, fmt.Errorf("任务 %d 不允许直接提交 engine_id，请使用 locator 或 target_parent_locator + target_name", i)
		}

		// 转换 engine_id 为 uint
		var engineID uint
		switch v := engineIDInterface.(type) {
		case float64:
			engineID = uint(v)
		case int:
			engineID = uint(v)
		case uint:
			engineID = v
		case string:
			// 支持字符串类型的 engine_id（如 "8"）
			id64, err := strconv.ParseUint(v, 10, 32)
			if err != nil {
				log.Printf("⚠️  任务 %d 的 engine_id 字符串转换失败: %v", i, err)
				continue
			}
			engineID = uint(id64)
		default:
			log.Printf("⚠️  任务 %d 的 engine_id 类型错误: %T", i, engineIDInterface)
			continue
		}

		// 从 System API 获取引擎信息（包含解密后的 connection_info）
		engine, err := s.systemClient.GetEngineByID(engineID)
		if err != nil {
			return nil, nil, fmt.Errorf("获取引擎 %d 信息失败: %w", engineID, err)
		}

		if err := normalizeDerivedWorkflowPath(params, engine.EngineType); err != nil {
			return nil, nil, fmt.Errorf("任务 %d 资源路径规范化失败: %w", i, err)
		}

		// 构建完整的 connection_info（包含 engine_type）
		// 注意：将 Engine.EngineType 添加到 connection_info 中，方便算子使用
		enrichedConnInfo := make(map[string]interface{})
		for k, v := range engine.ConnectionInfo {
			enrichedConnInfo[k] = v
		}
		enrichedConnInfo["engine_type"] = engine.EngineType

		// 替换 engine_id 为 connection_info
		delete(params, "engine_id")
		delete(params, "__workflow_resource_derived")
		delete(params, "__workflow_resource_kind")
		params["connection_info"] = enrichedConnInfo

		log.Printf("✅ 任务 %d: 已将 engine_id=%d 转换为 connection_info (engine_type=%s)",
			i, engineID, engine.EngineType)
	}

	return result, producedTargets, nil
}

func deriveWorkflowResourceParams(params map[string]interface{}, spec workflowOperatorAdapterSpec) ([]WorkflowProducedTarget, error) {
	producedTargets := make([]WorkflowProducedTarget, 0)
	for _, inputSpec := range spec.ResourceInputs {
		locator := stringParam(params, inputSpec.PublicParam)
		if locator == "" {
			continue
		}
		loc, err := resourcetree.ParseURI(locator)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", inputSpec.PublicParam, err)
		}
		if err := deriveWorkflowSourceParams(params, loc); err != nil {
			return nil, err
		}
		delete(params, inputSpec.PublicParam)
	}

	for _, outputSpec := range spec.ResourceOutputs {
		parentLocator := stringParam(params, outputSpec.ParentParam)
		if parentLocator == "" {
			continue
		}
		loc, err := resourcetree.ParseURI(parentLocator)
		if err != nil {
			return nil, fmt.Errorf("invalid %s: %w", outputSpec.ParentParam, err)
		}
		targetName := strings.TrimSpace(stringParam(params, outputSpec.NameParam))
		if targetName == "" {
			return nil, fmt.Errorf("%s is required when %s is provided", outputSpec.NameParam, outputSpec.ParentParam)
		}
		target, err := deriveWorkflowTargetParams(params, loc, targetName)
		if err != nil {
			return nil, err
		}
		producedTargets = append(producedTargets, target)
		delete(params, outputSpec.ParentParam)
		delete(params, outputSpec.NameParam)
	}

	return producedTargets, nil
}

func deriveWorkflowSourceParams(params map[string]interface{}, loc *resourcetree.ResourceLocator) error {
	switch loc.Type {
	case resourcetree.TypeTable, resourcetree.TypeCollection:
		schema, table := schemaTableFromPath(loc.Path)
		if table == "" {
			return fmt.Errorf("locator path must include table name")
		}
		params["engine_id"] = loc.EngineID
		params["schema"] = schema
		params["table"] = table
		params["__workflow_resource_derived"] = true
	case resourcetree.TypeFile:
		filePath := slashPath(loc.Path)
		if filePath == "" {
			return fmt.Errorf("file locator path must include file name")
		}
		params["engine_id"] = loc.EngineID
		params["path"] = filePath
		params["__workflow_resource_derived"] = true
		params["__workflow_resource_kind"] = "file"
	case resourcetree.TypeObject:
		objectPath, err := objectContentPath(loc.Path)
		if err != nil {
			return err
		}
		params["engine_id"] = loc.EngineID
		params["path"] = objectPath
		params["__workflow_resource_derived"] = true
		params["__workflow_resource_kind"] = "object"
	default:
		return fmt.Errorf("locator must point to table, collection, file or object, got %s", loc.Type)
	}
	return nil
}

func deriveWorkflowTargetParams(params map[string]interface{}, loc *resourcetree.ResourceLocator, targetName string) (WorkflowProducedTarget, error) {
	switch loc.Type {
	case resourcetree.TypeDatabase, resourcetree.TypeSchema:
		schema := lastPathSegment(loc.Path)
		if schema == "" {
			return WorkflowProducedTarget{}, fmt.Errorf("target_parent_locator path must include schema or database name")
		}
		params["engine_id"] = loc.EngineID
		params["schema"] = schema
		params["table"] = targetName
		params["__workflow_resource_derived"] = true
		return workflowProducedTarget(loc.EngineID, resourcetree.TypeTable, appendPath(loc.Path, targetName)), nil
	case resourcetree.TypeRoot:
		params["engine_id"] = loc.EngineID
		params["path"] = targetName
		params["__workflow_resource_derived"] = true
		params["__workflow_resource_kind"] = "file"
		return workflowProducedTarget(loc.EngineID, resourcetree.TypeFile, []string{targetName}), nil
	case resourcetree.TypeDirectory, resourcetree.TypeDir:
		parentPath := slashPath(loc.Path)
		if parentPath == "" {
			return WorkflowProducedTarget{}, fmt.Errorf("target_parent_locator path must include directory path")
		}
		params["engine_id"] = loc.EngineID
		params["path"] = joinResourcePath(parentPath, targetName)
		params["__workflow_resource_derived"] = true
		params["__workflow_resource_kind"] = "directory"
		return workflowProducedTarget(loc.EngineID, resourcetree.TypeFile, appendPath(loc.Path, targetName)), nil
	case resourcetree.TypeBucket, resourcetree.TypePrefix:
		objectPath, err := objectContentPath(appendPath(loc.Path, targetName))
		if err != nil {
			return WorkflowProducedTarget{}, err
		}
		params["engine_id"] = loc.EngineID
		params["path"] = objectPath
		params["__workflow_resource_derived"] = true
		params["__workflow_resource_kind"] = "object"
		return workflowProducedTarget(loc.EngineID, resourcetree.TypeObject, appendPath(loc.Path, targetName)), nil
	default:
		return WorkflowProducedTarget{}, fmt.Errorf("target_parent_locator must point to database, schema, root, directory, bucket or prefix, got %s", loc.Type)
	}
}

func workflowProducedTarget(engineID uint, resourceType resourcetree.ResourceType, resourcePath []string) WorkflowProducedTarget {
	locator := &resourcetree.ResourceLocator{
		EngineID: engineID,
		Type:     resourceType,
		Path:     resourcePath,
	}
	return WorkflowProducedTarget{
		EngineID: engineID,
		Type:     string(resourceType),
		Path:     resourcePath,
		Locator:  locator.ToURI(),
	}
}

func schemaTableFromPath(path []string) (string, string) {
	if len(path) == 0 {
		return "", ""
	}
	table := path[len(path)-1]
	if len(path) == 1 {
		return "", table
	}
	return path[len(path)-2], table
}

func lastPathSegment(path []string) string {
	if len(path) == 0 {
		return ""
	}
	return path[len(path)-1]
}

func slashPath(segments []string) string {
	cleaned := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment = strings.Trim(segment, "/ "); segment != "" {
			cleaned = append(cleaned, segment)
		}
	}
	return strings.Join(cleaned, "/")
}

func objectContentPath(segments []string) (string, error) {
	if len(segments) < 2 {
		return "", fmt.Errorf("object storage locator path must include bucket and object key")
	}
	bucket := strings.Trim(segments[0], "/ ")
	key := slashPath(segments[1:])
	if bucket == "" || key == "" {
		return "", fmt.Errorf("object storage locator path must include bucket and object key")
	}
	return bucket + "/" + key, nil
}

func joinResourcePath(parentPath, targetName string) string {
	return path.Join(parentPath, targetName)
}

func appendPath(segments []string, targetName string) []string {
	next := make([]string, 0, len(segments)+1)
	next = append(next, segments...)
	next = append(next, targetName)
	return next
}

func normalizeDerivedWorkflowPath(
	params map[string]interface{},
	engineType string,
) error {
	resourceKind := strings.TrimSpace(stringParam(params, "__workflow_resource_kind"))
	if resourceKind == "" {
		return nil
	}
	pathValue := stringParam(params, "path")
	if pathValue == "" {
		return fmt.Errorf("derived resource path is empty")
	}

	if resourceKind == "object" || (resourceKind == "directory" && isObjectStorageEngine(engineType)) {
		if strings.HasPrefix(pathValue, "s3://") || strings.HasPrefix(pathValue, "s3a://") {
			return nil
		}
		parts := strings.SplitN(pathValue, "/", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return fmt.Errorf("object storage path must include bucket and object key")
		}
		params["path"] = "s3a://" + parts[0] + "/" + parts[1]
		return nil
	}
	return nil
}

func isObjectStorageEngine(engineType string) bool {
	switch strings.ToLower(strings.TrimSpace(engineType)) {
	case "minio", "s3", "oss", "cos":
		return true
	default:
		return false
	}
}

func stringParam(params map[string]interface{}, key string) string {
	switch value := params[key].(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

// ListWorkflowEngines 获取支持 compute.workflow 能力的工作流引擎列表
// 用于工作流画布的引擎选择功能
func (s *WorkflowEngineService) ListWorkflowEngines(ctx context.Context, tenantID uint) ([]commonModels.Engine, error) {
	// 从System获取所有资源
	allEngines, err := s.systemClient.ListEngines("", tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch engines from system: %w", err)
	}

	// 过滤出支持 compute.workflow 能力的引擎
	workflowEngines := utils.FilterEnginesByComputeEntrypoint(allEngines, "workflow")

	log.Printf("✅ Develop: 获取工作流引擎列表成功 (tenant_id=%d, total=%d)", tenantID, len(workflowEngines))
	return workflowEngines, nil
}
