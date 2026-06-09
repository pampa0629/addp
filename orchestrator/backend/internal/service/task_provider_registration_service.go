package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// TaskProviderRegistrationService 将 Orchestrator 注册为任务提供者。
type TaskProviderRegistrationService struct {
	systemURL       string
	internalAPIKey  string
	orchestratorURL string
}

func NewTaskProviderRegistrationService(systemURL, internalAPIKey, orchestratorURL string) *TaskProviderRegistrationService {
	return &TaskProviderRegistrationService{
		systemURL:       systemURL,
		internalAPIKey:  internalAPIKey,
		orchestratorURL: orchestratorURL,
	}
}

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

func (s *TaskProviderRegistrationService) Register() error {
	capabilities := map[string]interface{}{
		"schema_version": "task.capabilities/v1",
		"task_types": []map[string]interface{}{
			{
				"type":                      "orchestration",
				"display_name":              "任务编排",
				"description":               "执行已保存的 Orchestrator 编排定义",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"execution_schema":          map[string]interface{}{"type": "object"},
				"supports_schedule":         true,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/orchestrator/orchestrations",
				"edit_url":                  "/orchestrator/orchestrations/:id/edit",
				"deprecated":                false,
			},
		},
	}
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := string(capabilitiesJSON)

	registration := TaskProviderRegistration{
		ModuleName:          "orchestrator",
		DisplayName:         "任务编排",
		Description:         "跨模块任务编排和调度任务",
		BaseURL:             s.orchestratorURL,
		TaskListEndpoint:    "/api/v1/orchestrator/tasks",
		TaskDetailEndpoint:  "/api/v1/orchestrator/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/orchestrator/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/orchestrator/executions/{execution_id}",
		Capabilities:        &capabilitiesStr,
		IsEnabled:           true,
	}

	return s.sendRegistration(&registration)
}

func (s *TaskProviderRegistrationService) sendRegistration(req *TaskProviderRegistration) error {
	bodyJSON, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal registration: %w", err)
	}

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
		var errBody map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return fmt.Errorf("registration failed with status %d: %v", resp.StatusCode, errBody)
	}

	log.Printf("✅ Orchestrator 模块已成功注册到 task_providers (module_name: orchestrator)")
	return nil
}
