package models

import (
	"encoding/json"
	"time"
)

// Issue 质量问题工单
type Issue struct {
	ID              int64           `gorm:"primaryKey" json:"id"`
	Version         int64           `gorm:"not null;default:1" json:"version"`
	Evidence        json.RawMessage `gorm:"type:jsonb" json:"-"`
	AcceptedKeys    json.RawMessage `gorm:"type:jsonb" json:"-"`
	EvidenceReason  string          `gorm:"not null;default:''" json:"evidence_reason"`
	AcceptedCount   int64           `gorm:"not null;default:0" json:"accepted_count"`
	PendingCount    int64           `gorm:"not null;default:0" json:"pending_count"`
	History         []IssueAction   `gorm:"-" json:"history,omitempty"`
	TenantID        int64           `gorm:"not null;index;uniqueIndex:uq_quality_issue_rule" json:"tenant_id"`
	ExecutionID     string          `gorm:"size:255;not null;index" json:"execution_id"` // common.task_executions.execution_id
	LastExecutionID string          `gorm:"size:255;not null;index" json:"last_execution_id"`
	PlanID          int64           `gorm:"not null;uniqueIndex:uq_quality_issue_rule" json:"plan_id"`
	TargetKey       *string         `gorm:"size:64;uniqueIndex:uq_quality_issue_rule" json:"target_key"`
	OwnerDomainID   *int64          `gorm:"index" json:"owner_domain_id,omitempty"`
	RuleKey         string          `gorm:"type:uuid;not null;uniqueIndex:uq_quality_issue_rule" json:"rule_key"`
	RuleType        string          `gorm:"size:100;not null" json:"type"`
	Severity        string          `gorm:"size:20;not null;default:'error'" json:"severity"`
	Message         string          `gorm:"type:text" json:"message"`
	ColumnName      string          `gorm:"type:text;not null" json:"column_name"`
	Table           string          `gorm:"size:200;not null;column:table_name" json:"table_name"`
	SchemaName      string          `gorm:"size:200" json:"schema_name"`
	EngineID        int64           `gorm:"not null" json:"engine_id"`
	FailedCount     int64           `gorm:"not null" json:"failed_count"`
	TotalCount      int64           `gorm:"not null" json:"total_count"`
	PassRate        float64         `gorm:"not null" json:"pass_rate"`
	Detail          json.RawMessage `gorm:"type:jsonb" json:"detail,omitempty"`
	Status          string          `gorm:"size:50;not null;default:'open'" json:"status"` // open/resolved/accepted; ignored for history and cleanup
	ResolvedAt      *time.Time      `json:"resolved_at,omitempty"`
	ResolvedBy      *int64          `json:"resolved_by,omitempty"`
	ResolutionNote  string          `gorm:"type:text" json:"resolution_note,omitempty"`
	LastObservedAt  *time.Time      `json:"last_observed_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

func (Issue) TableName() string { return "quality.issues" }

type IssueObservation struct {
	Evidence      *FailureEvidence
	TargetKey     string
	PlanID        int64
	OwnerDomainID *int64
	RuleKey       string
	RuleType      string
	Severity      string
	Message       string
	ColumnName    string
	Table         string
	SchemaName    string
	EngineID      int64
	FailedCount   int64
	TotalCount    int64
	PassRate      float64
	Passed        bool
}

// FailureEvidence is owner-internal: never expose record hashes as row data.
type FailureEvidence struct {
	Scope  string   `json:"scope"`
	Keys   []string `json:"keys"`
	Reason string   `json:"reason"`
}

const MaxFailureKeys = 10000

type IssueAction struct {
	ID            int64           `gorm:"primaryKey" json:"id"`
	TenantID      int64           `json:"-"`
	IssueID       int64           `json:"issue_id"`
	PlanID        int64           `json:"plan_id"`
	ExecutionID   string          `json:"execution_id"`
	Action        string          `json:"action"`
	ActorID       *int64          `json:"actor_id,omitempty"`
	Note          string          `json:"note"`
	AcceptedCount int64           `json:"accepted_count"`
	Evidence      json.RawMessage `gorm:"type:jsonb" json:"-"`
	CreatedAt     time.Time       `json:"created_at"`
}

func (IssueAction) TableName() string { return "quality.issue_actions" }
