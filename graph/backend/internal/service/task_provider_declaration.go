package service

import (
	"encoding/json"
	"fmt"

	commonModels "github.com/addp/common/models"
)

// TaskProviderDeclaration 返回 Graph 随模块注册发布的任务能力声明。
func GraphTaskProviderDeclaration() (*commonModels.TaskProviderDeclaration, error) {
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
		return nil, fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := commonModels.JSONString(capabilitiesJSON)

	return &commonModels.TaskProviderDeclaration{
		DisplayName:         "知识图谱",
		Description:         "知识图谱构建任务",
		TaskListEndpoint:    "/api/v1/graph/tasks",
		TaskDetailEndpoint:  "/api/v1/graph/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/graph/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/graph/executions/{execution_id}",
		Capabilities:        &capabilitiesStr,
	}, nil
}
