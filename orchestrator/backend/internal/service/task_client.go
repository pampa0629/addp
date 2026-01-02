package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	commonModels "github.com/addp/common/models"
)

// TaskClient 通用任务客户端（基于动态 API 配置）
type TaskClient struct {
	httpClient *http.Client
}

// NewTaskClient 创建任务客户端
func NewTaskClient(timeout time.Duration) *TaskClient {
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &TaskClient{
		httpClient: &http.Client{Timeout: timeout},
	}
}

// CreateTask 创建任务
// params 包含模板变量,如: {"TenantID": 1, "ResourceID": 5, "ScanConfig": {...}}
func (c *TaskClient) CreateTask(ctx context.Context, engine *commonModels.Engine, params map[string]interface{}) (string, error) {
	// 从 connection_info 构建 base_url
	baseURL, err := commonModels.BuildBaseURL(engine.ConnectionInfo)
	if err != nil {
		return "", fmt.Errorf("failed to build base_url: %w", err)
	}

	// 获取标准配置
	standard, ok := commonModels.WorkflowStandards[engine.EngineType]
	if !ok {
		return "", fmt.Errorf("no standard config for engine_type: %s", engine.EngineType)
	}

	// 获取 execute endpoint 配置（工作流引擎使用 execute 端点）
	executeEndpoint := standard.Endpoints["execute"]
	targetURL := baseURL + executeEndpoint.Path

	// 构建请求 body（直接使用 params）
	bodyJSON, err := json.Marshal(params)
	if err != nil {
		return "", fmt.Errorf("failed to marshal params: %w", err)
	}

	// 发送 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, executeEndpoint.Method, targetURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// 设置超时
	timeout := executeEndpoint.Timeout
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	resp, err := c.httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	// 提取任务 ID（优先级: id > task_id > run_id）
	if id, ok := result["id"]; ok {
		return fmt.Sprintf("%v", id), nil
	}
	if taskID, ok := result["task_id"]; ok {
		return fmt.Sprintf("%v", taskID), nil
	}
	if runID, ok := result["run_id"]; ok {
		return fmt.Sprintf("%v", runID), nil
	}

	return "", fmt.Errorf("no task ID found in response")
}

// ExecuteTask 执行任务（已废弃，工作流引擎使用 CreateTask 直接执行）
// 保留此方法以兼容旧代码，实际上调用 CreateTask
func (c *TaskClient) ExecuteTask(ctx context.Context, engine *commonModels.Engine, taskID string, params map[string]interface{}) (string, error) {
	// 工作流引擎的执行已合并到 CreateTask 中
	return c.CreateTask(ctx, engine, params)
}

// GetTaskStatus 获取任务状态
func (c *TaskClient) GetTaskStatus(ctx context.Context, engine *commonModels.Engine, taskID string) (*TaskStatus, error) {
	// 从 connection_info 构建 base_url
	baseURL, err := commonModels.BuildBaseURL(engine.ConnectionInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to build base_url: %w", err)
	}

	// 获取标准配置
	standard, ok := commonModels.WorkflowStandards[engine.EngineType]
	if !ok {
		return nil, fmt.Errorf("no standard config for engine_type: %s", engine.EngineType)
	}

	// 获取 status endpoint 配置
	statusEndpoint := standard.Endpoints["status"]

	// 替换路径中的 {id} 占位符
	path := strings.Replace(statusEndpoint.Path, "{id}", taskID, -1)
	targetURL := baseURL + path

	// 发送 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, statusEndpoint.Method, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// 设置超时
	timeout := statusEndpoint.Timeout
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	resp, err := c.httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// 解析响应
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// 提取字段（使用默认字段名）
	status := ""
	if v, ok := result["status"]; ok {
		status = fmt.Sprintf("%v", v)
	}

	message := ""
	if v, ok := result["message"]; ok {
		message = fmt.Sprintf("%v", v)
	}

	progress := 0
	if v, ok := result["progress"]; ok {
		fmt.Sscanf(fmt.Sprintf("%v", v), "%d", &progress)
	}

	return &TaskStatus{
		TaskID:   taskID,
		Status:   status,
		Progress: progress,
		Message:  message,
		Raw:      result,
	}, nil
}

// TaskStatus 任务状态
type TaskStatus struct {
	TaskID   string
	Status   string // created, pending, running, completed, failed, cancelled
	Progress int    // 0-100
	Message  string
	Raw      map[string]interface{} // 原始响应
}
