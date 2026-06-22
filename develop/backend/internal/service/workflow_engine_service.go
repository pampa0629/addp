package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/common/utils"
	"github.com/addp/develop/backend/internal/models"
)

// WorkflowEngineService 工作流执行引擎服务
// 支持动态发现的工作流引擎，通过引擎的 capabilities 配置调用相应的 API
type WorkflowEngineService struct {
	systemClient *commonClient.SystemClient
}

// NewWorkflowEngineService 创建工作流引擎服务
func NewWorkflowEngineService(systemClient *commonClient.SystemClient) *WorkflowEngineService {
	return &WorkflowEngineService{
		systemClient: systemClient,
	}
}

// WorkflowResponse 工作流执行响应
type WorkflowResponse struct {
	Status      string                   `json:"status"`
	ExecutionID string                   `json:"execution_id"`
	FinalResult string                   `json:"final_result,omitempty"` // GeoJSON 字符串
	AllResults  map[string]string        `json:"all_results,omitempty"`  // 所有中间结果
	Error       string                   `json:"error,omitempty"`
	Logs        []map[string]interface{} `json:"logs,omitempty"`      // 执行日志
	Traceback   string                   `json:"traceback,omitempty"` // 详细堆栈信息
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

	// 3. 预处理工作流参数：将 engine_id 转换为 connection_info（解耦工作流引擎和 System 服务）
	preprocessedWorkflowDef, err := s.preprocessWorkflowParams(ctx, workflowDef)
	if err != nil {
		return nil, fmt.Errorf("预处理工作流参数失败: %w", err)
	}

	// 4. 通过 Common Engine 的 WorkflowRuntimeProvider 执行工作流
	workflowReq := plugin.WorkflowExecuteRequest{
		WorkflowDef: preprocessedWorkflowDef,
		InputData:   inputData,
		Runtime:     workflowRuntimeOptions(config),
	}
	result, err := dbbridge.ExecuteWorkflow(ctx, engine, workflowReq)
	if err != nil {
		return nil, fmt.Errorf("调用工作流引擎 %s 失败: %w", engine.Name, err)
	}

	return toWorkflowResponse(result), nil
}

func workflowRuntimeOptions(config models.WorkflowExecutionConfig) map[string]interface{} {
	if len(config.EngineSpecific) == 0 {
		return nil
	}
	runtime := make(map[string]interface{})
	if sparkClusterID, ok := config.EngineSpecific["spark_cluster_id"]; ok {
		runtime["engine_id"] = sparkClusterID
	}
	if len(runtime) == 0 {
		return nil
	}
	return runtime
}

func toWorkflowResponse(result *plugin.WorkflowExecuteResult) *WorkflowResponse {
	if result == nil {
		return &WorkflowResponse{}
	}

	resp := &WorkflowResponse{
		Status:      result.Status,
		ExecutionID: result.ExecutionID,
		Error:       result.Error,
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

// preprocessWorkflowParams 预处理工作流参数。
// 表资源以 locator / parent_locator + name 作为存储契约，执行前在 Develop 边界派生 engine_id/schema/table，
// 再将 engine_id 转换为实际的 connection_info。
// 这样工作流引擎就不需要依赖 System 服务，实现解耦
func (s *WorkflowEngineService) preprocessWorkflowParams(
	ctx context.Context,
	workflowDef map[string]interface{},
) (map[string]interface{}, error) {
	// 深拷贝 workflowDef，避免修改原始数据
	result := make(map[string]interface{})
	for k, v := range workflowDef {
		result[k] = v
	}

	// 获取 tasks 数组
	tasksInterface, ok := result["tasks"]
	if !ok {
		// 如果没有 tasks 字段，可能是 Map 格式的工作流定义
		// Map 格式：{"step1": {...}, "step2": {...}}
		// 暂时不处理，直接返回原始定义
		log.Printf("⚠️  工作流定义中没有 tasks 字段，跳过参数预处理")
		return result, nil
	}

	tasks, ok := tasksInterface.([]interface{})
	if !ok {
		return nil, fmt.Errorf("tasks 字段格式错误，期望数组")
	}

	// 遍历所有任务，预处理参数
	for i, taskInterface := range tasks {
		task, ok := taskInterface.(map[string]interface{})
		if !ok {
			continue
		}

		paramsInterface, ok := task["params"]
		if !ok {
			continue
		}

		params, ok := paramsInterface.(map[string]interface{})
		if !ok {
			continue
		}

		if err := deriveWorkflowTableParams(params); err != nil {
			return nil, fmt.Errorf("任务 %d 表资源参数派生失败: %w", i, err)
		}

		// 检查是否有 engine_id 参数
		engineIDInterface, hasEngineID := params["engine_id"]
		if !hasEngineID {
			continue
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
			return nil, fmt.Errorf("获取引擎 %d 信息失败: %w", engineID, err)
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
		params["connection_info"] = enrichedConnInfo

		log.Printf("✅ 任务 %d: 已将 engine_id=%d 转换为 connection_info (engine_type=%s)",
			i, engineID, engine.EngineType)
	}

	return result, nil
}

func deriveWorkflowTableParams(params map[string]interface{}) error {
	if locator := stringParam(params, "locator"); locator != "" {
		loc, err := resourcetree.ParseURI(locator)
		if err != nil {
			return fmt.Errorf("invalid locator: %w", err)
		}
		if loc.Type != resourcetree.TypeTable && loc.Type != resourcetree.TypeCollection {
			return fmt.Errorf("locator must point to table or collection, got %s", loc.Type)
		}
		schema, table := schemaTableFromPath(loc.Path)
		if table == "" {
			return fmt.Errorf("locator path must include table name")
		}
		params["engine_id"] = loc.EngineID
		params["schema"] = schema
		params["table"] = table
		delete(params, "locator")
	}

	if parentLocator := stringParam(params, "target_parent_locator"); parentLocator != "" {
		loc, err := resourcetree.ParseURI(parentLocator)
		if err != nil {
			return fmt.Errorf("invalid target_parent_locator: %w", err)
		}
		targetName := strings.TrimSpace(stringParam(params, "target_name"))
		if targetName == "" {
			return fmt.Errorf("target_name is required when target_parent_locator is provided")
		}
		schema := lastPathSegment(loc.Path)
		if schema == "" {
			return fmt.Errorf("target_parent_locator path must include schema or database name")
		}
		params["engine_id"] = loc.EngineID
		params["schema"] = schema
		params["table"] = targetName
		delete(params, "target_parent_locator")
		delete(params, "target_name")
	}

	return nil
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
