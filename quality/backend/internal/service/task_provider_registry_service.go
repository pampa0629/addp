package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

// TaskProviderRegistryService 将 Quality 注册为任务提供者。
type TaskProviderRegistryService struct {
	systemClient *commonClient.SystemServiceClient
	qualityURL   string
}

func NewTaskProviderRegistryService(systemClient *commonClient.SystemServiceClient, qualityURL string) *TaskProviderRegistryService {
	return &TaskProviderRegistryService{
		systemClient: systemClient,
		qualityURL:   qualityURL,
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
				"type":                      "check",
				"display_name":              "质量检查",
				"description":               "执行 Quality 检查任务",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/quality/check-tasks?create=1",
				"edit_url":                  "/quality/check-tasks?task_id=:id",
				"deprecated":                false,
			},
		},
		"x_supported_source_models": []string{"tabular_catalog"},
		"x_features":                []string{"async", "quality_rules", "issue_generation"},
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

	log.Printf("✅ Quality 模块已成功注册到 task_providers (module_name: quality)")
	return nil
}
