package models

import "time"

// ExecutionPolicy is Develop's typed bounded execution policy.
// Platform owns the hard limits; a tenant row may override only the default timeout.
type ExecutionPolicy struct {
	ID                        uint      `gorm:"primaryKey" json:"-"`
	ScopeType                 string    `gorm:"size:32;not null;uniqueIndex:idx_develop_execution_policy_scope,priority:1" json:"scope_type"`
	TenantID                  *uint     `gorm:"uniqueIndex:idx_develop_execution_policy_scope,priority:2" json:"tenant_id,omitempty"`
	DefaultQueryTimeout       int       `gorm:"not null" json:"default_query_timeout"`
	MaxQueryTimeout           int       `gorm:"not null" json:"max_query_timeout"`
	QueryResultLimit          int       `gorm:"not null" json:"query_result_limit"`
	QueryConcurrency          int       `gorm:"not null;default:20" json:"query_concurrency"`
	QueryPerEngineConcurrency int       `gorm:"not null;default:5" json:"query_per_engine_concurrency"`
	WorkflowConcurrency       int       `gorm:"not null;default:2" json:"workflow_concurrency"`
	NotebookConcurrency       int       `gorm:"not null;default:1" json:"notebook_concurrency"`
	Version                   uint64    `gorm:"not null" json:"version"`
	UpdatedBy                 uint      `gorm:"not null" json:"updated_by"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

func (ExecutionPolicy) TableName() string { return "develop.execution_policy" }

// ExecutionConcurrency is a single applied version of the platform's budgets.
type ExecutionConcurrency struct{ Query, QueryPerEngine, Workflow, Notebook int }

func (c ExecutionConcurrency) Valid() bool {
	return c.Query > 0 && c.QueryPerEngine > 0 && c.QueryPerEngine <= c.Query && c.Workflow > 0 && c.Notebook > 0
}
func (c ExecutionConcurrency) For(taskType string) int {
	switch taskType {
	case "query":
		return c.Query
	case "workflow":
		return c.Workflow
	case "script":
		return c.Notebook
	}
	return 0
}
