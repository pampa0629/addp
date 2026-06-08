package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// TaskProviderRegistryService 将 Quality 注册为任务提供者。
type TaskProviderRegistryService struct {
	systemURL      string
	internalAPIKey string
	qualityURL     string
}

func NewTaskProviderRegistryService(systemURL, internalAPIKey, qualityURL string) *TaskProviderRegistryService {
	return &TaskProviderRegistryService{
		systemURL:      systemURL,
		internalAPIKey: internalAPIKey,
		qualityURL:     qualityURL,
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

func (s *TaskProviderRegistryService) Register() error {
	capabilities := map[string]interface{}{
		"schema_version": "task.capabilities/v1",
		"task_types": []map[string]interface{}{
			{
				"type":                      "check",
				"display_name":              "质量检查",
				"description":               "执行 Quality 检查任务",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"execution_schema":          map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/quality/check-tasks",
				"edit_url":                  "/quality/check-tasks?task_id=:id",
				"deprecated":                false,
			},
		},
		"supported_source_models": []string{"tabular_catalog"},
		"features":                []string{"async", "quality_rules", "issue_generation"},
	}

	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := string(capabilitiesJSON)

	registration := TaskProviderRegistration{
		ModuleName:          "quality",
		DisplayName:         "数据质量",
		Description:         "数据质量检查任务",
		BaseURL:             s.qualityURL,
		TaskListEndpoint:    "/api/v1/quality/tasks",
		TaskDetailEndpoint:  "/api/v1/quality/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/quality/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/quality/executions/{execution_id}",
		Capabilities:        &capabilitiesStr,
		IsEnabled:           true,
	}

	return s.sendRegistration(&registration)
}

func (s *TaskProviderRegistryService) sendRegistration(req *TaskProviderRegistration) error {
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

	log.Printf("✅ Quality 模块已成功注册到 task_providers (module_name: quality)")
	return nil
}
