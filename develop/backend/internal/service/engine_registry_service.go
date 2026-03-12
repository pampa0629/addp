package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// EngineRegistryService 任务提供者注册服务
// 将 Develop 模块注册到 System 的 task_providers 表
type EngineRegistryService struct {
	systemURL      string
	internalAPIKey string
	developURL     string
}

// NewEngineRegistryService 创建引擎注册服务
func NewEngineRegistryService(systemURL, internalAPIKey, developURL string) *EngineRegistryService {
	return &EngineRegistryService{
		systemURL:      systemURL,
		internalAPIKey: internalAPIKey,
		developURL:     developURL,
	}
}

// TaskProviderRegistration 任务提供者注册请求
type TaskProviderRegistration struct {
	ModuleName          string                 `json:"module_name"`
	DisplayName         string                 `json:"display_name"`
	Description         string                 `json:"description"`
	BaseURL             string                 `json:"base_url"`
	TaskListEndpoint    string                 `json:"task_list_endpoint"`
	TaskDetailEndpoint  string                 `json:"task_detail_endpoint"`
	TaskExecuteEndpoint string                 `json:"task_execute_endpoint"`
	TaskStatusEndpoint  string                 `json:"task_status_endpoint"`
	TaskCancelEndpoint  string                 `json:"task_cancel_endpoint,omitempty"`
	Capabilities        map[string]interface{} `json:"capabilities,omitempty"`
	IsEnabled           bool                   `json:"is_enabled"`
}

// RegisterEngine 注册 Develop 模块为任务提供者
func (s *EngineRegistryService) RegisterEngine() error {
	// 构造注册请求（注册到 task_providers 表）
	registration := TaskProviderRegistration{
		ModuleName:  "develop",
		DisplayName: "开发工作台",
		Description: "SQL 查询和空间工作流开发任务",

		// API 端点配置
		BaseURL:             s.developURL,
		TaskListEndpoint:    "/api/develop/items",
		TaskDetailEndpoint:  "/api/develop/items/:id",
		TaskExecuteEndpoint: "/api/develop/items/:id/execute",
		TaskStatusEndpoint:  "/api/develop/executions/:id",

		// 能力描述（供 Orchestrator 查询）
		Capabilities: map[string]interface{}{
			"task_types": []map[string]string{
				{
					"type":         "sql",
					"display_name": "SQL 任务",
				},
				{
					"type":         "workflow",
					"display_name": "工作流任务",
				},
			},
			"create_task_url": "http://localhost:5177/#/workflow/new",
			"edit_task_url":   "http://localhost:5177/#/workflow/:id",
		},

		IsEnabled: true,
	}

	return s.sendRegistration(&registration)
}

// sendRegistration 发送注册请求到 System task_providers API
func (s *EngineRegistryService) sendRegistration(req *TaskProviderRegistration) error {
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

	log.Printf("✅ Develop 模块已成功注册到 task_providers (module_name: develop)")
	return nil
}
