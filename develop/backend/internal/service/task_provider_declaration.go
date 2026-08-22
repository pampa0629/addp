package service

import (
	"encoding/json"
	"fmt"

	commonModels "github.com/addp/common/models"
)

func DevelopTaskProviderDeclaration() (*commonModels.TaskProviderDeclaration, error) {
	// 能力描述（供 Orchestrator 查询）
	capabilities := map[string]interface{}{
		"schema_version": "task.capabilities/v2",
		"task_capabilities": []map[string]interface{}{
			{
				"type":                      "query",
				"display_name":              "查询任务",
				"description":               "执行 SQL 查询开发任务",
				"definition_schema":         queryTaskDefinitionSchema(),
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
		return nil, fmt.Errorf("failed to marshal capabilities: %w", err)
	}
	capabilitiesStr := commonModels.JSONString(capabilitiesJSON)

	// 构造随模块定义一并发布的 TaskProvider 声明。
	return &commonModels.TaskProviderDeclaration{
		DisplayName: "数据开发",
		Description: "SQL 查询、工作流和脚本开发任务",

		// API 端点配置
		TaskListEndpoint:    "/api/v1/develop/task-provider/tasks",
		TaskDetailEndpoint:  "/api/v1/develop/task-provider/tasks/{task_type}/{id}",
		TaskExecuteEndpoint: "/api/v1/develop/task-provider/tasks/{task_type}/{id}/execute",
		TaskStatusEndpoint:  "/api/v1/develop/task-provider/executions/{execution_id}",

		Capabilities: &capabilitiesStr,
	}, nil
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
