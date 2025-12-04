package models

import (
	"time"
)

// Execution SQL执行记录
type Execution struct {
	ID              uint       `gorm:"primaryKey" json:"id"`
	ScriptID        *uint      `gorm:"index" json:"script_id,omitempty"`         // 关联的脚本ID (可选)
	ResourceID      uint       `gorm:"not null;index" json:"resource_id"`        // 目标数据源ID
	SQLContent      string     `gorm:"type:text;not null" json:"sql_content"`    // 执行的SQL内容
	Status          string     `gorm:"size:20;not null;index" json:"status"`     // running, success, failed, timeout
	RowsAffected    *int       `json:"rows_affected,omitempty"`                  // 影响的行数
	ExecutionTimeMs *int       `json:"execution_time_ms,omitempty"`              // 执行时间(毫秒)
	ErrorMessage    string     `gorm:"type:text" json:"error_message,omitempty"` // 错误信息
	ExecutedBy      uint       `gorm:"not null;index" json:"executed_by"`        // 执行用户ID
	TenantID        uint       `gorm:"not null;index" json:"tenant_id"`          // 租户ID
	StartedAt       time.Time  `gorm:"index;default:CURRENT_TIMESTAMP" json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

// TableName 指定表名
func (Execution) TableName() string {
	return "develop.executions"
}

// ExecutionRequest SQL执行请求
type ExecutionRequest struct {
	ResourceID uint   `json:"resource_id" binding:"required"` // 目标数据源ID
	SQL        string `json:"sql" binding:"required"`         // SQL语句
	Timeout    int    `json:"timeout"`                        // 超时时间(秒), 默认30秒
}

// ExecutionResponse SQL执行响应
type ExecutionResponse struct {
	Columns         []string                 `json:"columns"`           // 列名列表
	Rows            []map[string]interface{} `json:"rows"`              // 结果行
	RowsAffected    int                      `json:"rows_affected"`     // 影响的行数
	ExecutionTimeMs int                      `json:"execution_time_ms"` // 执行时间(毫秒)
	ExecutionID     uint                     `json:"execution_id"`      // 执行记录ID
}
