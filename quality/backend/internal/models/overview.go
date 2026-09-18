package models

import (
	"encoding/json"
	"time"
)

type QualityOverview struct {
	PlanCount          int64                 `json:"plan_count"`
	NeverRunPlans      int64                 `json:"never_run_plans"`
	OpenIssues         int64                 `json:"open_issues"`
	AcceptedIssues     int64                 `json:"accepted_issues"`
	UnscopedIssues     int64                 `json:"unscoped_issues"`
	UnscopedExecutions int64                 `json:"unscoped_executions"`
	Data               []QualityScopeSummary `gorm:"-" json:"data"`
	Total              int64                 `json:"total"`
	Page               int                   `json:"page"`
	PageSize           int                   `json:"page_size"`
	TotalPages         int                   `json:"total_pages"`
	Trend              []QualityDailySummary `gorm:"-" json:"trend"`
}

type QualityScopeSummary struct {
	PlanID              int64           `json:"plan_id"`
	PlanName            string          `json:"plan_name"`
	PlanVersion         int64           `json:"plan_version"`
	OwnerDomainID       *int64          `json:"owner_domain_id"`
	TargetKey           *string         `json:"target_key"`
	TableBindings       json.RawMessage `json:"table_bindings"`
	ExecutionID         *string         `json:"execution_id"`
	Status              *string         `json:"status"`
	CreatedAt           *time.Time      `json:"created_at"`
	ObservedExecutionID *string         `json:"observed_execution_id"`
	ObservedAt          *time.Time      `json:"observed_at"`
	ObservedVersion     *int64          `json:"observed_version"`
	PassedRules         *int64          `json:"passed_rules"`
	TotalRules          *int64          `json:"total_rules"`
	PassRate            *float64        `json:"pass_rate"`
}

type QualityDailySummary struct {
	Day           string   `json:"day"`
	Executions    int64    `json:"executions"`
	RuntimeErrors int64    `json:"runtime_errors"`
	PassedRules   int64    `json:"passed_rules"`
	TotalRules    int64    `json:"total_rules"`
	PassRate      *float64 `json:"pass_rate"`
}
