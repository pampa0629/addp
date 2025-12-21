package models

import "time"

// TaskProvider 任务提供者(ADDP 内置模块)
// 供 Orchestrator 查询和调用
type TaskProvider struct {
	ID          uint   `json:"id"`
	ModuleName  string `json:"module_name"`  // 'transfer', 'meta', 'develop', 'manager'
	DisplayName string `json:"display_name"` // 显示名称
	Description string `json:"description"`  // 描述

	// API 配置
	BaseURL             string `json:"base_url"`              // 服务基础 URL
	TaskListEndpoint    string `json:"task_list_endpoint"`    // 任务列表端点
	TaskDetailEndpoint  string `json:"task_detail_endpoint"`  // 任务详情端点
	TaskExecuteEndpoint string `json:"task_execute_endpoint"` // 任务执行端点
	TaskStatusEndpoint  string `json:"task_status_endpoint"`  // 任务状态端点

	// 能力描述
	Capabilities *string `json:"capabilities,omitempty"` // JSON 格式的能力描述

	// 前端集成(可选)
	CreateTaskURL string `json:"create_task_url,omitempty"` // 创建任务的前端 URL
	EditTaskURL   string `json:"edit_task_url,omitempty"`   // 编辑任务的前端 URL

	// 状态
	IsEnabled bool `json:"is_enabled"` // 是否启用

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
