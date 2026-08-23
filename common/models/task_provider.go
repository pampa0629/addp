package models

import "time"

// TaskProviderDeclaration 是模块定义附带的 TaskProvider 能力声明。
// 运行地址和管理员启用状态不属于声明。
type TaskProviderDeclaration struct {
	DisplayName string `json:"display_name"` // 显示名称
	Description string `json:"description"`  // 描述

	TaskListEndpoint    string `json:"task_list_endpoint"`             // 任务列表端点
	TaskDetailEndpoint  string `json:"task_detail_endpoint"`           // 任务详情端点
	TaskExecuteEndpoint string `json:"task_execute_endpoint"`          // 任务执行端点
	TaskStatusEndpoint  string `json:"task_status_endpoint"`           // 任务状态端点
	TaskCancelEndpoint  string `json:"task_cancel_endpoint,omitempty"` // 任务取消端点

	// 能力描述（JSON 格式，含 task.capabilities/v2、task_capabilities 等）
	Capabilities *JSONString `json:"capabilities,omitempty"`
}

// TaskProviderBackend 是 System 根据当前有效 Backend 租约生成的运行端点投影。
type TaskProviderBackend struct {
	InstanceID     string    `json:"instance_id"`
	BaseURL        string    `json:"base_url"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}

// TaskProvider 是 System 根据模块定义和当前 Backend 租约生成的读取投影。
type TaskProvider struct {
	ID            uint   `json:"id"`
	ModuleName    string `json:"module_name"`
	ModuleVersion int64  `json:"module_version"`
	Enabled       bool   `json:"enabled"`
	TaskProviderDeclaration

	Available         bool                  `json:"available"`
	UnavailableReason string                `json:"unavailable_reason,omitempty"`
	Backends          []TaskProviderBackend `json:"backends"`

	// ResolvedBaseURL 由调用方在一次实际调用前从 Backends 中选择，不属于 API 投影。
	ResolvedBaseURL string `json:"-"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
