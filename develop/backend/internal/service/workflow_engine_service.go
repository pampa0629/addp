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
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/utils"
	"github.com/addp/develop/backend/internal/models"
	"github.com/google/uuid"
)

// WorkflowEngineService 工作流执行引擎服务
// 支持多种工作流引擎（Python Workflow、Spark Workflow）
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
	Status      string                 `json:"status"`
	ExecutionID string                 `json:"execution_id"`
	FinalResult string                 `json:"final_result,omitempty"` // GeoJSON 字符串
	AllResults  map[string]string      `json:"all_results,omitempty"`  // 所有中间结果
	Error       string                 `json:"error,omitempty"`
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

	// 3. 根据引擎类型分发执行
	switch config.EngineType {
	case "api.python-workflow":
		return s.executeViaPythonWorkflow(ctx, engine, workflowDef, inputData)

	case "api.spark_workflow":
		// 从 engine_specific 提取 Spark 集群 ID
		sparkClusterID, ok := config.EngineSpecific["spark_cluster_id"].(float64)
		if !ok {
			return nil, errors.New("Spark 工作流引擎缺少 spark_cluster_id 配置")
		}
		return s.executeViaSparkWorkflow(ctx, engine, uint(sparkClusterID), workflowDef, inputData)

	default:
		return nil, fmt.Errorf("不支持的工作流引擎: %s", config.EngineType)
	}
}

// executeViaPythonWorkflow 通过 Python Workflow 引擎执行（自带运行时）
func (s *WorkflowEngineService) executeViaPythonWorkflow(
	ctx context.Context,
	engine *commonModels.Engine,
	workflowDef map[string]interface{},
	inputData map[string]interface{},
) (*WorkflowResponse, error) {
	// 从引擎 ConnectionInfo 提取 API URL
	apiURL, ok := engine.ConnectionInfo["api_url"].(string)
	if !ok || apiURL == "" {
		return nil, errors.New("Python Workflow 引擎缺少 api_url 配置")
	}

	url := fmt.Sprintf("%s/api/spatial/workflow", apiURL)

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

	log.Printf("🚀 Develop: 调用 Python Workflow 引擎 (url=%s)", url)

	// 执行请求
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("调用 Python Workflow 引擎失败: %w", err)
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

	log.Printf("✅ Develop: Python Workflow 引擎执行成功 (execution_id=%s)", workflowResp.ExecutionID)
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
	workflowAPIURL, ok := engine.ConnectionInfo["api_url"].(string)
	if !ok || workflowAPIURL == "" {
		return nil, errors.New("Spark 工作流引擎缺少 api_url 配置")
	}

	url := fmt.Sprintf("%s/api/spatial/workflow/execute", workflowAPIURL)

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
	url := fmt.Sprintf("%s/api/spatial/operators/%s/execute", pythonWorkflowEngineURL, operatorName)
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
	url := fmt.Sprintf("%s/api/spatial/operators", pythonWorkflowEngineURL)
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
	url := fmt.Sprintf("%s/api/spatial/executions/%s", pythonWorkflowEngineURL, executionID)
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
