package models

import (
	"database/sql/driver"
	"encoding/json"
	commonExecution "github.com/addp/common/execution"
)

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
	TriggerType string                 `json:"trigger_type" binding:"required,oneof=manual scheduled"`
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
	TriggerType string `form:"trigger_type" binding:"omitempty,oneof=manual scheduled"`
	StartDate   string `form:"start_date"` // YYYY-MM-DD
	EndDate     string `form:"end_date"`   // YYYY-MM-DD
}

// ExecutionWithDevItem 执行记录和开发任务关联
type ExecutionWithDevItem struct {
	*commonExecution.TaskExecution
	DevItem *DevTask `json:"dev_item,omitempty"`
}

// ListExecutionsResponse 执行列表响应
type ListExecutionsResponse struct {
	Executions []ExecutionWithDevItem `json:"executions"`
	Total      int64                  `json:"total"`
	Page       int                    `json:"page"`
	PageSize   int                    `json:"page_size"`
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
