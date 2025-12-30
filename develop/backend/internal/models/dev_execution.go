package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// DevExecution 统一执行记录表
type DevExecution struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	DevItemID   *uint  `gorm:"index:idx_dev_executions_item" json:"dev_item_id,omitempty"`
	TenantID    uint   `gorm:"not null;index:idx_dev_executions_tenant_status" json:"tenant_id"`
	ExecutionID string `gorm:"size:255;uniqueIndex:idx_dev_executions_execution_id;not null" json:"execution_id"` // UUID 全局唯一标识

	// 执行信息
	DevType     string `gorm:"size:50;not null" json:"dev_type"`                                    // 'query' | 'workflow' | 'script' | 'notebook'
	TriggerType string `gorm:"size:50;index:idx_dev_executions_trigger_type" json:"trigger_type"` // 'manual' | 'schedule' | 'orchestrator' | 'api'
	TriggeredBy *uint  `json:"triggered_by,omitempty"`

	// 状态跟踪
	Status      string `gorm:"size:50;not null;index:idx_dev_executions_tenant_status" json:"status"` // 'pending' | 'running' | 'success' | 'failed' | 'timeout' | 'cancelled'
	Progress    int    `gorm:"default:0" json:"progress"`                                              // 0-100
	CurrentStep string `gorm:"size:255" json:"current_step,omitempty"`

	// 执行结果
	Result       ExecutionResult `gorm:"type:jsonb" json:"result,omitempty"`
	ErrorMessage string          `gorm:"type:text" json:"error_message,omitempty"`
	ExecutionTimeMs *int64       `json:"execution_time_ms,omitempty"` // 执行时间（毫秒）

	// 资源使用
	EngineID         *uint  `json:"engine_id,omitempty"`
	RowsAffected     *int64 `json:"rows_affected,omitempty"`
	ResultSizeBytes  *int64 `json:"result_size_bytes,omitempty"`

	// 时间戳
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	CreatedAt   time.Time  `gorm:"index:idx_dev_executions_created_at,sort:desc" json:"created_at"`
}

// TableName 指定表名
func (DevExecution) TableName() string {
	return "develop.dev_executions"
}

// ExecutionResult 执行结果（支持任意 JSON 结构）
type ExecutionResult map[string]interface{}

// Value 实现 driver.Valuer 接口
func (r ExecutionResult) Value() (driver.Value, error) {
	if r == nil {
		return nil, nil
	}
	return json.Marshal(r)
}

// Scan 实现 sql.Scanner 接口
func (r *ExecutionResult) Scan(value interface{}) error {
	if value == nil {
		*r = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, r)
}

// CreateExecutionRequest 创建执行请求
type CreateExecutionRequest struct {
	DevItemID   *uint                  `json:"dev_item_id"`
	DevType     string                 `json:"dev_type" binding:"required,oneof=query workflow script notebook"`
	TriggerType string                 `json:"trigger_type" binding:"required,oneof=manual schedule orchestrator api"`
	EngineID    *uint                  `json:"engine_id"`
	Content     map[string]interface{} `json:"content"` // 执行内容（对于临时执行）
}

// UpdateExecutionRequest 更新执行状态请求
type UpdateExecutionRequest struct {
	Status          string                 `json:"status" binding:"omitempty,oneof=pending running success failed timeout cancelled"`
	Progress        int                    `json:"progress" binding:"min=0,max=100"`
	CurrentStep     string                 `json:"current_step"`
	Result          map[string]interface{} `json:"result"`
	ErrorMessage    string                 `json:"error_message"`
	ExecutionTimeMs *int64                 `json:"execution_time_ms"`
	RowsAffected    *int64                 `json:"rows_affected"`
	ResultSizeBytes *int64                 `json:"result_size_bytes"`
}

// ListExecutionsRequest 查询执行列表请求
type ListExecutionsRequest struct {
	Page        int    `form:"page" binding:"min=1"`
	PageSize    int    `form:"page_size" binding:"min=1,max=100"`
	DevItemID   *uint  `form:"dev_item_id"`
	DevType     string `form:"dev_type" binding:"omitempty,oneof=query workflow script notebook"`
	Status      string `form:"status" binding:"omitempty,oneof=pending running success failed timeout cancelled"`
	TriggerType string `form:"trigger_type" binding:"omitempty,oneof=manual schedule orchestrator api"`
	StartDate   string `form:"start_date"` // YYYY-MM-DD
	EndDate     string `form:"end_date"`   // YYYY-MM-DD
}

// ExecutionWithItem 执行记录和开发项关联
type ExecutionWithItem struct {
	DevExecution
	DevItem *DevItem `json:"dev_item,omitempty"`
}

// ListExecutionsResponse 执行列表响应
type ListExecutionsResponse struct {
	Executions []ExecutionWithItem `json:"executions"`
	Total      int64               `json:"total"`
	Page       int                 `json:"page"`
	PageSize   int                 `json:"page_size"`
}

// ExecutionStatistics 执行统计
type ExecutionStatistics struct {
	TotalExecutions   int64   `json:"total_executions"`
	SuccessCount      int64   `json:"success_count"`
	FailedCount       int64   `json:"failed_count"`
	RunningCount      int64   `json:"running_count"`
	SuccessRate       float64 `json:"success_rate"`
	AvgExecutionTime  float64 `json:"avg_execution_time_ms"`
	TotalRowsAffected int64   `json:"total_rows_affected"`
}
