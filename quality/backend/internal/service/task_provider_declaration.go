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
			{
				"type":                      "data_validation",
				"display_name":              "数据校验",
				"description":               "对所选正式物理表执行类型化质量断言",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/quality/data-validation-tasks?create=1",
				"edit_url":                  "/quality/data-validation-tasks?task_id=:id",
				"deprecated":                false,
			},
		},
		"x_supported_source_models": []string{"tabular_catalog"},
		"x_features":                []string{"async", "quality_rules", "issue_generation", "data_validation"},
	}

	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := commonModels.JSONString(capabilitiesJSON)

	return &commonModels.TaskProviderDeclaration{
		DisplayName:         "数据质量",
		Description:         "数据质量检查与数据校验任务",
		TaskListEndpoint:    "/api/v1/quality/task-provider/tasks",
		TaskDetailEndpoint:  "/api/v1/quality/task-provider/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/quality/task-provider/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/quality/task-provider/executions/{execution_id}",
		Capabilities:        &capabilitiesStr,
	}, nil
}
