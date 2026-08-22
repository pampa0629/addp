package service

import (
	"encoding/json"
	"fmt"

	commonModels "github.com/addp/common/models"
)

func TaskProviderDeclaration() (*commonModels.TaskProviderDeclaration, error) {
	capabilities := map[string]interface{}{
		"schema_version": "task.capabilities/v2",
		"task_capabilities": []map[string]interface{}{{
			"type": "scan", "display_name": "元数据扫描", "description": "执行 Meta ScanTask",
			"definition_schema": map[string]interface{}{"type": "object"},
			"supports_schedule": true, "supports_cancel": false, "supports_inline_execution": false,
			"create_url": "/meta/scan", "edit_url": "/meta/scan?task_id=:id", "deprecated": false,
		}},
		"x_supported_source_models": []string{"tabular_catalog", "branch_leaf_catalog", "object_catalog", "file_catalog"},
		"x_features":                []string{"async", "cron", "spatial_facts", "vector_index"},
	}
	encoded, err := json.Marshal(capabilities)
	if err != nil {
		return nil, fmt.Errorf("marshal Meta TaskProvider capabilities: %w", err)
	}
	capabilitiesJSON := commonModels.JSONString(encoded)
	return &commonModels.TaskProviderDeclaration{
		DisplayName: "元数据管理", Description: "元数据扫描、索引、向量化任务",
		TaskListEndpoint: "/api/v1/meta/tasks", TaskDetailEndpoint: "/api/v1/meta/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/meta/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/meta/executions/{execution_id}",
		Capabilities:        &capabilitiesJSON,
	}, nil
}
