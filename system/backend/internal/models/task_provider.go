package models

import "time"

// TaskProvider 任务提供者（ADDP 内置模块）
// 供 Orchestrator 查询和调用
type TaskProvider struct {
	ID          uint   `gorm:"column:id;primaryKey" json:"id"`
	ModuleName  string `gorm:"column:module_name;uniqueIndex;size:50;not null" json:"module_name"` // 'transfer', 'meta', 'develop'
	DisplayName string `gorm:"column:display_name;size:100" json:"display_name"`
	Description string `gorm:"column:description;type:text" json:"description"`

	// API 配置
	BaseURL             string `gorm:"column:base_url;size:255" json:"base_url"`
	TaskListEndpoint    string `gorm:"column:task_list_endpoint;size:255" json:"task_list_endpoint"`
	TaskDetailEndpoint  string `gorm:"column:task_detail_endpoint;size:255" json:"task_detail_endpoint"`
	TaskExecuteEndpoint string `gorm:"column:task_execute_endpoint;size:255" json:"task_execute_endpoint"`
	TaskStatusEndpoint  string `gorm:"column:task_status_endpoint;size:255" json:"task_status_endpoint"`
	TaskCancelEndpoint  string `gorm:"column:task_cancel_endpoint;size:255" json:"task_cancel_endpoint,omitempty"`

	// 能力描述（JSON 格式，含 task_types、create_task_url、edit_task_url 等）
	Capabilities *string `gorm:"column:capabilities;type:json" json:"capabilities,omitempty"`

	// 状态
	IsEnabled bool `gorm:"column:is_enabled;default:true" json:"is_enabled"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (TaskProvider) TableName() string {
	return "task_providers"
}
