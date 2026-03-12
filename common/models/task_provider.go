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
	BaseURL             string `json:"base_url"`               // 服务基础 URL
	TaskListEndpoint    string `json:"task_list_endpoint"`     // 任务列表端点
	TaskDetailEndpoint  string `json:"task_detail_endpoint"`   // 任务详情端点
	TaskExecuteEndpoint string `json:"task_execute_endpoint"`  // 任务执行端点
	TaskStatusEndpoint  string `json:"task_status_endpoint"`   // 任务状态端点
	TaskCancelEndpoint  string `json:"task_cancel_endpoint,omitempty"` // 任务取消端点

	// 能力描述（JSON 格式，含 task_types、create_task_url 等）
	Capabilities *string `json:"capabilities,omitempty"`

	// 状态
	IsEnabled bool `json:"is_enabled"` // 是否启用

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
