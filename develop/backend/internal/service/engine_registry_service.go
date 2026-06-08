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

// RegisterEngine 注册 Develop 模块为任务提供者
func (s *EngineRegistryService) RegisterEngine() error {
	// 能力描述（供 Orchestrator 查询）
	capabilities := map[string]interface{}{
		"schema_version": "task.capabilities/v1",
		"task_types": []map[string]interface{}{
			{
				"type":                      "query",
				"display_name":              "查询任务",
				"description":               "执行 SQL 查询开发任务",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"execution_schema":          map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/develop/tasks?action=create&task_type=query",
				"edit_url":                  "/develop/tasks?action=edit&id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "workflow",
				"display_name":              "工作流任务",
				"description":               "执行 Develop 工作流任务",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"execution_schema":          map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/develop/tasks?action=create&task_type=workflow",
				"edit_url":                  "/develop/tasks?action=edit&id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "script",
				"display_name":              "脚本任务",
				"description":               "执行脚本开发任务；当前由 Jupyter Notebook runtime 承载",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"execution_schema":          map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/develop/tasks?action=create&task_type=script",
				"edit_url":                  "/develop/tasks?action=edit&id=:id",
				"deprecated":                false,
			},
		},
	}
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := string(capabilitiesJSON)

	// 构造注册请求（注册到 task_providers 表）
	registration := TaskProviderRegistration{
		ModuleName:  "develop",
		DisplayName: "数据开发",
		Description: "SQL 查询、工作流和脚本开发任务",

		// API 端点配置
		BaseURL:             s.developURL,
		TaskListEndpoint:    "/api/v1/develop/tasks",
		TaskDetailEndpoint:  "/api/v1/develop/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/develop/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/develop/executions/{execution_id}",

		Capabilities: &capabilitiesStr,

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

	log.Printf("✅ Develop 模块已成功注册到 task_providers (module_name: develop)")
	return nil
}
