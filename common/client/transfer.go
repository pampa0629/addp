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
	TaskType         string                 `json:"task_type"` // Transfer 当前统一任务类型，固定为 import
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

// TriggerTaskResponse 触发任务响应
// Transfer StartTask 接口直接返回 TaskExecution 对象，其中 execution ID 字段为 "id"
type TriggerTaskResponse struct {
	ExecutionID uint   `json:"id"` // 对应 TaskExecution.ID
	Status      string `json:"status"`
	Message     string `json:"message"`
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

	var result struct {
		Data  *TransferTaskResponse `json:"data"`
		Error string                `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("create task error: %s", result.Error)
	}
	if result.Data != nil {
		return result.Data, nil
	}

	// Transfer API 直接返回任务对象（不包 data 字段），尝试直接解析
	var directResult TransferTaskResponse
	if err := json.Unmarshal(respBody, &directResult); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if directResult.ID == 0 {
		return nil, fmt.Errorf("empty response data")
	}
	return &directResult, nil
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

	var result struct {
		Data  *TriggerTaskResponse `json:"data"`
		Error string               `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		// 有些响应不包含 data 包装，直接解析
		var directResult TriggerTaskResponse
		if err2 := json.Unmarshal(respBody, &directResult); err2 != nil {
			return nil, fmt.Errorf("failed to parse response: %w", err)
		}
		return &directResult, nil
	}
	if result.Error != "" {
		return nil, fmt.Errorf("trigger task error: %s", result.Error)
	}
	if result.Data != nil {
		return result.Data, nil
	}

	// Transfer StartTask 直接返回 TaskExecution 对象（无 data 包装），尝试直接解析
	var directResult TriggerTaskResponse
	if err2 := json.Unmarshal(respBody, &directResult); err2 == nil && directResult.ExecutionID != 0 {
		return &directResult, nil
	}

	// 返回空响应也认为成功（极端兜底，ExecutionID 为 0 时调用方需容错）
	return &TriggerTaskResponse{Status: "pending"}, nil
}
