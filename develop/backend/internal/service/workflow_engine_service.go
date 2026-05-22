package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/utils"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
)

// WorkflowEngineService 工作流执行引擎服务
// 支持动态发现的工作流引擎，通过引擎的 capabilities 配置调用相应的 API
type WorkflowEngineService struct {
	systemClient *commonClient.SystemClient
	httpClient   *http.Client
}

// NewWorkflowEngineService 创建工作流引擎服务
func NewWorkflowEngineService(systemClient *commonClient.SystemClient) *WorkflowEngineService {
	return &WorkflowEngineService{
		systemClient: systemClient,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute, // 工作流计算可能需要较长时间
		},
	}
}

// WorkflowRequest 工作流执行请求
type WorkflowRequest struct {
	WorkflowDef map[string]interface{} `json:"workflow_def"`
	InputData   map[string]interface{} `json:"input_data,omitempty"`
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

// getAPIEndpoint 返回 ADDP 工作流运行时标准 API 端点
func getAPIEndpoint(engine *commonModels.Engine, endpointType string) string {
	return getDefaultEndpoint(endpointType)
}

// getDefaultEndpoint 返回默认的 API 端点（向后兼容）
func getDefaultEndpoint(endpointType string) string {
	switch endpointType {
	case "operators":
		return "/api/operators"
	case "execute":
		return "/api/operators/:name/execute"
	case "workflow":
		return "/api/workflow"
	case "executions":
		return "/api/executions/:id"
	default:
		return ""
	}
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

// preprocessWorkflowParams 预处理工作流参数
// 将算子参数中的 engine_id 转换为实际的 connection_info
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

// executeViaGenericWorkflow 通过通用工作流引擎执行（自带运行时）
// 支持 python_workflow、math_workflow 等所有自带运行时的工作流引擎
func (s *WorkflowEngineService) executeViaGenericWorkflow(
	ctx context.Context,
	engine *commonModels.Engine,
	workflowDef map[string]interface{},
	inputData map[string]interface{},
) (*WorkflowResponse, error) {
	// 从引擎 ConnectionInfo 提取 API URL
	// 优先使用 api_url 字段，如果没有则从 host/port/protocol 构造
	apiURL, ok := engine.ConnectionInfo["api_url"].(string)
	if !ok || apiURL == "" {
		// 尝试从 host、port、protocol 构造 API URL
		host, hostOK := engine.ConnectionInfo["host"].(string)
		portFloat, portOK := engine.ConnectionInfo["port"].(float64) // JSON 数字默认为 float64
		protocol, protocolOK := engine.ConnectionInfo["protocol"].(string)

		if !hostOK || !portOK || !protocolOK {
			return nil, fmt.Errorf("工作流引擎 %s 缺少 api_url 或 host/port/protocol 配置", engine.Name)
		}

		port := int(portFloat)
		apiURL = fmt.Sprintf("%s://%s:%d", protocol, host, port)
		log.Printf("📍 [WorkflowEngine] 从 connection_info 构造 API URL: %s", apiURL)
	}

	// 动态获取 workflow API 端点
	endpoint := getAPIEndpoint(engine, "workflow")
	url := fmt.Sprintf("%s%s", apiURL, endpoint)

	// 构造请求体
	req := WorkflowRequest{
		WorkflowDef: workflowDef,
		InputData:   inputData,
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 发送 HTTP POST 请求
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	log.Printf("🚀 Develop: 调用通用工作流引擎 %s (url=%s)", engine.Name, url)

	// 执行请求
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("调用工作流引擎 %s 失败: %w", engine.Name, err)
	}
	defer resp.Body.Close()

	// 解析响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var workflowResp WorkflowResponse
	if err := json.Unmarshal(body, &workflowResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &workflowResp, fmt.Errorf("工作流执行失败 (status=%d): %s", resp.StatusCode, workflowResp.Error)
	}

	log.Printf("✅ Develop: 工作流引擎 %s 执行成功 (execution_id=%s)", engine.Name, workflowResp.ExecutionID)
	return &workflowResp, nil
}

// executeViaSparkWorkflow 通过 Spark 工作流引擎执行（PySpark 脚本生成）
func (s *WorkflowEngineService) executeViaSparkWorkflow(
	ctx context.Context,
	engine *commonModels.Engine,
	sparkClusterID uint,
	workflowDef map[string]interface{},
	inputData map[string]interface{},
) (*WorkflowResponse, error) {
	// 1. 查询 Spark 运行时详情
	sparkRuntime, err := s.systemClient.GetEngineByID(sparkClusterID)
	if err != nil {
		return nil, fmt.Errorf("查询 Spark 运行时失败: %w", err)
	}

	// 2. 从 Spark 工作流引擎 ConnectionInfo 提取 API URL
	// 优先使用 api_url 字段，如果没有则从 host/port/protocol 构造
	workflowAPIURL, ok := engine.ConnectionInfo["api_url"].(string)
	if !ok || workflowAPIURL == "" {
		// 尝试从 host、port、protocol 构造 API URL
		host, hostOK := engine.ConnectionInfo["host"].(string)
		portFloat, portOK := engine.ConnectionInfo["port"].(float64)
		protocol, protocolOK := engine.ConnectionInfo["protocol"].(string)

		if !hostOK || !portOK || !protocolOK {
			return nil, errors.New("Spark 工作流引擎缺少 api_url 或 host/port/protocol 配置")
		}

		port := int(portFloat)
		workflowAPIURL = fmt.Sprintf("%s://%s:%d", protocol, host, port)
		log.Printf("📍 [WorkflowEngine] 从 connection_info 构造 Spark 工作流引擎 API URL: %s", workflowAPIURL)
	}

	// 动态获取 workflow API 端点
	endpoint := getAPIEndpoint(engine, "workflow")
	url := fmt.Sprintf("%s%s", workflowAPIURL, endpoint)

	// 3. 构造请求体（包含目标 Spark 集群信息）
	requestBody := map[string]interface{}{
		"workflow_def": workflowDef,
		"input_data":   inputData,
		"target_runtime": map[string]interface{}{
			"type": "spark",
			// Spark Master 地址（用于 spark-submit 或 Livy）
			"spark_master": sparkRuntime.ConnectionInfo["spark_master"],
			"deploy_mode":  sparkRuntime.ConnectionInfo["deploy_mode"], // cluster/client
			// 可选：Livy REST API URL（推荐，异步提交）
			"livy_url": sparkRuntime.ConnectionInfo["livy_url"],
			// 认证信息
			"username": sparkRuntime.ConnectionInfo["username"],
			"password": sparkRuntime.ConnectionInfo["password"],
		},
	}

	reqBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 4. 调用 Spark 工作流引擎 API
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	log.Printf("🚀 Develop: 调用 Spark 工作流引擎 (url=%s, spark_cluster_id=%d)", url, sparkClusterID)

	// 执行请求
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("调用 Spark 工作流引擎失败: %w", err)
	}
	defer resp.Body.Close()

	// 解析响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var workflowResp WorkflowResponse
	if err := json.Unmarshal(body, &workflowResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &workflowResp, fmt.Errorf("工作流执行失败 (status=%d): %s", resp.StatusCode, workflowResp.Error)
	}

	log.Printf("✅ Develop: Spark 工作流引擎执行成功 (execution_id=%s)", workflowResp.ExecutionID)
	return &workflowResp, nil
}

// ExecuteOperator 执行单个算子（向后兼容，使用 Python Workflow 引擎）
func (s *WorkflowEngineService) ExecuteOperator(ctx context.Context, pythonWorkflowEngineURL string, operatorName string, params map[string]interface{}) (*WorkflowResponse, error) {
	// 构建请求
	reqBody, err := json.Marshal(map[string]interface{}{
		"params": params,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 发送请求到 Python Workflow Engine
	// 注意：此函数为向后兼容接口，使用默认端点路径
	url := fmt.Sprintf("%s/api/operators/%s/execute", pythonWorkflowEngineURL, operatorName)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// 执行请求
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute operator: %w", err)
	}
	defer resp.Body.Close()

	// 解析响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var operatorResp struct {
		Status string `json:"status"`
		Result string `json:"result,omitempty"` // GeoJSON 字符串
		Error  string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(body, &operatorResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("operator execution failed with status %d: %s", resp.StatusCode, operatorResp.Error)
	}

	// 转换为统一的响应格式
	return &WorkflowResponse{
		Status:      operatorResp.Status,
		ExecutionID: uuid.New().String(),
		FinalResult: operatorResp.Result,
	}, nil
}

// ListOperators 获取所有算子列表（向后兼容）
func (s *WorkflowEngineService) ListOperators(ctx context.Context, pythonWorkflowEngineURL string) ([]commonModels.OperatorMetadata, error) {
	// 发送请求到 Python Workflow Engine
	// 注意：此函数为向后兼容接口，使用默认端点路径
	url := fmt.Sprintf("%s/api/operators", pythonWorkflowEngineURL)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 执行请求
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to list operators: %w", err)
	}
	defer resp.Body.Close()

	// 解析响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response commonModels.OperatorsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list operators with status %d", resp.StatusCode)
	}

	log.Printf("✅ Develop: 获取工作流算子列表成功 (count=%d)", len(response.Operators))
	return response.Operators, nil
}

// GetTaskStatus 查询执行状态（向后兼容）
func (s *WorkflowEngineService) GetTaskStatus(ctx context.Context, pythonWorkflowEngineURL string, executionID string) (map[string]interface{}, error) {
	// 发送请求到 Python Workflow Engine
	// 注意：此函数为向后兼容接口，使用默认端点路径
	url := fmt.Sprintf("%s/api/executions/%s", pythonWorkflowEngineURL, executionID)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 执行请求
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get task status: %w", err)
	}
	defer resp.Body.Close()

	// 解析响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return response, fmt.Errorf("failed to get task status with status %d", resp.StatusCode)
	}

	return response, nil
}

// ListWorkflowEngines 获取支持workflow开发模式的工作流引擎列表
// 用于工作流画布的引擎选择功能
func (s *WorkflowEngineService) ListWorkflowEngines(ctx context.Context, tenantID uint) ([]commonModels.Engine, error) {
	// 从System获取所有资源
	allEngines, err := s.systemClient.ListEngines("", tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch engines from system: %w", err)
	}

	// 过滤出支持 workflow 开发模式的引擎
	workflowEngines := utils.FilterEnginesByDevMode(allEngines, "workflow")

	log.Printf("✅ Develop: 获取工作流引擎列表成功 (tenant_id=%d, total=%d)", tenantID, len(workflowEngines))
	return workflowEngines, nil
}
