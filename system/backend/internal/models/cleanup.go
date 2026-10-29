package models

import (
	"github.com/addp/common/events"
	"github.com/addp/common/execution"
	"time"
)

// CleanupTask contains frozen neutral request facts and real parent times.
type CleanupTask struct {
	TaskID          string                `json:"task_id"`
	Action          string                `json:"action"`
	TenantID        uint                  `json:"tenant_id"`
	CleanupMode     string                `json:"cleanup_mode,omitempty"`
	TriggerType     string                `json:"trigger_type"`
	CauseEvent      string                `json:"cause_event,omitempty"`
	Status          string                `json:"status"`
	ExpectedModules []string              `json:"expected_modules"`
	Context         events.CleanupContext `json:"context"`
	ExecutionID     string                `json:"execution_id"`
	RequestedBy     uint                  `json:"requested_by"`
	CreatedAt       time.Time             `json:"created_at"`
	StartedAt       *time.Time            `json:"started_at"`
	CompletedAt     *time.Time            `json:"completed_at"`
	TimeoutAt       time.Time             `json:"timeout_at"`
	BasedOnScan     string                `json:"based_on_scan,omitempty"`
}

type CleanupModuleExecution struct {
	*execution.Observation
	CleanupAdmission *execution.CleanupAdmissionView `json:"cleanup_admission,omitempty"`
	Receipt          *execution.CleanupReceipt       `json:"receipt,omitempty"`
}

type TaskStatusResponse struct {
	TaskID           string                    `json:"task_id"`
	Action           string                    `json:"action"`
	Status           string                    `json:"status"`
	Task             CleanupTask               `json:"task"`
	ObservedAt       time.Time                 `json:"observed_at"`
	CutoffAt         *time.Time                `json:"cutoff_at,omitempty"`
	ModuleExecutions *[]CleanupModuleExecution `json:"module_executions,omitempty"`
	Summary          *TaskSummary              `json:"summary,omitempty"`
}

// TaskSummary includes only readable child receipts in the frozen cutoff. It
// is absent before cutoff or when the Owner scope protocol is not applicable.
type TaskSummary struct {
	events.CleanupResultSummary
	Impact       events.CleanupImpactSummary `json:"impact"`
	ImpactDigest string                      `json:"impact_digest,omitempty"`
}
