package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// TransferClient Transfer 服务客户端
type TransferClient struct {
	baseURL     string
	httpClient  *http.Client
	internalKey string
}

// NewTransferClient 创建 Transfer 客户端（服务间调用）
func NewTransferClient(baseURL, internalKey string) *TransferClient {
	return &TransferClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		internalKey: internalKey,
	}
}

// addAuth 添加认证头
func (c *TransferClient) addAuth(req *http.Request) {
	if c.internalKey != "" {
		req.Header.Set("X-Internal-API-Key", c.internalKey)
	}
}

// addAuthWithTenant 添加认证头并附带租户 ID（服务间调用时需要传递租户上下文）
func (c *TransferClient) addAuthWithTenant(req *http.Request, tenantID uint) {
	c.addAuth(req)
	if tenantID > 0 {
		req.Header.Set("X-Tenant-ID", strconv.FormatUint(uint64(tenantID), 10))
	}
}

// CreateTransferTaskRequest 创建 Transfer 任务的请求（匹配 Transfer 模块的 CreateTaskRequest）
type CreateTransferTaskRequest struct {
	Name             string                 `json:"name"`
	Description      string                 `json:"description,omitempty"`
	TaskType         string                 `json:"task_type"` // Transfer 当前统一任务类型，固定为 sync
	Config           map[string]interface{} `json:"config"`    // 包含 reader/writer 配置
	BatchSize        int                    `json:"batch_size,omitempty"`
	AutoScanMetadata *bool                  `json:"auto_scan_metadata,omitempty"`
	TenantID         uint                   `json:"tenant_id,omitempty"`
}

// TransferTaskResponse Transfer 任务响应
type TransferTaskResponse struct {
	ID               uint   `json:"id"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	AutoScanMetadata bool   `json:"auto_scan_metadata"`
}

// TriggerTaskResponse 触发任务响应。
type TriggerTaskResponse struct {
	ID          uint   `json:"id"`           // 对应 TaskExecution.ID，仅用于内部关联
	ExecutionID string `json:"execution_id"` // 对应 common.task_executions.execution_id，前端和 Monitor 使用该 UUID
	Status      string `json:"status"`
}

// TransferExecutionResponse Transfer 执行记录响应。
type TransferExecutionResponse struct {
	ID              uint                   `json:"id"`
	ExecutionID     string                 `json:"execution_id"`
	TaskID          uint                   `json:"task_id"`
	Status          string                 `json:"status"`
	Progress        int                    `json:"progress"`
	ErrorMessage    string                 `json:"error_msg,omitempty"`
	ExecutionConfig map[string]interface{} `json:"execution_config,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// CreateTask 创建 Transfer 任务
func (c *TransferClient) CreateTask(req *CreateTransferTaskRequest) (*TransferTaskResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", c.baseURL+"/api/v1/transfer/task-definitions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.addAuthWithTenant(httpReq, req.TenantID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create task failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result TransferTaskResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if result.ID == 0 {
		return nil, fmt.Errorf("empty response data")
	}
	return &result, nil
}

// TriggerTask 触发任务执行
func (c *TransferClient) TriggerTask(taskID, tenantID uint) (*TriggerTaskResponse, error) {
	url := fmt.Sprintf("%s/api/v1/transfer/task-definitions/%d/start", c.baseURL, taskID)

	httpReq, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	c.addAuthWithTenant(httpReq, tenantID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("trigger task failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result TriggerTaskResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if result.ExecutionID == "" {
		return nil, fmt.Errorf("empty response data")
	}
	return &result, nil
}

// GetExecution 根据 Transfer execution UUID 查询执行详情。
func (c *TransferClient) GetExecution(executionID string, tenantID uint) (*TransferExecutionResponse, error) {
	url := fmt.Sprintf("%s/api/v1/transfer/executions/%s", c.baseURL, executionID)

	httpReq, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	c.addAuthWithTenant(httpReq, tenantID)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get execution failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var result TransferExecutionResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if result.ExecutionID == "" {
		return nil, fmt.Errorf("empty response data")
	}
	return &result, nil
}
