package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
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

// Call 调用模块 API (支持从 gin.Context 或 context.Context 调用)
func (c *ModuleClient) Call(ctx interface{}, module, endpoint, method string, params map[string]interface{}) (interface{}, error) {
	baseURL := c.baseURLs[module]
	if baseURL == "" {
		return nil, fmt.Errorf("未配置模块 %s 的 URL", module)
	}

	targetURL := baseURL + endpoint
	var req *http.Request
	var err error
	var reqCtx context.Context
	var authHeader string

	// 根据 ctx 类型提取 context 和 auth header
	switch v := ctx.(type) {
	case *gin.Context:
		reqCtx = v.Request.Context()
		authHeader = v.GetHeader("Authorization")
	case context.Context:
		reqCtx = v
		authHeader = "" // 后台任务暂时没有认证,将来可以从配置获取系统 token
	default:
		return nil, fmt.Errorf("不支持的 context 类型")
	}

	// GET 请求使用 query params,其他方法使用 body
	if method == "GET" && params != nil {
		queryParams := url.Values{}
		for k, v := range params {
			queryParams.Add(k, fmt.Sprintf("%v", v))
		}
		targetURL += "?" + queryParams.Encode()
		req, err = http.NewRequestWithContext(reqCtx, method, targetURL, nil)
	} else {
		body, _ := json.Marshal(params)
		req, err = http.NewRequestWithContext(reqCtx, method, targetURL, bytes.NewReader(body))
	}

	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	// 转发 Authorization header (如果有)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

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
