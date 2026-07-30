package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

type TaskProviderRegistryService struct {
	systemClient *commonClient.SystemServiceClient
	metaURL      string
}

func NewTaskProviderRegistryService(systemClient *commonClient.SystemServiceClient, metaURL string) *TaskProviderRegistryService {
	return &TaskProviderRegistryService{systemClient: systemClient, metaURL: metaURL}
}

func (s *TaskProviderRegistryService) Register(ctx context.Context) error {
	if s == nil || s.systemClient == nil {
		return errors.New("System service client is required")
	}
	capabilities := map[string]interface{}{
		"schema_version": "task.capabilities/v1",
		"task_capabilities": []map[string]interface{}{
			{
				"type": "scan", "display_name": "元数据扫描", "description": "执行 Meta ScanTask",
				"definition_schema": map[string]interface{}{"type": "object"},
				"execution_schema":  map[string]interface{}{"type": "object", "additionalProperties": false},
				"supports_schedule": true, "supports_cancel": false, "supports_inline_execution": false,
				"create_url": "/meta/scan", "edit_url": "/meta/scan?task_id=:id", "deprecated": false,
			},
		},
		"x_supported_source_models": []string{"tabular_catalog", "branch_leaf_catalog", "object_catalog", "file_catalog"},
		"x_features":                []string{"async", "cron", "spatial_facts", "vector_index"},
	}
	encoded, err := json.Marshal(capabilities)
	if err != nil {
		return fmt.Errorf("marshal Meta TaskProvider capabilities: %w", err)
	}
	capabilitiesJSON := commonModels.JSONString(encoded)
	return s.systemClient.RegisterTaskProvider(ctx, &commonModels.TaskProvider{
		ModuleName: "meta", DisplayName: "元数据管理", Description: "元数据扫描、索引、向量化任务",
		BaseURL:          s.metaURL,
		TaskListEndpoint: "/api/v1/meta/tasks", TaskDetailEndpoint: "/api/v1/meta/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/meta/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/meta/executions/{execution_id}",
		Capabilities:        &capabilitiesJSON, IsEnabled: true,
	})
}
