package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// TaskType 任务类型
type TaskType string

const (
	TaskTypeImport TaskType = "import" // 导入
	TaskTypeExport TaskType = "export" // 导出
	TaskTypeSync   TaskType = "sync"   // 同步
)

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskStatusPending TaskStatus = "pending" // 待执行
	TaskStatusRunning TaskStatus = "running" // 运行中
	TaskStatusSuccess TaskStatus = "success" // 成功
	TaskStatusFailed  TaskStatus = "failed"  // 失败
	TaskStatusPaused  TaskStatus = "paused"  // 暂停
	TaskStatusStopped TaskStatus = "stopped" // 已停止
)

// TaskMode 任务模式
type TaskMode string

const (
	TaskModeBatch      TaskMode = "batch"       // 批处理模式
	TaskModeStream     TaskMode = "stream"      // 流式模式
	TaskModeMicroBatch TaskMode = "micro-batch" // 微批次模式
)

// JSONMap 自定义 JSON 类型
type JSONMap map[string]interface{}

// Value 实现 driver.Valuer 接口
func (m JSONMap) Value() (driver.Value, error) {
	return json.Marshal(m)
}

// Scan 实现 sql.Scanner 接口
func (m *JSONMap) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, m)
}

// Task 传输任务
type Task struct {
	ID             uint       `gorm:"primaryKey" json:"id"`
	Name           string     `gorm:"type:varchar(255);not null" json:"name"`
	Description    string     `gorm:"type:text" json:"description"`
	Type           TaskType   `gorm:"type:varchar(50);not null" json:"type"`
	Mode           TaskMode   `gorm:"type:varchar(20);default:'batch'" json:"mode"`
	SourceID       *uint      `gorm:"index" json:"source_id,omitempty"`       // 源数据源 ID（关联 system.resources）
	TargetID       *uint      `gorm:"index" json:"target_id,omitempty"`       // 目标数据源 ID
	Config         JSONMap    `gorm:"type:jsonb;not null" json:"config"`      // 任务配置
	Schedule       string     `gorm:"type:varchar(100)" json:"schedule"`      // Cron 表达式
	BatchSize      int        `gorm:"default:1000" json:"batch_size"`         // 批大小
	MaxParallelism int        `gorm:"default:1" json:"max_parallelism"`       // 最大并行度
	RetryPolicy    JSONMap    `gorm:"type:jsonb" json:"retry_policy"`         // 重试策略
	Status         TaskStatus `gorm:"type:varchar(20);default:'pending';index" json:"status"`
	Progress       float64    `gorm:"type:numeric(5,2);default:0" json:"progress"` // 0-100
	LastExecutionID *uint     `gorm:"index" json:"last_execution_id,omitempty"`
	CreatedBy      *uint      `json:"created_by,omitempty"`
	TenantID       uint       `gorm:"not null;index" json:"tenant_id"`
	CreatedAt      time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// TableName 指定表名
func (Task) TableName() string {
	return "transfer.tasks"
}

// ExecutionStatus 执行状态
type ExecutionStatus string

const (
	ExecutionStatusPending   ExecutionStatus = "pending"   // 待执行
	ExecutionStatusRunning   ExecutionStatus = "running"   // 运行中
	ExecutionStatusSuccess   ExecutionStatus = "success"   // 成功
	ExecutionStatusFailed    ExecutionStatus = "failed"    // 失败
	ExecutionStatusCancelled ExecutionStatus = "cancelled" // 已取消
)

// TaskExecution 任务执行记录
type TaskExecution struct {
	ID              uint            `gorm:"primaryKey" json:"id"`
	TaskID          uint            `gorm:"not null;index:idx_executions_task" json:"task_id"`
	Status          ExecutionStatus `gorm:"type:varchar(20);not null;index:idx_executions_status" json:"status"`
	StartTime       time.Time       `gorm:"not null;index:idx_executions_start_time" json:"start_time"`
	EndTime         *time.Time      `json:"end_time,omitempty"`
	RecordsRead     int64           `gorm:"default:0" json:"records_read"`
	RecordsWritten  int64           `gorm:"default:0" json:"records_written"`
	BytesRead       int64           `gorm:"default:0" json:"bytes_read"`
	BytesWritten    int64           `gorm:"default:0" json:"bytes_written"`
	ErrorMsg        string          `gorm:"type:text" json:"error_msg,omitempty"`
	Logs            string          `gorm:"type:text" json:"logs,omitempty"`
	CheckpointOffset int64          `gorm:"default:0" json:"checkpoint_offset"`
	CheckpointState JSONMap         `gorm:"type:jsonb" json:"checkpoint_state,omitempty"`
	TriggerType     string          `gorm:"type:varchar(50)" json:"trigger_type"` // manual, schedule, api
	TriggerBy       *uint           `json:"trigger_by,omitempty"`
}

// TableName 指定表名
func (TaskExecution) TableName() string {
	return "transfer.task_executions"
}

// Duration 返回执行时长
func (e *TaskExecution) Duration() time.Duration {
	if e.EndTime == nil {
		return time.Since(e.StartTime)
	}
	return e.EndTime.Sub(e.StartTime)
}

// DataMapping 数据映射配置
type DataMapping struct {
	ID           uint    `gorm:"primaryKey" json:"id"`
	TaskID       uint    `gorm:"not null;index" json:"task_id"`
	SourceField  string  `gorm:"type:varchar(255);not null" json:"source_field"`
	TargetField  string  `gorm:"type:varchar(255);not null" json:"target_field"`
	Transform    string  `gorm:"type:varchar(500)" json:"transform,omitempty"`    // 转换函数
	DefaultValue string  `gorm:"type:text" json:"default_value,omitempty"`        // 默认值
	FieldType    string  `gorm:"type:varchar(50)" json:"field_type,omitempty"`    // 字段类型
	Format       string  `gorm:"type:varchar(100)" json:"format,omitempty"`       // 格式（日期等）
	Nullable     bool    `gorm:"default:true" json:"nullable"`                    // 是否可为空
	CreatedAt    time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

// TableName 指定表名
func (DataMapping) TableName() string {
	return "transfer.data_mappings"
}

// CreateTaskRequest 创建任务请求
type CreateTaskRequest struct {
	Name           string            `json:"name" binding:"required"`
	Description    string            `json:"description"`
	Type           TaskType          `json:"type" binding:"required"`
	Mode           TaskMode          `json:"mode"`
	SourceID       *uint             `json:"source_id"`
	TargetID       *uint             `json:"target_id"`
	Config         map[string]interface{} `json:"config" binding:"required"`
	Schedule       string            `json:"schedule"`
	BatchSize      int               `json:"batch_size"`
	MaxParallelism int               `json:"max_parallelism"`
	Mappings       []DataMapping     `json:"mappings"`
}

// UpdateTaskRequest 更新任务请求
type UpdateTaskRequest struct {
	Name           *string           `json:"name"`
	Description    *string           `json:"description"`
	Config         map[string]interface{} `json:"config"`
	Schedule       *string           `json:"schedule"`
	BatchSize      *int              `json:"batch_size"`
	MaxParallelism *int              `json:"max_parallelism"`
	Status         *TaskStatus       `json:"status"`
}

// ListTasksRequest 查询任务列表请求
type ListTasksRequest struct {
	Type     *TaskType   `form:"type"`
	Status   *TaskStatus `form:"status"`
	Page     int         `form:"page" binding:"min=1"`
	PageSize int         `form:"page_size" binding:"min=1,max=100"`
}

// TaskStatistics 任务统计
type TaskStatistics struct {
	TotalTasks       int64 `json:"total_tasks"`
	PendingTasks     int64 `json:"pending_tasks"`
	RunningTasks     int64 `json:"running_tasks"`
	SuccessTasks     int64 `json:"success_tasks"`
	FailedTasks      int64 `json:"failed_tasks"`
	TotalExecutions  int64 `json:"total_executions"`
	TotalRecords     int64 `json:"total_records"`
	TotalBytes       int64 `json:"total_bytes"`
}

// CreateDataMappingRequest 创建数据映射请求
type CreateDataMappingRequest struct {
	SourceField  string `json:"source_field" binding:"required"`
	TargetField  string `json:"target_field" binding:"required"`
	Transform    string `json:"transform"`
	DefaultValue string `json:"default_value"`
	FieldType    string `json:"field_type"`
	Format       string `json:"format"`
	Nullable     bool   `json:"nullable"`
}

// UpdateDataMappingRequest 更新数据映射请求
type UpdateDataMappingRequest struct {
	SourceField  *string `json:"source_field"`
	TargetField  *string `json:"target_field"`
	Transform    *string `json:"transform"`
	DefaultValue *string `json:"default_value"`
	FieldType    *string `json:"field_type"`
	Format       *string `json:"format"`
	Nullable     *bool   `json:"nullable"`
}
