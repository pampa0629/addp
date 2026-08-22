package service

import (
	"encoding/json"
	"fmt"

	commonModels "github.com/addp/common/models"
)

func OrchestratorTaskProviderDeclaration() (*commonModels.TaskProviderDeclaration, error) {
	capabilities := map[string]interface{}{
		"schema_version": "task.capabilities/v2",
		"task_capabilities": []map[string]interface{}{{
			"type": "orchestration", "display_name": "任务编排", "description": "执行已保存的 Orchestrator 编排定义",
			"definition_schema": map[string]interface{}{"type": "object"},
			"supports_schedule": true, "supports_cancel": false, "supports_inline_execution": false,
			"create_url": "/orchestrator/orchestrations/new", "edit_url": "/orchestrator/orchestrations/:id/edit", "deprecated": false,
		}},
	}
	encoded, err := json.Marshal(capabilities)
	if err != nil {
		return nil, fmt.Errorf("marshal Orchestrator TaskProvider capabilities: %w", err)
	}
	capabilitiesJSON := commonModels.JSONString(encoded)
	return &commonModels.TaskProviderDeclaration{
		DisplayName: "任务编排", Description: "跨模块任务编排和调度任务",
		TaskListEndpoint: "/api/v1/orchestrator/tasks", TaskDetailEndpoint: "/api/v1/orchestrator/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/orchestrator/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/orchestrator/executions/{execution_id}",
		Capabilities:        &capabilitiesJSON,
	}, nil
}
