package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/utils"
	"github.com/google/uuid"
)

// WorkflowEngineService 工作流执行引擎服务
// 负责与 GeoPandas Engine 交互，执行工作流任务
type WorkflowEngineService struct {
	geopandasEngineURL string
	systemClient       *commonClient.SystemClient
	httpClient         *http.Client
}

// NewWorkflowEngineService 创建工作流引擎服务
func NewWorkflowEngineService(geopandasEngineURL string, systemClient *commonClient.SystemClient) *WorkflowEngineService {
	return &WorkflowEngineService{
		geopandasEngineURL: geopandasEngineURL,
		systemClient:       systemClient,
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

// ExecuteWorkflow 执行工作流（即时执行）
func (s *WorkflowEngineService) ExecuteWorkflow(ctx context.Context, workflowDef map[string]interface{}, inputData map[string]interface{}) (*WorkflowResponse, error) {
	// 构建请求
	req := WorkflowRequest{
		WorkflowDef: workflowDef,
		InputData:   inputData,
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 发送请求到 GeoPandas Engine
	url := fmt.Sprintf("%s/api/spatial/workflow", s.geopandasEngineURL)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// 执行请求
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute workflow: %w", err)
	}
	defer resp.Body.Close()

	// 解析响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var workflowResp WorkflowResponse
	if err := json.Unmarshal(body, &workflowResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return &workflowResp, fmt.Errorf("workflow execution failed with status %d: %s", resp.StatusCode, workflowResp.Error)
	}

	return &workflowResp, nil
}

// ExecuteOperator 执行单个算子
func (s *WorkflowEngineService) ExecuteOperator(ctx context.Context, operatorName string, params map[string]interface{}) (*WorkflowResponse, error) {
	// 构建请求
	reqBody, err := json.Marshal(map[string]interface{}{
		"params": params,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// 发送请求到 GeoPandas Engine
	url := fmt.Sprintf("%s/api/spatial/operators/%s/execute", s.geopandasEngineURL, operatorName)
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

// ListOperators 获取所有算子列表
func (s *WorkflowEngineService) ListOperators(ctx context.Context) ([]commonModels.OperatorMetadata, error) {
	// 发送请求到 GeoPandas Engine
	url := fmt.Sprintf("%s/api/spatial/operators", s.geopandasEngineURL)
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

// GetTaskStatus 查询执行状态
func (s *WorkflowEngineService) GetTaskStatus(ctx context.Context, executionID string) (map[string]interface{}, error) {
	// 发送请求到 GeoPandas Engine
	url := fmt.Sprintf("%s/api/spatial/executions/%s", s.geopandasEngineURL, executionID)
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
func (s *WorkflowEngineService) ListWorkflowEngines(ctx context.Context, tenantID uint) ([]commonModels.Resource, error) {
	// 从System获取所有资源
	allResources, err := s.systemClient.ListResources("", tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch resources from system: %w", err)
	}

	// 过滤出支持 workflow 开发模式的资源
	workflowEngines := utils.FilterResourcesByDevMode(allResources, "workflow")

	log.Printf("✅ Develop: 获取工作流引擎列表成功 (tenant_id=%d, total=%d)", tenantID, len(workflowEngines))
	return workflowEngines, nil
}
