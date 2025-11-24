package models

import (
	"time"

	"gorm.io/gorm"
)

// DataLineage 数据血缘记录
// 设计原则：独立于任何工作流引擎，支持多种使用场景
type DataLineage struct {
	ID uint `gorm:"primaryKey" json:"id"`

	// ========== 外部工作流引擎信息（可选） ==========
	// 支持任何工作流引擎：Temporal / Airflow / Prefect / 手动记录
	ExternalWorkflowID  string `gorm:"index" json:"external_workflow_id,omitempty"`  // 工作流 ID
	ExternalExecutionID string `gorm:"index" json:"external_execution_id,omitempty"` // 执行 ID/RunID
	WorkflowEngine      string `json:"workflow_engine,omitempty"`                    // temporal/airflow/manual/""

	// ========== 源数据信息 ==========
	SourceItemID      uint   `gorm:"index" json:"source_item_id"`      // 源数据项 ID
	SourceFingerprint string `gorm:"index" json:"source_fingerprint"`  // 源数据指纹（用于跨系统追踪）
	SourceType        string `json:"source_type"`                      // shapefile/geojson/table/object
	SourcePath        string `json:"source_path"`                      // 源数据路径

	// ========== 目标数据信息 ==========
	TargetItemID      uint   `gorm:"index" json:"target_item_id"`      // 目标数据项 ID
	TargetFingerprint string `gorm:"index" json:"target_fingerprint"`  // 目标数据指纹
	TargetType        string `json:"target_type"`                      // mvt/table/file/object
	TargetPath        string `json:"target_path"`                      // 目标数据路径

	// ========== 转换信息 ==========
	LineageType     string `gorm:"index" json:"lineage_type"`       // transform/copy/merge/aggregate/filter
	TransformConfig string `gorm:"type:text" json:"transform_config"` // JSON 字符串：转换配置

	// ========== 执行信息 ==========
	Status       string     `gorm:"index" json:"status"`            // success/failed/partial
	ErrorMessage string     `gorm:"type:text" json:"error_message,omitempty"`
	StartTime    time.Time  `gorm:"index" json:"start_time"`
	EndTime      *time.Time `json:"end_time,omitempty"`
	DurationMs   int64      `json:"duration_ms"` // 执行耗时（毫秒）

	// ========== 指标信息 ==========
	RecordsProcessed int64  `json:"records_processed"` // 处理记录数
	BytesWritten     int64  `json:"bytes_written"`     // 写入字节数
	Metrics          string `gorm:"type:text" json:"metrics"` // JSON 字符串：其他指标

	// ========== 审计字段 ==========
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名
func (DataLineage) TableName() string {
	return "data_lineage"
}
