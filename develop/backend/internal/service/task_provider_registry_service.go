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
// 将 Develop 模块注册到 System 的 task_providers 表
type TaskProviderRegistryService struct {
	systemClient *commonClient.SystemServiceClient
	developURL   string
}

// NewTaskProviderRegistryService 创建任务提供者注册服务
func NewTaskProviderRegistryService(systemClient *commonClient.SystemServiceClient, developURL string) *TaskProviderRegistryService {
	return &TaskProviderRegistryService{
		systemClient: systemClient,
		developURL:   developURL,
	}
}

// Register 注册 Develop 模块为任务提供者
func (s *TaskProviderRegistryService) Register(ctx context.Context) error {
	if s == nil || s.systemClient == nil {
		return fmt.Errorf("System Service Client is required")
	}
	// 能力描述（供 Orchestrator 查询）
	capabilities := map[string]interface{}{
		"schema_version": "task.capabilities/v1",
		"task_capabilities": []map[string]interface{}{
			{
				"type":                      "query",
				"display_name":              "查询任务",
				"description":               "执行 SQL 查询开发任务",
				"definition_schema":         queryTaskDefinitionSchema(),
				"execution_schema":          developExecutionSchema(),
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/develop/sql?action=create",
				"edit_url":                  "/develop/sql?action=edit&id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "workflow",
				"display_name":              "工作流任务",
				"description":               "执行 Develop 工作流任务",
				"definition_schema":         workflowTaskDefinitionSchema(),
				"execution_schema":          developExecutionSchema(),
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/develop/workflow?action=create",
				"edit_url":                  "/develop/workflow?action=edit&id=:id",
				"deprecated":                false,
			},
			{
				"type":                      "script",
				"display_name":              "脚本任务",
				"description":               "执行脚本开发任务；当前由 Jupyter Notebook runtime 承载",
				"definition_schema":         scriptTaskDefinitionSchema(),
				"execution_schema":          developExecutionSchema(),
				"supports_schedule":         false,
				"supports_cancel":           false,
				"supports_inline_execution": false,
				"create_url":                "/develop/notebook?action=create",
				"edit_url":                  "/develop/notebook?action=edit&id=:id",
				"deprecated":                false,
			},
		},
	}
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := commonModels.JSONString(capabilitiesJSON)

	// 构造注册请求（注册到 task_providers 表）
	registration := commonModels.TaskProvider{
		ModuleName:  "develop",
		DisplayName: "数据开发",
		Description: "SQL 查询、工作流和脚本开发任务",

		// API 端点配置
		BaseURL:             s.developURL,
		TaskListEndpoint:    "/api/v1/develop/task-provider/tasks",
		TaskDetailEndpoint:  "/api/v1/develop/task-provider/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/develop/task-provider/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/develop/task-provider/executions/{execution_id}",

		Capabilities: &capabilitiesStr,

		IsEnabled: true,
	}

	if err := s.systemClient.RegisterTaskProvider(ctx, &registration); err != nil {
		return err
	}
	log.Printf("✅ Develop 模块已成功注册到 task_providers (module_name: develop)")
	return nil
}

func baseDevelopDefinitionSchema(taskType string, contentProperties map[string]interface{}, contentRequired []interface{}) map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"title":       "Develop 任务定义公开摘要",
		"description": "Develop 任务定义归 Develop 模块所有；该 schema 只描述跨模块可展示的公开摘要字段，不用于 Orchestrator 渲染完整编辑表单。",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":  "string",
				"title": "任务名称",
			},
			"display_name": map[string]interface{}{
				"type":  "string",
				"title": "展示名称",
			},
			"task_type": map[string]interface{}{
				"type":    "string",
				"enum":    []interface{}{taskType},
				"default": taskType,
				"title":   "任务类型",
			},
			"description": map[string]interface{}{
				"type":  "string",
				"title": "描述",
			},
			"timeout": map[string]interface{}{
				"type":    "integer",
				"minimum": float64(0),
				"title":   "超时时间",
			},
			"content": map[string]interface{}{
				"type":                 "object",
				"title":                "公开内容摘要",
				"properties":           contentProperties,
				"required":             contentRequired,
				"additionalProperties": true,
			},
			"execution_config": map[string]interface{}{
				"type":                 "object",
				"title":                "执行配置摘要",
				"additionalProperties": true,
			},
		},
		"required":             []interface{}{"name", "task_type"},
		"additionalProperties": true,
	}
}

func queryTaskDefinitionSchema() map[string]interface{} {
	return baseDevelopDefinitionSchema(
		"query",
		map[string]interface{}{
			"query": map[string]interface{}{
				"type":  "string",
				"title": "查询语句",
			},
			"query_type": map[string]interface{}{
				"type":        "string",
				"title":       "查询类型",
				"description": "查询语言：sql、mql 或 cypher。",
				"enum":        []interface{}{"sql", "mql", "cypher"},
			},
		},
		[]interface{}{"query", "query_type"},
	)
}

func workflowTaskDefinitionSchema() map[string]interface{} {
	return baseDevelopDefinitionSchema(
		"workflow",
		map[string]interface{}{
			"workflow_definition": map[string]interface{}{
				"type":  "object",
				"title": "工作流定义摘要",
				"properties": map[string]interface{}{
					"tasks": map[string]interface{}{
						"type":  "array",
						"title": "工作流任务列表",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"id": map[string]interface{}{
									"type":  "string",
									"title": "任务 ID",
								},
								"operator": map[string]interface{}{
									"type":  "string",
									"title": "算子名称",
								},
								"params": map[string]interface{}{
									"type":                 "object",
									"title":                "算子参数",
									"additionalProperties": true,
								},
								"depends_on": map[string]interface{}{
									"type":  "array",
									"title": "前驱任务 ID",
									"items": map[string]interface{}{
										"type": "string",
									},
								},
							},
							"required":             []interface{}{"id", "operator", "params", "depends_on"},
							"additionalProperties": true,
						},
					},
				},
				"required":             []interface{}{"tasks"},
				"additionalProperties": true,
			},
			"inputs": map[string]interface{}{
				"type":                 "object",
				"title":                "输入参数摘要",
				"additionalProperties": true,
			},
		},
		[]interface{}{"workflow_definition"},
	)
}

func scriptTaskDefinitionSchema() map[string]interface{} {
	return baseDevelopDefinitionSchema(
		"script",
		map[string]interface{}{
			"notebook_path": map[string]interface{}{
				"type":  "string",
				"title": "Notebook 路径",
			},
			"parameters": map[string]interface{}{
				"type":                 "object",
				"title":                "脚本参数摘要",
				"additionalProperties": true,
			},
		},
		[]interface{}{},
	)
}

func developExecutionSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"title":                "Develop 执行参数",
		"description":          "Develop 执行参数由具体任务定义中的 parameter_schema/default_parameters 决定；本 schema 只声明 parameters 是开放对象，执行时会写入 execution_config.inputs。",
		"additionalProperties": true,
	}
}
