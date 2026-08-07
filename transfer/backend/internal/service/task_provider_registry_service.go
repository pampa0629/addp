package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

// TaskProviderRegistryService 任务提供者注册服务
// 将 Transfer 模块注册到 System 的 task_providers 表
type TaskProviderRegistryService struct {
	systemClient *commonClient.SystemServiceClient
	transferURL  string
}

// NewTaskProviderRegistryService 创建任务提供者注册服务
func NewTaskProviderRegistryService(systemClient *commonClient.SystemServiceClient, transferURL string) *TaskProviderRegistryService {
	return &TaskProviderRegistryService{
		systemClient: systemClient,
		transferURL:  transferURL,
	}
}

// TaskProviderRegistration 任务提供者注册请求
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

// Register 注册 Transfer 模块为任务提供者
func (s *TaskProviderRegistryService) Register(ctx context.Context) error {
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
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := string(capabilitiesJSON)

	// 构造注册请求（注册到 task_providers 表）
	registration := TaskProviderRegistration{
		ModuleName:  "transfer",
		DisplayName: "数据传输",
		Description: "数据同步任务",

		// API 端点配置
		BaseURL:             s.transferURL,
		TaskListEndpoint:    "/api/v1/transfer/tasks",                          // 标准 TaskProvider 列表；task_type 查询只返回 bounded
		TaskDetailEndpoint:  "/api/v1/transfer/tasks/{task_type}/{id}",         // 传输任务详情
		TaskExecuteEndpoint: "/api/v1/transfer/tasks/{task_type}/{id}/execute", // 启动传输任务
		TaskStatusEndpoint:  "/api/v1/transfer/executions/{execution_id}",      // 传输执行状态

		// 能力描述（JSON 字符串，含前端集成 URL）
		Capabilities: &capabilitiesStr,

		IsEnabled: true,
	}

	return s.sendRegistration(ctx, &registration)
}

// sendRegistration 发送注册请求到 System task_providers API
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

	log.Printf("✅ Transfer 模块已成功注册到 task_providers (module_name: transfer)")
	return nil
}
