package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// SpatialWorkflowService 空间工作流服务
type SpatialWorkflowService struct {
	geopandasEngineURL string
	httpClient         *http.Client
}

// NewSpatialWorkflowService 创建空间工作流服务
func NewSpatialWorkflowService(geopandasEngineURL string) *SpatialWorkflowService {
	return &SpatialWorkflowService{
		geopandasEngineURL: geopandasEngineURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute, // 空间计算可能需要较长时间
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
	Status       string                 `json:"status"`
	ExecutionID  string                 `json:"execution_id"`
	FinalResult  string                 `json:"final_result,omitempty"`  // GeoJSON 字符串
	AllResults   map[string]string      `json:"all_results,omitempty"`   // 所有中间结果
	Error        string                 `json:"error,omitempty"`
}

// OperatorInfo 算子信息
type OperatorInfo struct {
	Params      map[string]string `json:"params"`
	Category    string            `json:"category"`
	Description string            `json:"description"`
}

// ExecuteWorkflow 执行工作流（即时执行）
func (s *SpatialWorkflowService) ExecuteWorkflow(ctx context.Context, workflowDef map[string]interface{}, inputData map[string]interface{}) (*WorkflowResponse, error) {
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
func (s *SpatialWorkflowService) ExecuteOperator(ctx context.Context, operatorName string, params map[string]interface{}) (*WorkflowResponse, error) {
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
func (s *SpatialWorkflowService) ListOperators(ctx context.Context) (map[string]OperatorInfo, error) {
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

	var response struct {
		Status    string                  `json:"status"`
		Operators map[string]OperatorInfo `json:"operators"`
		Count     int                     `json:"count"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list operators with status %d", resp.StatusCode)
	}

	return response.Operators, nil
}

// GetTaskStatus 查询执行状态
func (s *SpatialWorkflowService) GetTaskStatus(ctx context.Context, executionID string) (map[string]interface{}, error) {
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
