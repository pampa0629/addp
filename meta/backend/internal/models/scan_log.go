package models

import (
	"time"
)

// ScanLog 扫描日志（用于追踪扫描历史）
type ScanLog struct {
	ID            uint       `gorm:"primaryKey" json:"id"`
	ResourceID    uint       `gorm:"not null;index" json:"resource_id"`
	SchemaID      *uint      `gorm:"index" json:"schema_id,omitempty"`      // 可选：特定Schema扫描
	TenantID      uint       `gorm:"not null;index" json:"tenant_id"`

	// 扫描类型
	ScanType      string     `gorm:"size:50;not null" json:"scan_type"`     // auto/manual/scheduled
	ScanDepth     string     `gorm:"size:20" json:"scan_depth"`             // basic/deep/full

	// 扫描范围
	TargetSchemas string     `gorm:"type:text" json:"target_schemas"`       // JSON数组: ["schema1", "schema2"]

	// 扫描状态
	Status        string     `gorm:"size:20;not null" json:"status"`        // running/success/failed
	ErrorMessage  string     `gorm:"type:text" json:"error_message,omitempty"`

	// 扫描统计
	SchemasScanned int       `json:"schemas_scanned"`
	TablesScanned  int       `json:"tables_scanned"`
	FieldsScanned  int       `json:"fields_scanned"`

	// 时间统计
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	DurationMs    int64      `json:"duration_ms"`                           // 耗时(毫秒)

	CreatedAt     time.Time  `json:"created_at"`
}

func (ScanLog) TableName() string {
	return "scan_logs"
}
