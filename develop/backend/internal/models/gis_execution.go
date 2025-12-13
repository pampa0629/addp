package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// GISExecution GIS 任务执行实例
type GISExecution struct {
	ID              uint                   `gorm:"primaryKey" json:"id"`
	TaskID          *uint                  `gorm:"index" json:"task_id,omitempty"`
	TaskName        string                 `gorm:"size:255" json:"task_name"` // 冗余任务名（方便查询）
	Status          string                 `gorm:"size:20;not null;index" json:"status"` // pending, running, success, failed, timeout
	Inputs          ExecutionInputs        `gorm:"type:jsonb" json:"inputs,omitempty"`
	ResultTable     string                 `gorm:"size:255" json:"result_table,omitempty"`     // 结果表名
	ResultCount     *int                   `json:"result_count,omitempty"`                     // 结果记录数
	ErrorMessage    string                 `gorm:"type:text" json:"error_message,omitempty"`
	Logs            string                 `gorm:"type:text" json:"logs,omitempty"`
	ExecutionTimeMs *int                   `json:"execution_time_ms,omitempty"`                // 执行时间（毫秒）
	TriggerType     string                 `gorm:"size:50" json:"trigger_type"`                // manual, schedule, orchestrator, api
	TriggerBy       *uint                  `json:"trigger_by,omitempty"`                       // 触发用户ID
	TenantID        uint                   `gorm:"not null;index" json:"tenant_id"`
	StartedAt       time.Time              `gorm:"not null;index:idx_spatial_executions_started_at,sort:desc" json:"started_at"`
	CompletedAt     *time.Time             `json:"completed_at,omitempty"`
}

func (GISExecution) TableName() string {
	return "develop.spatial_executions"
}

// ExecutionInputs 执行输入参数
type ExecutionInputs map[string]interface{}

// Value 实现 driver.Valuer 接口
func (e ExecutionInputs) Value() (driver.Value, error) {
	if e == nil {
		return nil, nil
	}
	return json.Marshal(e)
}

// Scan 实现 sql.Scanner 接口
func (e *ExecutionInputs) Scan(value interface{}) error {
	if value == nil {
		*e = nil
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, e)
}

// CreateGISExecutionRequest 创建执行请求
type CreateGISExecutionRequest struct {
	TaskID      uint                   `json:"task_id" binding:"required"`
	Inputs      map[string]interface{} `json:"inputs"`
	TriggerType string                 `json:"trigger_type"` // manual, schedule, orchestrator, api
}

// ExecuteTaskRequest 执行任务请求
type ExecuteTaskRequest struct {
	Inputs map[string]interface{} `json:"inputs"`
}

// ListExecutionsRequest 查询执行列表请求
type ListExecutionsRequest struct {
	Page        int    `form:"page" binding:"min=1"`
	PageSize    int    `form:"page_size" binding:"min=1,max=100"`
	TaskID      *uint  `form:"task_id"`
	Status      string `form:"status"`
	TriggerType string `form:"trigger_type"`
	StartDate   string `form:"start_date"` // YYYY-MM-DD
	EndDate     string `form:"end_date"`   // YYYY-MM-DD
}

// GISExecutionResponse 执行响应
type GISExecutionResponse struct {
	GISExecution
	Task *SpatialTask `json:"task,omitempty"`
}

// ListExecutionsResponse 执行列表响应
type ListExecutionsResponse struct {
	Executions []GISExecutionResponse `json:"executions"`
	Total      int64                  `json:"total"`
	Page       int                    `json:"page"`
	PageSize   int                    `json:"page_size"`
}
