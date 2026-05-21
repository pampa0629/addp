package models

import (
	"database/sql/driver"
	"time"

	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

// LocalTime 本地时间类型，序列化为不带时区的本地时间字符串
type LocalTime struct {
	time.Time
}

// MarshalJSON 自定义 JSON 序列化，返回本地时间格式
func (t LocalTime) MarshalJSON() ([]byte, error) {
	if t.Time.IsZero() {
		return []byte("null"), nil
	}
	// 格式化为本地时间字符串（不带时区标识）
	formatted := t.Time.Format(`"2006-01-02T15:04:05"`)
	return []byte(formatted), nil
}

// UnmarshalJSON 自定义 JSON 反序列化
func (t *LocalTime) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	str := string(data)
	if len(str) > 2 {
		str = str[1 : len(str)-1] // 去掉引号
	}
	parsed, err := time.Parse("2006-01-02T15:04:05", str)
	if err != nil {
		// 尝试其他格式
		parsed, err = time.Parse(time.RFC3339, str)
		if err != nil {
			return err
		}
	}
	t.Time = parsed
	return nil
}

// Scan 实现 sql.Scanner 接口，允许从数据库读取时间
func (t *LocalTime) Scan(value interface{}) error {
	if value == nil {
		t.Time = time.Time{}
		return nil
	}
	if v, ok := value.(time.Time); ok {
		t.Time = v
		return nil
	}
	return nil
}

// Value 实现 driver.Valuer 接口，允许写入数据库
func (t LocalTime) Value() (driver.Value, error) {
	if t.Time.IsZero() {
		return nil, nil
	}
	return t.Time, nil
}

// TaskStatus 任务状态（简化版）
type TaskStatus string

const (
	TaskStatusIdle    TaskStatus = "idle"    // 空闲（未执行或执行完成）
	TaskStatusRunning TaskStatus = "running" // 执行中
)

// JSONMap is now imported from common/models
// Use commonModels.JSONMap instead
type JSONMap = commonModels.JSONMap

// TransferTask 传输任务定义
type TransferTask struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	TenantID         uint       `gorm:"not null;index" json:"tenant_id"`
	Name             string     `gorm:"type:varchar(255);not null" json:"name"`
	Description      string     `gorm:"type:text" json:"description"`
	TaskType         string     `gorm:"type:varchar(20);not null;default:'import';index" json:"task_type"` // import | export | sync
	Config           JSONMap    `gorm:"type:jsonb;not null" json:"config"`                                 // Reader-Transform-Writer 管道配置
	Schedule         string     `gorm:"type:varchar(100)" json:"schedule"`                                 // Cron 表达式
	BatchSize        int        `gorm:"default:1000" json:"batch_size"`
	Enabled          bool       `gorm:"default:false;index" json:"enabled"`
	AutoScanMetadata bool       `json:"auto_scan_metadata"`
	Status           TaskStatus `gorm:"type:varchar(20);default:'idle';index" json:"status"`
	Progress         float64    `gorm:"type:numeric(5,2);default:0" json:"progress"`
	CreatedBy        *uint      `json:"created_by,omitempty"`
	// BaseTask 基类字段
	LastExecutionID     *string        `gorm:"size:36" json:"last_execution_id,omitempty"`
	LastExecutionStatus *string        `gorm:"size:20" json:"last_execution_status,omitempty"`
	LastRunAt           *time.Time     `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time     `json:"next_run_at,omitempty"`
	CreatedAt           time.Time      `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt           time.Time      `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt           gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// TableName 指定表名（带 schema 前缀）
func (TransferTask) TableName() string {
	return "transfer.transfer_tasks"
}

// ExecutionStatus 执行状态
type ExecutionStatus string

const (
	ExecutionStatusPending ExecutionStatus = "pending" // 待执行
	ExecutionStatusRunning ExecutionStatus = "running" // 运行中
	ExecutionStatusSuccess ExecutionStatus = "success" // 成功
	ExecutionStatusFailed  ExecutionStatus = "failed"  // 失败
)

// TaskExecution Transfer 执行记录 DTO（仅用于 API 响应，数据来自 common.task_executions）
type TaskExecution struct {
	ID               uint            `json:"id"`
	TaskID           uint            `json:"task_id"`
	Status           ExecutionStatus `json:"status"`
	StartTime        LocalTime       `json:"start_time"`
	EndTime          *LocalTime      `json:"end_time,omitempty"`
	RecordsRead      int64           `json:"records_read"`
	RecordsWritten   int64           `json:"records_written"`
	BytesRead        int64           `json:"bytes_read"`
	BytesWritten     int64           `json:"bytes_written"`
	ErrorMsg         string          `json:"error_msg,omitempty"`
	Logs             string          `json:"logs,omitempty"`
	CheckpointOffset int64           `json:"checkpoint_offset"`
	CheckpointState  JSONMap         `json:"checkpoint_state,omitempty"`
	TriggerType      string          `json:"trigger_type"`
	TriggerBy        *uint           `json:"trigger_by,omitempty"`
}

// Duration 返回执行时长
func (e *TaskExecution) Duration() time.Duration {
	if e.EndTime == nil {
		return time.Since(e.StartTime.Time)
	}
	return e.EndTime.Time.Sub(e.StartTime.Time)
}

// FieldMapping 字段映射配置
type FieldMapping struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	TaskID       uint      `gorm:"not null;index" json:"task_id"`
	SourceField  string    `gorm:"type:varchar(255);not null" json:"source_field"`
	TargetField  string    `gorm:"type:varchar(255);not null" json:"target_field"`
	DefaultValue string    `gorm:"type:text" json:"default_value,omitempty"`
	FieldType    string    `gorm:"type:varchar(50)" json:"field_type,omitempty"`
	Format       string    `gorm:"type:varchar(100)" json:"format,omitempty"`
	Nullable     bool      `gorm:"default:true" json:"nullable"`
	CreatedAt    time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名（不包含 schema，因为已通过 search_path 设置）
func (FieldMapping) TableName() string {
	return "field_mappings"
}

// CreateTaskRequest 创建任务请求
type CreateTaskRequest struct {
	Name             string                 `json:"name" binding:"required"`
	Description      string                 `json:"description"`
	TaskType         string                 `json:"task_type"`                 // import | export | sync
	Config           map[string]interface{} `json:"config" binding:"required"` // 包含 source 和 target endpoint 配置；source.attributes 可携带 Meta item 标准 attributes
	Schedule         string                 `json:"schedule"`
	BatchSize        int                    `json:"batch_size"`
	AutoScanMetadata *bool                  `json:"auto_scan_metadata"`
	Mappings         []FieldMapping         `json:"mappings"`
}

// UpdateTaskRequest 更新任务请求
type UpdateTaskRequest struct {
	Name             *string                `json:"name"`
	Description      *string                `json:"description"`
	TaskType         *string                `json:"task_type"`
	Config           map[string]interface{} `json:"config"`
	Schedule         *string                `json:"schedule"`
	BatchSize        *int                   `json:"batch_size"`
	Enabled          *bool                  `json:"enabled"`
	AutoScanMetadata *bool                  `json:"auto_scan_metadata"`
}

// ListTasksRequest 查询任务列表请求
type ListTasksRequest struct {
	Status   *TaskStatus `form:"status"`
	TaskType string      `form:"task_type"`
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
	NotExecutedTasks int64 `json:"not_executed_tasks"`
	LastRunningTasks int64 `json:"last_running_tasks"`
	LastSuccessTasks int64 `json:"last_success_tasks"`
	LastFailedTasks  int64 `json:"last_failed_tasks"`
	TotalExecutions  int64 `json:"total_executions"`
	TotalRecords     int64 `json:"total_records"`
	TotalBytes       int64 `json:"total_bytes"`
}

// CreateFieldMappingRequest 创建字段映射请求
type CreateFieldMappingRequest struct {
	SourceField  string `json:"source_field" binding:"required"`
	TargetField  string `json:"target_field" binding:"required"`
	DefaultValue string `json:"default_value"`
	FieldType    string `json:"field_type"`
	Format       string `json:"format"`
	Nullable     bool   `json:"nullable"`
}

// UpdateFieldMappingRequest 更新字段映射请求
type UpdateFieldMappingRequest struct {
	SourceField  *string `json:"source_field"`
	TargetField  *string `json:"target_field"`
	DefaultValue *string `json:"default_value"`
	FieldType    *string `json:"field_type"`
	Format       *string `json:"format"`
	Nullable     *bool   `json:"nullable"`
}
