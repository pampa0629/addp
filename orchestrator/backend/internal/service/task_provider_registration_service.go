package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

// TaskProviderRegistrationService 将 Orchestrator 注册为任务提供者。
type TaskProviderRegistrationService struct {
	systemClient    *commonClient.SystemServiceClient
	orchestratorURL string
}

func NewTaskProviderRegistrationService(systemClient *commonClient.SystemServiceClient, orchestratorURL string) *TaskProviderRegistrationService {
	return &TaskProviderRegistrationService{
		systemClient:    systemClient,
		orchestratorURL: orchestratorURL,
	}
}

func (s *TaskProviderRegistrationService) Register(ctx context.Context) error {
	if s == nil || s.systemClient == nil {
		return fmt.Errorf("System Service Client is required")
	}
	capabilities := map[string]interface{}{
		"schema_version": "task.capabilities/v2",
		"task_capabilities": []map[string]interface{}{
			{
				"type":                      "orchestration",
				"display_name":              "任务编排",
				"description":               "执行已保存的 Orchestrator 编排定义",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         true,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/orchestrator/orchestrations/new",
				"edit_url":                  "/orchestrator/orchestrations/:id/edit",
				"deprecated":                false,
			},
		},
	}
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := commonModels.JSONString(capabilitiesJSON)

	registration := commonModels.TaskProvider{
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

	if err := s.systemClient.RegisterTaskProvider(ctx, &registration); err != nil {
		return err
	}
	log.Printf("✅ Orchestrator 模块已成功注册到 task_providers (module_name: orchestrator)")
	return nil
}
