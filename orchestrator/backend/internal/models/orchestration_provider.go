package models

import (
	"time"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/taskprovider"
)

// ProviderOrchestrationTask 是 Orchestrator 通过 TaskProvider API 暴露的标准任务定义。
// Orchestrator 内部持久化的是编排定义，对外固定声明 task_type=orchestration。
type ProviderOrchestrationTask struct {
	ID                  uint                           `json:"id"`
	TenantID            uint                           `json:"tenant_id"`
	TaskType            string                         `json:"task_type"`
	Name                string                         `json:"name"`
	DisplayName         string                         `json:"display_name,omitempty"`
	Description         string                         `json:"description,omitempty"`
	Steps               Steps                          `json:"steps,omitempty"`
	Enabled             bool                           `json:"enabled"`
	Schedule            string                         `json:"schedule,omitempty"`
	LastRunAt           *time.Time                     `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time                     `json:"next_run_at,omitempty"`
	LastExecutionID     *string                        `json:"last_execution_id,omitempty"`
	LastExecutionStatus *string                        `json:"last_execution_status,omitempty"`
	CreatedBy           *uint                          `json:"created_by,omitempty"`
	CreatedAt           time.Time                      `json:"created_at"`
	UpdatedAt           time.Time                      `json:"updated_at"`
	ExecutionContract   taskprovider.ExecutionContract `json:"execution_contract"`
}

// ListProviderOrchestrationTasksResponse 是 TaskProvider 标准任务列表响应。
type ListProviderOrchestrationTasksResponse struct {
	Items    []ProviderOrchestrationTask `json:"items"`
	Total    int64                       `json:"total"`
	Page     int                         `json:"page"`
	PageSize int                         `json:"page_size"`
}

func NewProviderOrchestrationTask(orch Orchestration) ProviderOrchestrationTask {
	return ProviderOrchestrationTask{
		ID:                  orch.ID,
		TenantID:            orch.TenantID,
		TaskType:            commonExecution.TaskTypeOrchestration,
		Name:                orch.Name,
		DisplayName:         orch.Name,
		Description:         orch.Description,
		Steps:               orch.Steps,
		Enabled:             orch.Enabled,
		Schedule:            orch.Schedule,
		LastRunAt:           orch.LastRunAt,
		NextRunAt:           orch.NextRunAt,
		LastExecutionID:     orch.LastExecutionID,
		LastExecutionStatus: orch.LastExecutionStatus,
		CreatedBy:           orch.CreatedBy,
		CreatedAt:           orch.CreatedAt,
		UpdatedAt:           orch.UpdatedAt,
		ExecutionContract:   taskprovider.EmptyExecutionContract(),
	}
}
