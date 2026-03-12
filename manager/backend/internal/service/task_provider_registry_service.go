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
// 将 Manager 模块注册到 System 的 task_providers 表
type TaskProviderRegistryService struct {
	systemURL      string
	internalAPIKey string
	managerURL     string
}

// NewTaskProviderRegistryService 创建任务提供者注册服务
func NewTaskProviderRegistryService(systemURL, internalAPIKey, managerURL string) *TaskProviderRegistryService {
	return &TaskProviderRegistryService{
		systemURL:      systemURL,
		internalAPIKey: internalAPIKey,
		managerURL:     managerURL,
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
	Capabilities        *string `json:"capabilities,omitempty"` // JSON 字符串
	IsEnabled           bool    `json:"is_enabled"`
}

// Register 注册 Manager 模块为任务提供者
func (s *TaskProviderRegistryService) Register() error {
	// 构造能力描述
	capabilities := map[string]interface{}{
		"task_types": []map[string]interface{}{
			{
				"type":         "mvt_generation",
				"display_name": "MVT 瓦片生成",
				"description":  "对空间表生成矢量瓦片并缓存到 MinIO",
				"create_url":   "/manager/mvt-tasks/create",
				"edit_url":     "/manager/mvt-tasks/:id/edit",
			},
			{
				"type":         "embedding",
				"display_name": "向量化",
				"description":  "对对象存储文件进行多模态向量化",
				"create_url":   "/manager/embedding-tasks/create",
				"edit_url":     "/manager/embedding-tasks/:id/edit",
			},
		},
		"supports_cancel":   true,
		"supports_schedule": false,
	}

	// 序列化为 JSON 字符串
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := string(capabilitiesJSON)

	// 构造注册请求（注册到 task_providers 表）
	registration := TaskProviderRegistration{
		ModuleName:  "manager",
		DisplayName: "数据管理",
		Description: "MVT 瓦片生成任务和对象存储向量化任务",

		// API 端点配置（相对于 base_url，支持 {task_type}/{id} 占位符）
		BaseURL:             s.managerURL,
		TaskListEndpoint:    "/api/manager/tasks",
		TaskDetailEndpoint:  "/api/manager/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/manager/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/manager/executions/{execution_id}",
		TaskCancelEndpoint:  "/api/manager/executions/{execution_id}/cancel",

		// 能力描述（JSON 字符串）
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
	httpReq, err := http.NewRequest("POST", s.systemURL+"/internal/task-providers/register", bytes.NewReader(bodyJSON))
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

	log.Printf("✅ Manager 模块已成功注册到 task_providers (module_name: manager)")
	return nil
}
