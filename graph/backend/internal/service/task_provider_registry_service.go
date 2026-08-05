package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// TaskProviderRegistryService 将 Graph 注册为任务提供者。
type TaskProviderRegistryService struct {
	systemURL      string
	internalAPIKey string
	graphURL       string
}

func NewTaskProviderRegistryService(systemURL, internalAPIKey, graphURL string) *TaskProviderRegistryService {
	return &TaskProviderRegistryService{
		systemURL:      systemURL,
		internalAPIKey: internalAPIKey,
		graphURL:       graphURL,
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
		"schema_version": "task.capabilities/v2",
		"task_capabilities": []map[string]interface{}{
			{
				"type":                      "kg_build",
				"display_name":              "图谱构建",
				"description":               "执行 Graph 知识图谱构建任务",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/graph/graphs",
				"edit_url":                  "/graph/graphs/:graph_id/build/tasks/:id",
				"deprecated":                false,
			},
		},
		"x_supported_source_models": []string{"document", "object_catalog"},
		"x_features":                []string{"async", "kg_build", "review_queue"},
	}

	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := string(capabilitiesJSON)

	registration := TaskProviderRegistration{
		ModuleName:          "graph",
		DisplayName:         "知识图谱",
		Description:         "知识图谱构建任务",
		BaseURL:             s.graphURL,
		TaskListEndpoint:    "/api/v1/graph/tasks",
		TaskDetailEndpoint:  "/api/v1/graph/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/graph/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/graph/executions/{execution_id}",
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

	log.Printf("✅ Graph 模块已成功注册到 task_providers (module_name: graph)")
	return nil
}
