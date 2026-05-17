package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// TaskProviderRegistryService 任务提供者注册服务
// 将 Transfer 模块注册到 System 的 task_providers 表
type TaskProviderRegistryService struct {
	systemURL      string
	internalAPIKey string
	transferURL    string
}

// NewTaskProviderRegistryService 创建任务提供者注册服务
func NewTaskProviderRegistryService(systemURL, internalAPIKey, transferURL string) *TaskProviderRegistryService {
	return &TaskProviderRegistryService{
		systemURL:      systemURL,
		internalAPIKey: internalAPIKey,
		transferURL:    transferURL,
	}
}

// TaskProviderRegistration 任务提供者注册请求
type TaskProviderRegistration struct {
	ModuleName          string  `json:"module_name"`
	DisplayName         string  `json:"display_name"`
	Description         string  `json:"description"`
	BaseURL             string  `json:"base_url"`
	TaskListEndpoint    string  `json:"task_list_endpoint"`
	TaskDetailEndpoint  string  `json:"task_detail_endpoint"`
	TaskExecuteEndpoint string  `json:"task_execute_endpoint"`
	TaskStatusEndpoint  string  `json:"task_status_endpoint"`
	TaskCancelEndpoint  string  `json:"task_cancel_endpoint,omitempty"`
	Capabilities        *string `json:"capabilities,omitempty"`
	IsEnabled           bool    `json:"is_enabled"`
}

// Register 注册 Transfer 模块为任务提供者
func (s *TaskProviderRegistryService) Register() error {
	// 构造能力描述（含前端集成 URL）
	capabilities := map[string]interface{}{
		"task_types": []map[string]string{
			{
				"type":         "import",
				"display_name": "数据导入",
				"description":  "从外部数据源导入数据",
			},
			{
				"type":         "export",
				"display_name": "数据导出",
				"description":  "导出数据到外部目标",
			},
			{
				"type":         "sync",
				"display_name": "数据同步",
				"description":  "双向数据同步",
			},
		},
		"execution_modes":   []string{"batch", "stream", "micro-batch"},
		"supported_sources": []string{"postgresql", "mysql", "doris", "minio", "s3", "csv", "json", "parquet"},
		"features":          []string{"async", "checkpoint", "retry", "field_mapping", "scheduled"},
		"create_task_url":   "http://localhost:5176/#/tasks/new",
		"edit_task_url":     "http://localhost:5176/#/tasks/:id",
	}

	// 序列化为 JSON 字符串
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := string(capabilitiesJSON)

	// 构造注册请求（注册到 task_providers 表）
	registration := TaskProviderRegistration{
		ModuleName:  "transfer",
		DisplayName: "数据传输",
		Description: "数据导入、导出、同步任务",

		// API 端点配置
		BaseURL:             s.transferURL,
		TaskListEndpoint:    "/api/v1/transfer/tasks",           // 传输任务列表
		TaskDetailEndpoint:  "/api/v1/transfer/tasks/:id",       // 传输任务详情
		TaskExecuteEndpoint: "/api/v1/transfer/tasks/:id/start", // 启动传输任务
		TaskStatusEndpoint:  "/api/v1/transfer/executions/:id",  // 传输执行状态

		// 能力描述（JSON 字符串，含前端集成 URL）
		Capabilities: &capabilitiesStr,

		IsEnabled: true,
	}

	return s.sendRegistration(&registration)
}

// sendRegistration 发送注册请求到 System task_providers API
func (s *TaskProviderRegistryService) sendRegistration(req *TaskProviderRegistration) error {
	bodyJSON, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal registration: %w", err)
	}

	// 注册到 task_providers Internal API（使用 Internal API Key 认证）
	httpReq, err := http.NewRequest("POST", s.systemURL+"/api/v1/internal/task-providers/register", bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Internal-API-Key", s.internalAPIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		// 读取响应 body 以获取详细错误信息
		var errBody map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&errBody)
		return fmt.Errorf("registration failed with status %d: %v", resp.StatusCode, errBody)
	}

	log.Printf("✅ Transfer 模块已成功注册到 task_providers (module_name: transfer)")
	return nil
}
