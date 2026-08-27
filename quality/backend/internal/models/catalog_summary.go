package models

import "time"

type CatalogSummaryReference struct {
	EngineID   int64  `json:"engine_id"`
	SchemaName string `json:"schema_name"`
	TableName  string `json:"table_name"`
}

type ResolveCatalogSummariesRequest struct {
	References []CatalogSummaryReference `json:"references"`
}

type CatalogSummaryResolution struct {
	Reference           CatalogSummaryReference `json:"reference"`
	Configured          bool                    `json:"configured"`
	CheckTaskID         int64                   `json:"check_task_id,omitempty"`
	LastExecutionID     string                  `json:"last_execution_id,omitempty"`
	LastExecutionStatus string                  `json:"last_execution_status,omitempty"`
	QualityScore        *float64                `json:"quality_score,omitempty"`
	OpenIssueCount      int64                   `json:"open_issue_count"`
	LastObservedAt      *time.Time              `json:"last_observed_at,omitempty"`
	DetailPath          string                  `json:"detail_path,omitempty"`
}

type ResolveCatalogSummariesResponse struct {
	Results []CatalogSummaryResolution `json:"results"`
}
