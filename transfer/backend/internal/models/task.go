package models

import (
	"database/sql/driver"
	"time"

	commonModels "github.com/addp/common/models"
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

// TaskType 任务类型
type TaskType string

const (
	TaskTypeImport TaskType = "import" // 导入
	TaskTypeExport TaskType = "export" // 导出
	TaskTypeSync   TaskType = "sync"   // 同步
)

// TaskStatus 任务状态（简化版）
type TaskStatus string

const (
	TaskStatusIdle    TaskStatus = "idle"    // 空闲（未执行或执行完成）
	TaskStatusRunning TaskStatus = "running" // 执行中
)

// JSONMap is now imported from common/models
// Use commonModels.JSONMap instead
type JSONMap = commonModels.JSONMap

// Task 传输任务
type Task struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	Name             string     `gorm:"type:varchar(255);not null" json:"name"`
	Description      string     `gorm:"type:text" json:"description"`
	Type             TaskType   `gorm:"type:varchar(50);not null" json:"type"`
	Config           JSONMap    `gorm:"type:jsonb;not null" json:"config"` // 任务配置（包含 source 和 target）
	Schedule         string     `gorm:"type:varchar(100)" json:"schedule"` // Cron 表达式
	BatchSize        int        `gorm:"default:1000" json:"batch_size"`    // 批大小
	Enabled          bool       `gorm:"default:false;index" json:"enabled"` // 任务启用状态（用于定时任务）
	AutoScanMetadata bool       `gorm:"default:true" json:"auto_scan_metadata"` // 任务完成后自动扫描元数据
	Status           TaskStatus `gorm:"type:varchar(20);default:'idle';index" json:"status"`
	Progress         float64    `gorm:"type:numeric(5,2);default:0" json:"progress"` // 0-100
	CreatedBy        *uint      `json:"created_by,omitempty"`
	TenantID         uint       `gorm:"not null;index" json:"tenant_id"`
	CreatedAt        time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`
}

// TableName 指定表名
func (Task) TableName() string {
	return "transfer.tasks"
}

// ExecutionStatus 执行状态
type ExecutionStatus string

const (
	ExecutionStatusPending ExecutionStatus = "pending" // 待执行
	ExecutionStatusRunning ExecutionStatus = "running" // 运行中
	ExecutionStatusSuccess ExecutionStatus = "success" // 成功
	ExecutionStatusFailed  ExecutionStatus = "failed"  // 失败
)

// TaskExecution 任务执行记录
type TaskExecution struct {
	ID               uint            `gorm:"primaryKey" json:"id"`
	TaskID           uint            `gorm:"not null;index:idx_executions_task" json:"task_id"`
	Status           ExecutionStatus `gorm:"type:varchar(20);not null;index:idx_executions_status" json:"status"`
	StartTime        LocalTime       `gorm:"not null;index:idx_executions_start_time;type:timestamp" json:"start_time"`
	EndTime          *LocalTime      `gorm:"type:timestamp" json:"end_time,omitempty"`
	RecordsRead      int64           `gorm:"default:0" json:"records_read"`
	RecordsWritten   int64           `gorm:"default:0" json:"records_written"`
	BytesRead        int64           `gorm:"default:0" json:"bytes_read"`
	BytesWritten     int64           `gorm:"default:0" json:"bytes_written"`
	ErrorMsg         string          `gorm:"type:text" json:"error_msg,omitempty"`
	Logs             string          `gorm:"type:text" json:"logs,omitempty"`
	CheckpointOffset int64           `gorm:"default:0" json:"checkpoint_offset"`
	CheckpointState  JSONMap         `gorm:"type:jsonb" json:"checkpoint_state,omitempty"`
	TriggerType      string          `gorm:"type:varchar(50)" json:"trigger_type"` // manual, schedule, api
	TriggerBy        *uint           `json:"trigger_by,omitempty"`
}

// TableName 指定表名
func (TaskExecution) TableName() string {
	return "transfer.task_executions"
}

// Duration 返回执行时长
func (e *TaskExecution) Duration() time.Duration {
	if e.EndTime == nil {
		return time.Since(e.StartTime.Time)
	}
	return e.EndTime.Time.Sub(e.StartTime.Time)
}

// DataMapping 数据映射配置
type DataMapping struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	TaskID       uint      `gorm:"not null;index" json:"task_id"`
	SourceField  string    `gorm:"type:varchar(255);not null" json:"source_field"`
	TargetField  string    `gorm:"type:varchar(255);not null" json:"target_field"`
	Transform    string    `gorm:"type:varchar(500)" json:"transform,omitempty"` // 转换函数
	DefaultValue string    `gorm:"type:text" json:"default_value,omitempty"`     // 默认值
	FieldType    string    `gorm:"type:varchar(50)" json:"field_type,omitempty"` // 字段类型
	Format       string    `gorm:"type:varchar(100)" json:"format,omitempty"`    // 格式（日期等）
	Nullable     bool      `gorm:"default:true" json:"nullable"`                 // 是否可为空
	CreatedAt    time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
}

// TableName 指定表名
func (DataMapping) TableName() string {
	return "transfer.data_mappings"
}

// CreateTaskRequest 创建任务请求
type CreateTaskRequest struct {
	Name             string                 `json:"name" binding:"required"`
	Description      string                 `json:"description"`
	Type             TaskType               `json:"type" binding:"required"`
	Config           map[string]interface{} `json:"config" binding:"required"` // 包含 source 和 target 配置
	Schedule         string                 `json:"schedule"`
	BatchSize        int                    `json:"batch_size"`
	AutoScanMetadata *bool                  `json:"auto_scan_metadata"` // 任务完成后自动扫描元数据（默认 true）
	Mappings         []DataMapping          `json:"mappings"`
}

// UpdateTaskRequest 更新任务请求
type UpdateTaskRequest struct {
	Name             *string                `json:"name"`
	Description      *string                `json:"description"`
	Config           map[string]interface{} `json:"config"`
	Schedule         *string                `json:"schedule"`
	BatchSize        *int                   `json:"batch_size"`
	Enabled          *bool                  `json:"enabled"`
	AutoScanMetadata *bool                  `json:"auto_scan_metadata"`
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
	SuccessTasks     int64 `json:"success_tasks"` // 复用字段名，表示已完成任务数
	FailedTasks      int64 `json:"failed_tasks"`  // 复用字段名，表示已停止任务数
	NotExecutedTasks int64 `json:"not_executed_tasks"`
	LastRunningTasks int64 `json:"last_running_tasks"`
	LastSuccessTasks int64 `json:"last_success_tasks"`
	LastFailedTasks  int64 `json:"last_failed_tasks"`
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
