package models

import "time"

// LineageInput 血缘记录输入（通用数据结构，独立于任何框架）
type LineageInput struct {
	// 外部工作流引擎信息（可选）
	ExternalWorkflowID  string `json:"external_workflow_id,omitempty"`
	ExternalExecutionID string `json:"external_execution_id,omitempty"`
	WorkflowEngine      string `json:"workflow_engine,omitempty"` // temporal/airflow/manual/""

	// 源数据
	SourceItemID      uint   `json:"source_item_id"`
	SourceFingerprint string `json:"source_fingerprint"`
	SourceType        string `json:"source_type"`
	SourcePath        string `json:"source_path"`

	// 目标数据
	TargetItemID      uint   `json:"target_item_id"`
	TargetFingerprint string `json:"target_fingerprint"`
	TargetType        string `json:"target_type"`
	TargetPath        string `json:"target_path"`

	// 转换信息
	LineageType     string                 `json:"lineage_type"` // transform/copy/merge/aggregate
	TransformConfig map[string]interface{} `json:"transform_config,omitempty"`

	// 执行信息
	Status       string    `json:"status"` // success/failed/partial
	ErrorMessage string    `json:"error_message,omitempty"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`

	// 指标
	RecordsProcessed int64                  `json:"records_processed,omitempty"`
	BytesWritten     int64                  `json:"bytes_written,omitempty"`
	Metrics          map[string]interface{} `json:"metrics,omitempty"`
}

// LineageQuery 血缘查询条件
type LineageQuery struct {
	// 按数据项查询
	SourceItemID *uint
	TargetItemID *uint

	// 按工作流查询
	ExternalWorkflowID  string
	ExternalExecutionID string
	WorkflowEngine      string

	// 按时间范围查询
	StartTimeFrom *time.Time
	StartTimeTo   *time.Time

	// 按状态查询
	Status string

	// 按类型查询
	LineageType string
	SourceType  string
	TargetType  string

	// 分页
	Limit  int
	Offset int
}
