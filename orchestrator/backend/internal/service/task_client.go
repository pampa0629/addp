package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/template"
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
func (c *TaskClient) CreateTask(ctx context.Context, engine *commonModels.Resource, params map[string]interface{}) (string, error) {
	// 解析 TaskAPIConfig
	var apiConfig commonModels.TaskAPIConfig
	if err := unmarshalTaskAPIConfig(engine.TaskAPIConfig, &apiConfig); err != nil {
		return "", fmt.Errorf("failed to parse task_api_config: %w", err)
	}

	// 获取 create endpoint 配置
	createEndpoint, ok := apiConfig.Endpoints["create"]
	if !ok {
		return "", fmt.Errorf("no 'create' endpoint defined in task_api_config")
	}

	// 构建请求 URL
	targetURL := apiConfig.BaseURL + createEndpoint.Path

	// 渲染请求 body 模板
	bodyJSON, err := renderTemplate(createEndpoint.BodyTemplate, params)
	if err != nil {
		return "", fmt.Errorf("failed to render body template: %w", err)
	}

	// 发送 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, createEndpoint.Method, targetURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// 设置超时
	timeout := 30
	if t, ok := apiConfig.Timeout["create"]; ok {
		timeout = t
	}
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

// ExecuteTask 执行任务
func (c *TaskClient) ExecuteTask(ctx context.Context, engine *commonModels.Resource, taskID string, params map[string]interface{}) (string, error) {
	// 解析 TaskAPIConfig
	var apiConfig commonModels.TaskAPIConfig
	if err := unmarshalTaskAPIConfig(engine.TaskAPIConfig, &apiConfig); err != nil {
		return "", fmt.Errorf("failed to parse task_api_config: %w", err)
	}

	// 获取 execute endpoint 配置
	executeEndpoint, ok := apiConfig.Endpoints["execute"]
	if !ok {
		return "", fmt.Errorf("no 'execute' endpoint defined in task_api_config")
	}

	// 渲染路径模板（替换 {{.TaskID}}）
	pathParams := map[string]interface{}{
		"TaskID": taskID,
	}
	for k, v := range params {
		pathParams[k] = v
	}

	path, err := renderStringTemplate(executeEndpoint.Path, pathParams)
	if err != nil {
		return "", fmt.Errorf("failed to render path template: %w", err)
	}

	targetURL := apiConfig.BaseURL + path

	// 渲染请求 body 模板（如果有）
	var bodyReader io.Reader
	if executeEndpoint.BodyTemplate != nil {
		bodyJSON, err := renderTemplate(executeEndpoint.BodyTemplate, params)
		if err != nil {
			return "", fmt.Errorf("failed to render body template: %w", err)
		}
		bodyReader = bytes.NewReader(bodyJSON)
	}

	// 发送 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, executeEndpoint.Method, targetURL, bodyReader)
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// 设置超时
	timeout := 300
	if t, ok := apiConfig.Timeout["execute"]; ok {
		timeout = t
	}
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

	// 提取执行 ID（优先级: execution_id > run_id > id）
	if execID, ok := result["execution_id"]; ok {
		return fmt.Sprintf("%v", execID), nil
	}
	if runID, ok := result["run_id"]; ok {
		return fmt.Sprintf("%v", runID), nil
	}
	if id, ok := result["id"]; ok {
		return fmt.Sprintf("%v", id), nil
	}

	// 如果没有返回 ID,返回任务 ID
	return taskID, nil
}

// GetTaskStatus 获取任务状态
func (c *TaskClient) GetTaskStatus(ctx context.Context, engine *commonModels.Resource, taskID string) (*TaskStatus, error) {
	// 解析 TaskAPIConfig
	var apiConfig commonModels.TaskAPIConfig
	if err := unmarshalTaskAPIConfig(engine.TaskAPIConfig, &apiConfig); err != nil {
		return nil, fmt.Errorf("failed to parse task_api_config: %w", err)
	}

	// 获取 status endpoint 配置
	statusEndpoint, ok := apiConfig.Endpoints["status"]
	if !ok {
		return nil, fmt.Errorf("no 'status' endpoint defined in task_api_config")
	}

	// 渲染路径模板（替换 {{.TaskID}} 或 {{.RunID}}）
	pathParams := map[string]interface{}{
		"TaskID": taskID,
		"RunID":  taskID, // 兼容两种命名
	}

	path, err := renderStringTemplate(statusEndpoint.Path, pathParams)
	if err != nil {
		return nil, fmt.Errorf("failed to render path template: %w", err)
	}

	targetURL := apiConfig.BaseURL + path

	// 添加 query params（如果有）
	if len(statusEndpoint.QueryParams) > 0 {
		queryVals := url.Values{}
		for key, valueTpl := range statusEndpoint.QueryParams {
			value, err := renderStringTemplate(valueTpl, pathParams)
			if err != nil {
				return nil, fmt.Errorf("failed to render query param %s: %w", key, err)
			}
			queryVals.Set(key, value)
		}
		targetURL += "?" + queryVals.Encode()
	}

	// 发送 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, statusEndpoint.Method, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// 设置超时
	timeout := 10
	if t, ok := apiConfig.Timeout["status"]; ok {
		timeout = t
	}
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

	// 使用 ResponseMapping 提取字段
	status := extractField(result, statusEndpoint.ResponseMapping, "status")
	message := extractField(result, statusEndpoint.ResponseMapping, "message")
	progressStr := extractField(result, statusEndpoint.ResponseMapping, "progress")

	// 解析 progress
	progress := 0
	if progressStr != "" {
		fmt.Sscanf(progressStr, "%d", &progress)
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

// unmarshalTaskAPIConfig 辅助函数：解析 TaskAPIConfig JSON 字符串
func unmarshalTaskAPIConfig(configJSON *string, result *commonModels.TaskAPIConfig) error {
	if configJSON == nil {
		return fmt.Errorf("task_api_config is nil")
	}

	if err := json.Unmarshal([]byte(*configJSON), result); err != nil {
		return fmt.Errorf("failed to unmarshal task_api_config: %w", err)
	}

	return nil
}

// renderTemplate 渲染 JSON 模板（支持 Go template 语法）
func renderTemplate(tpl map[string]interface{}, params map[string]interface{}) ([]byte, error) {
	// 先将模板转换为 JSON 字符串
	tplJSON, err := json.Marshal(tpl)
	if err != nil {
		return nil, err
	}

	// 使用 Go template 渲染
	tmpl, err := template.New("body").Parse(string(tplJSON))
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, params); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// renderStringTemplate 渲染字符串模板
func renderStringTemplate(tpl string, params map[string]interface{}) (string, error) {
	tmpl, err := template.New("string").Parse(tpl)
	if err != nil {
		return "", err
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, params); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// extractField 从响应中提取字段（支持 ResponseMapping）
func extractField(result map[string]interface{}, mapping *commonModels.ResponseMapping, fieldType string) string {
	if mapping == nil {
		// 使用默认字段名
		switch fieldType {
		case "status":
			if v, ok := result["status"]; ok {
				return fmt.Sprintf("%v", v)
			}
		case "message":
			if v, ok := result["message"]; ok {
				return fmt.Sprintf("%v", v)
			}
			if v, ok := result["error_message"]; ok {
				return fmt.Sprintf("%v", v)
			}
		case "progress":
			if v, ok := result["progress"]; ok {
				return fmt.Sprintf("%v", v)
			}
			if v, ok := result["progress_percent"]; ok {
				return fmt.Sprintf("%v", v)
			}
		}
		return ""
	}

	// 使用自定义映射
	var fieldName string
	switch fieldType {
	case "status":
		fieldName = mapping.StatusField
	case "message":
		fieldName = mapping.MessageField
	case "progress":
		fieldName = mapping.ProgressField
	}

	if fieldName == "" {
		return ""
	}

	if v, ok := result[fieldName]; ok {
		return fmt.Sprintf("%v", v)
	}

	return ""
}
