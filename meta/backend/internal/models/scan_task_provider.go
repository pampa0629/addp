package models

import (
	"time"

	commonExecution "github.com/addp/common/execution"
)

// ProviderScanTask 是 Meta 通过 TaskProvider API 暴露的标准任务定义。
// ScanTask 内部不持久化 task_type；TaskProvider 契约对外必须显式返回 task_type=scan。
type ProviderScanTask struct {
	ID                  uint       `json:"id"`
	TenantID            uint       `json:"tenant_id"`
	EngineID            uint       `json:"engine_id"`
	Name                string     `json:"name"`
	DisplayName         string     `json:"display_name,omitempty"`
	TaskType            string     `json:"task_type"`
	Description         string     `json:"description,omitempty"`
	Schedule            string     `json:"schedule,omitempty"`
	Enabled             bool       `json:"enabled"`
	Scope               JSONMap    `json:"scope,omitempty"`
	Parameters          JSONMap    `json:"parameters,omitempty"`
	OwnerModule         string     `json:"owner_module,omitempty"`
	OwnerRef            string     `json:"owner_ref,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastExecutionID     *string    `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string    `json:"last_execution_status,omitempty"`
	CreatedBy           uint       `json:"created_by,omitempty"`
	UpdatedBy           uint       `json:"updated_by,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// ListProviderScanTasksResponse 是 TaskProvider 标准任务列表响应。
type ListProviderScanTasksResponse struct {
	Items    []ProviderScanTask `json:"items"`
	Total    int64              `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
}

func NewProviderScanTask(task ScanTask) ProviderScanTask {
	return ProviderScanTask{
		ID:                  task.ID,
		TenantID:            task.TenantID,
		EngineID:            task.EngineID,
		Name:                task.Name,
		DisplayName:         task.Name,
		TaskType:            commonExecution.TaskTypeScan,
		Description:         task.Description,
		Schedule:            task.Schedule,
		Enabled:             task.Enabled,
		Scope:               task.Scope,
		Parameters:          task.Parameters,
		OwnerModule:         task.OwnerModule,
		OwnerRef:            task.OwnerRef,
		LastRunAt:           task.LastRunAt,
		NextRunAt:           task.NextRunAt,
		LastExecutionID:     task.LastExecutionID,
		LastExecutionStatus: task.LastExecutionStatus,
		CreatedBy:           task.CreatedBy,
		UpdatedBy:           task.UpdatedBy,
		CreatedAt:           task.CreatedAt,
		UpdatedAt:           task.UpdatedAt,
	}
}

func NewProviderScanTasks(tasks []ScanTask) []ProviderScanTask {
	result := make([]ProviderScanTask, 0, len(tasks))
	for _, task := range tasks {
		result = append(result, NewProviderScanTask(task))
	}
	return result
}
