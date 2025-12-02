package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ModuleClient HTTP 客户端用于调用其他模块
type ModuleClient struct {
	baseURLs   map[string]string
	httpClient *http.Client
}

// NewModuleClient 创建模块客户端
func NewModuleClient(baseURLs map[string]string) *ModuleClient {
	return &ModuleClient{
		baseURLs:   baseURLs,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Call 调用模块 API
func (c *ModuleClient) Call(ctx context.Context, module, endpoint, method string, params map[string]interface{}) (interface{}, error) {
	baseURL := c.baseURLs[module]
	if baseURL == "" {
		return nil, fmt.Errorf("未配置模块 %s 的 URL", module)
	}

	url := baseURL + endpoint
	body, _ := json.Marshal(params)
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	// 提取任务 ID
	if taskID, ok := result["task_id"]; ok {
		return taskID, nil
	}
	if runID, ok := result["run_id"]; ok {
		return runID, nil
	}
	if id, ok := result["id"]; ok {
		return id, nil
	}

	return result, nil
}

// GetTaskStatus 获取任务状态
func (c *ModuleClient) GetTaskStatus(ctx context.Context, module string, taskID interface{}) (string, map[string]interface{}, error) {
	baseURL := c.baseURLs[module]
	if baseURL == "" {
		return "", nil, fmt.Errorf("未配置模块 %s 的 URL", module)
	}

	var endpoint string
	switch module {
	case "transfer":
		endpoint = fmt.Sprintf("/api/executions/%v", taskID)
	case "meta":
		endpoint = fmt.Sprintf("/api/scan/runs/%v", taskID)
	case "manager":
		endpoint = fmt.Sprintf("/api/cache/tasks/%v", taskID)
	default:
		return "", nil, fmt.Errorf("未知模块: %s", module)
	}

	url := baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", nil, err
	}

	status, _ := result["status"].(string)
	return status, result, nil
}
