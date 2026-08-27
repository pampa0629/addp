package service

import (
	"encoding/json"
	"fmt"

	commonModels "github.com/addp/common/models"
)

// TransferTaskProviderDeclaration 返回随 Transfer 模块定义一并发布的任务能力声明。
func TransferTaskProviderDeclaration() (*commonModels.TaskProviderDeclaration, error) {
	// 构造能力描述（含前端集成 URL）
	capabilities := map[string]interface{}{
		"schema_version": "task.capabilities/v2",
		"task_capabilities": []map[string]interface{}{
			{
				"type":                      "sync",
				"display_name":              "数据同步",
				"description":               "执行 Transfer 同步任务定义",
				"definition_schema":         map[string]interface{}{"type": "object"},
				"supports_schedule":         true,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/transfer/tasks/create",
				"edit_url":                  "/transfer/tasks/:id/edit",
				"deprecated":                false,
			},
		},
		"x_runtime_boundaries": []string{"bounded"},
		"x_load_modes":         []string{"snapshot", "incremental"},
		"x_change_detection":   []string{"watermark"},
		"x_features":           []string{"async", "restartable_retry", "watermark_resume", "field_mapping", "scheduled"},
	}

	// 序列化为 JSON 字符串
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := commonModels.JSONString(capabilitiesJSON)

	return &commonModels.TaskProviderDeclaration{
		DisplayName: "数据传输",
		Description: "数据同步任务",

		// API 端点配置
		TaskListEndpoint:    "/api/v1/transfer/task-provider/tasks",
		TaskDetailEndpoint:  "/api/v1/transfer/task-provider/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/transfer/task-provider/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/transfer/task-provider/executions/{execution_id}",

		// 能力描述（JSON 字符串，含前端集成 URL）
		Capabilities: &capabilitiesStr,
	}, nil
}
