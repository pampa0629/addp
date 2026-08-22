package service

import (
	"encoding/json"
	"fmt"

	commonModels "github.com/addp/common/models"
)

// TaskProviderDeclaration 返回 Quality 随模块注册发布的任务能力声明。
func QualityTaskProviderDeclaration() (*commonModels.TaskProviderDeclaration, error) {
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
		return nil, fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := commonModels.JSONString(capabilitiesJSON)

	return &commonModels.TaskProviderDeclaration{
		DisplayName:         "数据质量",
		Description:         "数据质量检查任务",
		TaskListEndpoint:    "/api/v1/quality/tasks",
		TaskDetailEndpoint:  "/api/v1/quality/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/quality/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/quality/executions/{execution_id}",
		Capabilities:        &capabilitiesStr,
	}, nil
}
