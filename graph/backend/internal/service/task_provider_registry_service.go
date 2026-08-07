package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

// TaskProviderRegistryService 将 Graph 注册为任务提供者。
type TaskProviderRegistryService struct {
	systemClient *commonClient.SystemServiceClient
	graphURL     string
}

func NewTaskProviderRegistryService(systemClient *commonClient.SystemServiceClient, graphURL string) *TaskProviderRegistryService {
	return &TaskProviderRegistryService{
		systemClient: systemClient,
		graphURL:     graphURL,
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

func (s *TaskProviderRegistryService) Register(ctx context.Context) error {
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

	return s.sendRegistration(ctx, &registration)
}

func (s *TaskProviderRegistryService) sendRegistration(ctx context.Context, req *TaskProviderRegistration) error {
	if s == nil || s.systemClient == nil || req == nil {
		return fmt.Errorf("System Service Client and registration are required")
	}
	var capabilities *commonModels.JSONString
	if req.Capabilities != nil {
		value := commonModels.JSONString(*req.Capabilities)
		capabilities = &value
	}
	if err := s.systemClient.RegisterTaskProvider(ctx, &commonModels.TaskProvider{
		ModuleName: req.ModuleName, DisplayName: req.DisplayName, Description: req.Description,
		BaseURL: req.BaseURL, TaskListEndpoint: req.TaskListEndpoint,
		TaskDetailEndpoint: req.TaskDetailEndpoint, TaskExecuteEndpoint: req.TaskExecuteEndpoint,
		TaskStatusEndpoint: req.TaskStatusEndpoint, TaskCancelEndpoint: req.TaskCancelEndpoint,
		Capabilities: capabilities, IsEnabled: req.IsEnabled,
	}); err != nil {
		return err
	}

	log.Printf("✅ Graph 模块已成功注册到 task_providers (module_name: graph)")
	return nil
}
